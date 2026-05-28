// Package summarizer calls a configured LLM (Ollama or OpenAI-compatible)
// to produce a short spoken-language summary of session output for use in
// alerts and TTS auto-play. It is wired into the session manager's
// state-transition callback so summaries are generated automatically when a
// session transitions to completed or waiting_input.
package summarizer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
)

// DefaultSummarizerPrompt is the default system prompt for the summarizer.
// Tests reference this constant directly so the expected prompt stays in sync
// with whatever the service uses.
const DefaultSummarizerPrompt = "Summarize the following AI coding session output in 1-3 plain spoken sentences suitable for a voice notification or alert. State what was done, whether it succeeded or failed, and any critical next step. Do not use markdown, code blocks, bullet points, or file paths. Write as if speaking aloud to someone who cannot see the screen."

// Service calls a configured LLM (Ollama or openai-compatible) to produce
// a short spoken-language summary of session output for alerts and TTS.
type Service struct {
	cfg *config.Config
	reg *inference.Registry
}

// New creates a new Service. reg may be nil if no LLM registry is available;
// the service will fall back to direct Ollama calls based on cfg.Ollama.Host.
func New(cfg *config.Config, reg *inference.Registry) *Service {
	return &Service{cfg: cfg, reg: reg}
}

// Summarize sends text to the configured LLM and returns a short summary.
// Returns ("", nil) when the service is disabled or not configured.
func (s *Service) Summarize(ctx context.Context, text string) (string, error) {
	if s.cfg == nil {
		return "", nil
	}
	sc := s.cfg.Session.Summarizer
	if !sc.Enabled || sc.LLMRef == "" {
		return "", nil
	}

	prompt := sc.Prompt
	if strings.TrimSpace(prompt) == "" {
		prompt = DefaultSummarizerPrompt
	}
	fullPrompt := prompt + "\n\n" + text

	// Apply 30-second timeout.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Look up LLM entry in the registry.
	if s.reg != nil {
		llm, err := s.reg.Get(sc.LLMRef)
		if err == nil && llm != nil {
			switch llm.Kind {
			case inference.KindOllama:
				return s.callOllama(ctx, llm, fullPrompt)
			case "openai-api", "openai", inference.KindClaude:
				return s.callOpenAI(ctx, llm, fullPrompt)
			default:
				// For other kinds (openwebui, etc.) try Ollama protocol.
				return s.callOllama(ctx, llm, fullPrompt)
			}
		}
	}

	// Fallback: use cfg.Ollama.Host if configured.
	if s.cfg.Ollama.Enabled && s.cfg.Ollama.Host != "" {
		return s.callOllamaRaw(ctx, s.cfg.Ollama.Host, s.cfg.Ollama.Model, fullPrompt)
	}

	return "", fmt.Errorf("summarizer: LLM %q not found in registry", sc.LLMRef)
}

// callOllama calls the Ollama API using the LLM registry entry.
func (s *Service) callOllama(ctx context.Context, llm *inference.LLM, prompt string) (string, error) {
	// Resolve address from the first compute node in the registry, or fall
	// back to cfg.Ollama.Host.
	host := ""
	if len(llm.ComputeNodes) > 0 {
		// The registry compute node resolution is deep — for now use Ollama host.
		host = s.cfg.Ollama.Host
	}
	if host == "" {
		host = s.cfg.Ollama.Host
	}
	if host == "" {
		host = "http://localhost:11434"
	}
	model := llm.Model
	if model == "" {
		model = s.cfg.Ollama.Model
	}
	return s.callOllamaRaw(ctx, host, model, prompt)
}

// callOllamaRaw sends a POST to {host}/api/generate and parses .response.
func (s *Service) callOllamaRaw(ctx context.Context, host, model, prompt string) (string, error) {
	if model == "" {
		return "", fmt.Errorf("summarizer: no model configured for Ollama")
	}
	body, _ := json.Marshal(map[string]interface{}{
		"model":  model,
		"prompt": prompt,
		"stream": false,
	})
	url := strings.TrimRight(host, "/") + "/api/generate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("summarizer: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("summarizer: ollama request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("summarizer: ollama HTTP %d: %s", resp.StatusCode, string(buf))
	}
	var out struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("summarizer: ollama decode: %w", err)
	}
	return strings.TrimSpace(out.Response), nil
}

// callOpenAI calls an OpenAI-compatible chat/completions endpoint.
func (s *Service) callOpenAI(ctx context.Context, llm *inference.LLM, prompt string) (string, error) {
	baseURL := "https://api.openai.com"
	apiKey := strings.TrimSpace(llm.APIKeyRef)

	model := llm.Model
	if model == "" {
		model = "gpt-4o-mini"
	}

	body, _ := json.Marshal(map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	url := strings.TrimRight(baseURL, "/") + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("summarizer: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("summarizer: openai request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("summarizer: openai HTTP %d: %s", resp.StatusCode, string(buf))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("summarizer: openai decode: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("summarizer: openai returned no choices")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}
