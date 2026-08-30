// Package vision provides image description (vision inference) for datawatch.
// Mirrors the internal/transcribe package pattern for audio → text.
package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultPrompt = "Describe this image concisely."
const defaultMaxBytes = 10 << 20 // 10 MB

// Describer converts image data to a text description.
type Describer interface {
	// Describe returns a natural-language description of the image.
	// imageData is raw image bytes; contentType is the MIME type (e.g. "image/jpeg").
	// prompt overrides the default description prompt when non-empty.
	Describe(ctx context.Context, imageData []byte, contentType, prompt string) (string, error)
}

// Config holds the settings needed to construct a Describer.
type Config struct {
	Backend       string // "ollama" | "openai" | "openai_compat"
	Endpoint      string // base URL (required for all backends)
	APIKey        string // bearer token (required for openai; optional for openai_compat)
	Model         string // e.g. "llava", "moondream", "gpt-4o"
	DefaultPrompt string // overrides the built-in default prompt when non-empty
	MaxImageBytes int64  // max bytes before rejection; 0 → 10 MB
}

// HTTPVisioner calls a remote vision API (Ollama or OpenAI-compatible) to
// describe an image. Implements Describer.
type HTTPVisioner struct {
	cfg    Config
	client *http.Client
}

// New creates an HTTPVisioner from cfg.
// Returns an error if required fields are missing.
func New(cfg Config) (*HTTPVisioner, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("vision: endpoint is required")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("vision: model is required")
	}
	backend := strings.ToLower(cfg.Backend)
	if backend == "" {
		backend = "ollama"
	}
	cfg.Backend = backend
	if cfg.MaxImageBytes <= 0 {
		cfg.MaxImageBytes = defaultMaxBytes
	}
	if cfg.DefaultPrompt == "" {
		cfg.DefaultPrompt = defaultPrompt
	}
	return &HTTPVisioner{
		cfg:    cfg,
		client: &http.Client{Timeout: 2 * time.Minute},
	}, nil
}

// Describe calls the configured vision backend and returns a description.
func (v *HTTPVisioner) Describe(ctx context.Context, imageData []byte, contentType, prompt string) (string, error) {
	if int64(len(imageData)) > v.cfg.MaxImageBytes {
		return "", fmt.Errorf("vision: image too large (%d bytes, max %d)", len(imageData), v.cfg.MaxImageBytes)
	}
	if prompt == "" {
		prompt = v.cfg.DefaultPrompt
	}
	if contentType == "" {
		contentType = "image/jpeg"
	}

	switch v.cfg.Backend {
	case "ollama":
		return v.describeOllama(ctx, imageData, prompt)
	case "openai", "openai_compat":
		return v.describeOpenAI(ctx, imageData, contentType, prompt)
	default:
		return "", fmt.Errorf("vision: unknown backend %q", v.cfg.Backend)
	}
}

// describeOllama uses the Ollama native /api/generate endpoint with the images field.
func (v *HTTPVisioner) describeOllama(ctx context.Context, imageData []byte, prompt string) (string, error) {
	b64 := base64.StdEncoding.EncodeToString(imageData)
	body, err := json.Marshal(map[string]interface{}{
		"model":  v.cfg.Model,
		"prompt": prompt,
		"images": []string{b64},
		"stream": false,
	})
	if err != nil {
		return "", fmt.Errorf("vision: marshal: %w", err)
	}

	endpoint := strings.TrimRight(v.cfg.Endpoint, "/") + "/api/generate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("vision: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if v.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+v.cfg.APIKey)
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("vision: ollama call: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("vision: ollama returned %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var result struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("vision: decode ollama response: %w", err)
	}
	return strings.TrimSpace(result.Response), nil
}

// describeOpenAI uses the OpenAI chat completions endpoint with an image_url content part.
func (v *HTTPVisioner) describeOpenAI(ctx context.Context, imageData []byte, contentType, prompt string) (string, error) {
	dataURI := "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(imageData)
	body, err := json.Marshal(map[string]interface{}{
		"model": v.cfg.Model,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "image_url", "image_url": map[string]string{"url": dataURI}},
					{"type": "text", "text": prompt},
				},
			},
		},
		"max_tokens": 512,
	})
	if err != nil {
		return "", fmt.Errorf("vision: marshal: %w", err)
	}

	endpoint := strings.TrimRight(v.cfg.Endpoint, "/") + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("vision: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if v.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+v.cfg.APIKey)
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("vision: openai call: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("vision: openai returned %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("vision: decode openai response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("vision: empty choices in response")
	}
	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}
