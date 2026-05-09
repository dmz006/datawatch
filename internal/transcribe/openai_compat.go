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

	// v7.0.0-alpha.14 (#236) — 503/504 = server is loading the model.
	// Retry with exponential backoff up to 4 attempts (≈1s + 2s + 4s
	// + 8s = 15s wait). Most lazy-load whisper backends finish a model
	// load well within that window. If still 503, fall through to the
	// regular error path so the chain's secondary takes over.
	for attempt := 0; attempt < 4 && (resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusGatewayTimeout); attempt++ {
		_ = resp.Body.Close()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Duration(1<<uint(attempt)) * time.Second):
		}
		// Re-open the file because the multipart body has been consumed.
		f2, ferr := os.Open(audioPath)
		if ferr != nil {
			return "", fmt.Errorf("transcribe(openai_compat): retry open: %w", ferr)
		}
		body2 := &bytes.Buffer{}
		mw2 := multipart.NewWriter(body2)
		part2, perr := mw2.CreateFormFile("file", filepath.Base(audioPath))
		if perr != nil {
			f2.Close()
			return "", fmt.Errorf("transcribe(openai_compat): retry multipart: %w", perr)
		}
		if _, cerr := io.Copy(part2, f2); cerr != nil {
			f2.Close()
			return "", fmt.Errorf("transcribe(openai_compat): retry copy: %w", cerr)
		}
		f2.Close()
		_ = mw2.WriteField("model", o.Model)
		if o.Language != "" {
			_ = mw2.WriteField("language", o.Language)
		}
		_ = mw2.WriteField("response_format", "json")
		_ = mw2.Close()
		req2, rerr := http.NewRequestWithContext(ctx, http.MethodPost, url, body2)
		if rerr != nil {
			return "", fmt.Errorf("transcribe(openai_compat): retry build: %w", rerr)
		}
		req2.Header.Set("Content-Type", mw2.FormDataContentType())
		if o.APIKey != "" {
			req2.Header.Set("Authorization", "Bearer "+o.APIKey)
		}
		resp, err = client.Do(req2)
		if err != nil {
			return "", fmt.Errorf("transcribe(openai_compat) retry: %w", err)
		}
	}
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

// Preflight (v7.0.0-alpha.14 #236) — actively probe the openai-compat
// endpoint by posting a 1-second silent WAV to /audio/transcriptions
// with the configured model. This both validates reachability AND
// triggers the server to load the whisper model (lazy-loading
// backends like faster-whisper-server and whisper.cpp load the model
// on first request — without an active probe the operator's first
// voice attempt pays the load latency or hits a 503 mid-load).
//
// Returns nil on success. Errors are advisory — caller logs them as
// warnings, doesn't fail startup.
func (o *OpenAICompatTranscriber) Preflight(ctx context.Context) error {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
	}
	// Quick reachability check via /v1/models — if the endpoint is
	// completely unreachable, skip the heavier probe.
	models := o.listModels(ctx)
	knownAudio := false
	for _, n := range knownWhisperNames {
		if n == o.Model {
			knownAudio = true
			break
		}
	}
	listed := false
	for _, id := range models {
		if id == o.Model {
			listed = true
			break
		}
	}
	if !listed && !knownAudio {
		// Configured model isn't in the listed set AND isn't a known
		// whisper name. Runtime will rely on the fallback chain.
		if len(models) == 0 {
			return fmt.Errorf("transcribe(openai_compat): %s unreachable — runtime relies on fallback chain", o.Endpoint)
		}
		return fmt.Errorf("transcribe(openai_compat): model %q not listed by %s/models (listed: %s) and not a known whisper name; runtime will fall back", o.Model, o.Endpoint, strings.Join(models, ", "))
	}

	// Active probe — post the silent WAV. Triggers lazy-load.
	silent := silentWAV(1) // 1 second
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	part, err := mw.CreateFormFile("file", "preflight.wav")
	if err != nil {
		return fmt.Errorf("preflight: multipart: %w", err)
	}
	if _, err := part.Write(silent); err != nil {
		return fmt.Errorf("preflight: write silent: %w", err)
	}
	_ = mw.WriteField("model", o.Model)
	if o.Language != "" {
		_ = mw.WriteField("language", o.Language)
	}
	_ = mw.WriteField("response_format", "json")
	if err := mw.Close(); err != nil {
		return fmt.Errorf("preflight: close multipart: %w", err)
	}
	url := o.Endpoint + "/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return fmt.Errorf("preflight: build request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if o.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.APIKey)
	}
	client := o.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("preflight: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
		return nil
	case resp.StatusCode == http.StatusNotFound:
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("preflight: model %q not found on %s (server: %s) — runtime will use fallback chain", o.Model, o.Endpoint, strings.TrimSpace(string(raw)))
	case resp.StatusCode == http.StatusServiceUnavailable, resp.StatusCode == http.StatusGatewayTimeout:
		// Server is loading; not an error per se.
		return nil
	case resp.StatusCode == http.StatusBadRequest:
		// v7.0.0-alpha.14 (#236) — some servers (OpenWebUI) reject the
		// synthetic silent WAV with "format not supported". The endpoint
		// + auth + model resolution all clearly work; the probe payload
		// is the only issue, which doesn't matter at runtime when the
		// real WAV from the user's mic is sent. Treat as success.
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		body := strings.ToLower(string(raw))
		if strings.Contains(body, "format") || strings.Contains(body, "audio") || strings.Contains(body, "file") {
			return nil
		}
		return fmt.Errorf("preflight: HTTP 400 on %s: %s", o.Endpoint, strings.TrimSpace(string(raw)))
	default:
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("preflight: HTTP %d on %s: %s", resp.StatusCode, o.Endpoint, strings.TrimSpace(string(raw)))
	}
}

// silentWAV builds a minimal 16-bit-PCM mono 16kHz WAV with the
// requested number of seconds of silence. Used by Preflight to
// trigger lazy model load without sending real audio. Header per
// http://soundfile.sapp.org/doc/WaveFormat/.
func silentWAV(seconds int) []byte {
	const sampleRate = 16000
	const bitsPerSample = 16
	const numChannels = 1
	if seconds < 1 {
		seconds = 1
	}
	numSamples := sampleRate * seconds
	dataSize := numSamples * numChannels * (bitsPerSample / 8)
	buf := bytes.NewBuffer(make([]byte, 0, 44+dataSize))
	// RIFF header
	buf.WriteString("RIFF")
	silentWriteLE32(buf, uint32(36+dataSize))
	buf.WriteString("WAVE")
	// fmt chunk
	buf.WriteString("fmt ")
	silentWriteLE32(buf, 16) // PCM chunk size
	silentWriteLE16(buf, 1)  // PCM format
	silentWriteLE16(buf, uint16(numChannels))
	silentWriteLE32(buf, sampleRate)
	silentWriteLE32(buf, uint32(sampleRate*numChannels*bitsPerSample/8)) // byte rate
	silentWriteLE16(buf, uint16(numChannels*bitsPerSample/8))            // block align
	silentWriteLE16(buf, bitsPerSample)
	// data chunk
	buf.WriteString("data")
	silentWriteLE32(buf, uint32(dataSize))
	buf.Write(make([]byte, dataSize)) // silence
	return buf.Bytes()
}

func silentWriteLE16(buf *bytes.Buffer, v uint16) {
	buf.WriteByte(byte(v))
	buf.WriteByte(byte(v >> 8))
}

func silentWriteLE32(buf *bytes.Buffer, v uint32) {
	buf.WriteByte(byte(v))
	buf.WriteByte(byte(v >> 8))
	buf.WriteByte(byte(v >> 16))
	buf.WriteByte(byte(v >> 24))
}
