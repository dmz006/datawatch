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
		return "", fmt.Errorf("transcribe(openai_compat): HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("transcribe(openai_compat): decode response: %w", err)
	}
	return strings.TrimSpace(out.Text), nil
}
