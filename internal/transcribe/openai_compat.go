// BL189 — alternative transcribe backend over an OpenAI-compatible
// HTTP `/v1/audio/transcriptions` endpoint. Works against:
//
//   - OpenAI itself (api.openai.com/v1)
//   - OpenWebUI (`<openwebui>/api/v1/audio/transcriptions`)
//   - faster-whisper-server, whisper.cpp's server mode, and any
//     other OpenAI-compat host.
//
// Operators wanting an "ollama-style" path point this at their
// OpenWebUI fronting ollama (since OpenWebUI exposes the audio
// API; bare ollama doesn't ship audio).

package transcribe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// OpenAICompatTranscriber posts the audio to an OpenAI-compatible
// `/audio/transcriptions` endpoint.
type OpenAICompatTranscriber struct {
	// Endpoint is the base URL — full request goes to
	// `<Endpoint>/audio/transcriptions`. Empty path component is
	// tolerated (joined with `/`).
	Endpoint string
	// APIKey is the bearer credential. Optional for self-hosted.
	APIKey string
	// Model defaults to "whisper-1" (the OpenAI-API canonical name)
	// when empty.
	Model string
	// Language is the ISO 639-1 code; empty enables auto-detect.
	Language string
	// HTTPClient is the http.Client used for the request. nil falls
	// back to a 5-minute-timeout default.
	HTTPClient *http.Client
}

// NewOpenAICompat constructs an OpenAICompatTranscriber. Endpoint is
// required; everything else has a default.
func NewOpenAICompat(endpoint, apiKey, model, language string) (*OpenAICompatTranscriber, error) {
	if strings.TrimSpace(endpoint) == "" {
		return nil, fmt.Errorf("transcribe(openai_compat): endpoint required")
	}
	if model == "" {
		model = "whisper-1"
	}
	if strings.EqualFold(language, "auto") {
		language = ""
	}
	return &OpenAICompatTranscriber{
		Endpoint:   strings.TrimRight(endpoint, "/"),
		APIKey:     apiKey,
		Model:      model,
		Language:   language,
		HTTPClient: &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

// Transcribe POSTs the audio file as multipart/form-data and returns
// the transcribed text.
func (o *OpenAICompatTranscriber) Transcribe(ctx context.Context, audioPath string) (string, error) {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
	}

	f, err := os.Open(audioPath)
	if err != nil {
		return "", fmt.Errorf("transcribe(openai_compat): open audio: %w", err)
	}
	defer f.Close()

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	part, err := mw.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return "", fmt.Errorf("transcribe(openai_compat): multipart: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return "", fmt.Errorf("transcribe(openai_compat): copy audio: %w", err)
	}
	_ = mw.WriteField("model", o.Model)
	if o.Language != "" {
		_ = mw.WriteField("language", o.Language)
	}
	_ = mw.WriteField("response_format", "json")
	if err := mw.Close(); err != nil {
		return "", fmt.Errorf("transcribe(openai_compat): close multipart: %w", err)
	}

	url := o.Endpoint + "/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return "", fmt.Errorf("transcribe(openai_compat): build request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if o.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.APIKey)
	}

	client := o.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("transcribe(openai_compat): %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		body := strings.TrimSpace(string(raw))
		// v7.0.0-alpha.6 — #186 enhanced error: when the server says
		// "model 'X' not found", surface a fix hint with the
		// configured model name so the operator can either change
		// cfg.Voice.WhisperModel OR install the model on the server.
		// v7.0.0-alpha.14 — operator-flagged 2026-05-09: also probe
		// /v1/models for the actual list, and auto-retry once with the
		// first whisper-shaped model. Saves the "go look up the model
		// name in OpenWebUI" round-trip.
		if resp.StatusCode == http.StatusNotFound && strings.Contains(strings.ToLower(body), "model") && strings.Contains(strings.ToLower(body), "not found") {
			// v7.0.0-alpha.14 (operator-flagged 2026-05-09 again): two
			// independent fallback sources, in this order:
			//   1) whisper-shaped ids from /v1/models (most servers
			//      DON'T list audio models there, so often empty)
			//   2) the hardcoded knownWhisperNames chain (whisper-1,
			//      whisper, large-v3, …, tiny) — tried one by one
			// Chat models are NEVER picked (an earlier version
			// silently picked gpt-oss:120b which "transcribed" by
			// replying "I can't do audio").
			models := o.listModels(ctx)
			tried := map[string]bool{o.Model: true}
			candidates := []string{}
			if v := pickWhisperModel(models); v != "" {
				candidates = append(candidates, v)
			}
			for _, n := range knownWhisperNames {
				if !tried[n] {
					candidates = append(candidates, n)
				}
			}
			for _, fallback := range candidates {
				if tried[fallback] {
					continue
				}
				tried[fallback] = true
				if text, retryErr := o.transcribeWithModel(ctx, audioPath, fallback); retryErr == nil {
					return text + "\n\n_(transcribe: auto-fell-back to model '" + fallback + "' — set cfg.voice.whisper_model to silence this notice)_", nil
				}
			}
			modelList := "(none listed by /v1/models)"
			if len(models) > 0 {
				modelList = strings.Join(models, ", ")
			}
			triedList := []string{o.Model}
			for _, n := range candidates {
				triedList = append(triedList, n)
			}
			return "", fmt.Errorf("transcribe(openai_compat): no working whisper model on %s.\n  Configured: %q (404)\n  Auto-tried: %s — all 404\n  Models listed by /v1/models: %s\n\nFix:\n  1. Set cfg.voice.whisper_model to a model the server actually has (consult your OpenWebUI / whisper-server admin)\n  2. Or install one (whisper.cpp: download ggml-large-v3.bin to its models/ dir)\nLast server response: %s", o.Endpoint, o.Model, strings.Join(triedList[1:], ", "), modelList, body)
		}
		return "", fmt.Errorf("transcribe(openai_compat): HTTP %d: %s", resp.StatusCode, body)
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("transcribe(openai_compat): decode response: %w", err)
	}
	return strings.TrimSpace(out.Text), nil
}

// listModels does a best-effort GET on /v1/models and returns the
// model id list, or nil on any failure. Used to populate "available
// models" in the model-not-found error path and to auto-fallback.
func (o *OpenAICompatTranscriber) listModels(ctx context.Context) []string {
	if ctx == nil {
		ctx = context.Background()
	}
	url := o.Endpoint + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	if o.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.APIKey)
	}
	client := o.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil
	}
	ids := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	return ids
}

// knownWhisperNames is the hardcoded fallback chain. Tried in order
// when /v1/models doesn't list a whisper-shaped id (most OpenAI-compat
// hosts list ONLY chat models on that endpoint — the audio transcribe
// path uses a separate, well-known model name).
var knownWhisperNames = []string{
	"whisper-1",      // OpenAI canonical + most openai-compat servers
	"whisper",        // OpenWebUI shorthand
	"large-v3",       // whisper.cpp / faster-whisper-server defaults
	"large-v2", "large",
	"medium", "small", "base", "tiny",
}

// pickWhisperModel scans the /v1/models list for a whisper-shaped id
// (must contain "whisper" — chat models are NEVER picked because they
// silently "transcribe" by replying "I can't do audio"). Returns "" if
// the list contains no whisper-named entry; the caller falls back to
// the hardcoded knownWhisperNames chain.
//
// Operator-flagged 2026-05-09: an earlier version picked the first id
// in the list when nothing matched "whisper", which returned chat-LLM
// completions instead of transcriptions (gpt-oss:120b answered with
// "I'm sorry, but I can't listen to or transcribe audio files").
func pickWhisperModel(ids []string) string {
	for _, id := range ids {
		if id == "whisper-1" {
			return id
		}
	}
	for _, id := range ids {
		if strings.Contains(strings.ToLower(id), "whisper") {
			return id
		}
	}
	return ""
}

// transcribeWithModel runs Transcribe with a one-shot model override.
// Used by the auto-fallback path on model-not-found.
func (o *OpenAICompatTranscriber) transcribeWithModel(ctx context.Context, audioPath, model string) (string, error) {
	tmp := *o
	tmp.Model = model
	return tmp.Transcribe(ctx, audioPath)
}

// Preflight (v7.0.0-alpha.14 #236) — verify the openai-compat
// endpoint is reachable AND the configured model is in the
// known-whisper-name set OR present in /v1/models. Surfaces a clear
// warning at daemon start when the configured model isn't going to
// work, so operators don't first see the failure on a voice attempt.
//
// Returns nil on success. Errors are advisory — caller logs them as
// warnings, doesn't fail startup.
func (o *OpenAICompatTranscriber) Preflight(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	models := o.listModels(ctx)
	// Configured model is fine if listed by /v1/models (case-sensitive)
	// or if it's a known whisper name (server may not list audio
	// models on /v1/models — still works at /audio/transcriptions).
	for _, id := range models {
		if id == o.Model {
			return nil
		}
	}
	for _, n := range knownWhisperNames {
		if n == o.Model {
			return nil
		}
	}
	if len(models) == 0 {
		return fmt.Errorf("transcribe(openai_compat): could not reach %s/models — runtime calls will rely on the fall-back chain", o.Endpoint)
	}
	return fmt.Errorf("transcribe(openai_compat): configured model %q not listed by %s/models (listed: %s) and not a known whisper name; runtime will fall back through %v", o.Model, o.Endpoint, strings.Join(models, ", "), knownWhisperNames)
}
