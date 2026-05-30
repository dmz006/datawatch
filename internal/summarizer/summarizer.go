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
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
)

// DefaultSummarizerPrompt is the default system prompt for the summarizer.
// Tests reference this constant directly so the expected prompt stays in sync
// with whatever the service uses.
const DefaultSummarizerPrompt = "Compress the following AI coding assistant output into exactly 3 short sentences (under 15 words each) suitable for a car dashboard or phone notification. Sentence 1: what was done. Sentence 2: did it succeed or fail. Sentence 3: what comes next. No code, no markdown, no bullet points."

const dualSummaryPrompt = `Analyze the following AI session terminal output and produce two summaries.

===SHORT===
Exactly 3 sentences, each under 15 words. Sentence 1: what was done. Sentence 2: did it succeed or fail. Sentence 3: what comes next or what was asked. No code, no markdown, no lists.

===LONG===
A clear narrative of 3-5 sentences (under 60 words each). Cover: what the session worked on, key decisions or findings, current status, any errors or blockers, and what was last asked of the user. Plain English only.

Output ONLY the two sections with their markers. No preamble, no closing.

Terminal output:
`

// Service calls a configured LLM (Ollama or openai-compatible) to produce
// a short spoken-language summary of session output for alerts and TTS.
type Service struct {
	cfg               *config.Config
	reg               *inference.Registry
	contextLinesCache int
}

// New creates a new Service. reg may be nil if no LLM registry is available;
// the service will fall back to direct Ollama calls based on cfg.Ollama.Host.
func New(cfg *config.Config, reg *inference.Registry) *Service {
	return &Service{cfg: cfg, reg: reg, contextLinesCache: -1}
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

// SummarizeDual generates both a short (3-sentence notification-safe) and a
// long (narrative paragraph) summary from text in a single LLM call.
// prevShort is the previous summary for this session; pass "" if none exists.
// The LLM is instructed not to repeat information already in prevShort.
// Returns ("", "", nil) when disabled or not configured.
func (s *Service) SummarizeDual(ctx context.Context, text string, prevShort string) (short, long string, err error) {
	if s.cfg == nil {
		return "", "", nil
	}
	sc := s.cfg.Session.Summarizer
	if !sc.Enabled || sc.LLMRef == "" {
		return "", "", nil
	}

	prefix := dualSummaryPrompt
	if strings.TrimSpace(prevShort) != "" {
		prefix = "Previously reported (do not repeat this):\n" + prevShort + "\n\n" + dualSummaryPrompt
	}
	fullPrompt := prefix + text

	// Apply 30-second timeout.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var raw string

	// Look up LLM entry in the registry.
	if s.reg != nil {
		llm, err := s.reg.Get(sc.LLMRef)
		if err == nil && llm != nil {
			switch llm.Kind {
			case inference.KindOllama:
				raw, err = s.callOllama(ctx, llm, fullPrompt)
			case "openai-api", "openai", inference.KindClaude:
				raw, err = s.callOpenAI(ctx, llm, fullPrompt)
			default:
				// For other kinds (openwebui, etc.) try Ollama protocol.
				raw, err = s.callOllama(ctx, llm, fullPrompt)
			}
			if err != nil {
				return "", "", err
			}
			short, long = parseDualSummary(raw)
			return short, long, nil
		}
	}

	// Fallback: use cfg.Ollama.Host if configured.
	if s.cfg.Ollama.Enabled && s.cfg.Ollama.Host != "" {
		raw, err = s.callOllamaRaw(ctx, s.cfg.Ollama.Host, s.cfg.Ollama.Model, fullPrompt)
		if err != nil {
			return "", "", err
		}
		short, long = parseDualSummary(raw)
		return short, long, nil
	}

	return "", "", fmt.Errorf("summarizer: LLM %q not found in registry", sc.LLMRef)
}

// ContextLines returns the recommended number of tmux history lines to
// capture for this model's context window. Queries Ollama /api/show once
// per service lifetime and caches the result.
func (s *Service) ContextLines() int {
	if s.contextLinesCache != -1 {
		return s.contextLinesCache
	}

	if s.cfg == nil {
		s.contextLinesCache = 200
		return s.contextLinesCache
	}
	sc := s.cfg.Session.Summarizer
	if !sc.Enabled || sc.LLMRef == "" {
		s.contextLinesCache = 200
		return s.contextLinesCache
	}

	if s.reg != nil {
		llm, err := s.reg.Get(sc.LLMRef)
		if err == nil && llm != nil {
			if llm.Kind != inference.KindOllama {
				s.contextLinesCache = 200
				return s.contextLinesCache
			}
			host := s.cfg.Ollama.Host
			if host == "" {
				host = "http://localhost:11434"
			}
			model := ""
			if s.cfg.Session.Summarizer.Model != "" {
				model = s.cfg.Session.Summarizer.Model
			} else if llm.Model != "" {
				model = llm.Model
			} else {
				model = s.cfg.Ollama.Model
			}
			contextLen := s.queryOllamaContextLen(host, model)
			s.contextLinesCache = contextLinesToHistoryLines(contextLen)
			return s.contextLinesCache
		}
	}

	s.contextLinesCache = 200
	return s.contextLinesCache
}

// queryOllamaContextLen POSTs to {host}/api/show and returns the model's
// context length. Returns 0 on any error.
func (s *Service) queryOllamaContextLen(host, model string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	body, _ := json.Marshal(map[string]string{"name": model})
	url := strings.TrimRight(host, "/") + "/api/show"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return 0
	}

	var out struct {
		Details struct {
			ContextLength int `json:"context_length"`
		} `json:"details"`
		Parameters string `json:"parameters"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0
	}

	// First try details.context_length.
	if out.Details.ContextLength > 0 {
		return out.Details.ContextLength
	}

	// Fall back to parsing parameters string for "num_ctx N".
	for _, line := range strings.Split(out.Parameters, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "num_ctx") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				if n, err := strconv.Atoi(parts[1]); err == nil {
					return n
				}
			}
		}
	}

	return 0
}

// contextLinesToHistoryLines maps a model context length to a recommended
// number of tmux scrollback lines to capture.
func contextLinesToHistoryLines(contextLen int) int {
	switch {
	case contextLen <= 0:
		return 200
	case contextLen < 8192:
		return 100
	case contextLen < 32768:
		return 200
	case contextLen < 131072:
		return 400
	default:
		return 600
	}
}

// parseDualSummary splits an LLM response containing ===SHORT=== and
// ===LONG=== markers into the two summary strings.
// If either marker is missing, the entire raw string is returned as short
// and long is empty.
func parseDualSummary(raw string) (short, long string) {
	const markerShort = "===SHORT==="
	const markerLong = "===LONG==="

	shortIdx := strings.Index(raw, markerShort)
	longIdx := strings.Index(raw, markerLong)

	if shortIdx == -1 || longIdx == -1 {
		return strings.TrimSpace(raw), ""
	}

	shortContent := raw[shortIdx+len(markerShort) : longIdx]
	longContent := raw[longIdx+len(markerLong):]

	return strings.TrimSpace(shortContent), strings.TrimSpace(longContent)
}

// callOllama calls the Ollama API using the LLM registry entry.
// If session.summarizer.model is set it takes priority over llm.Model.
func (s *Service) callOllama(ctx context.Context, llm *inference.LLM, prompt string) (string, error) {
	host := s.cfg.Ollama.Host
	if host == "" {
		host = "http://localhost:11434"
	}
	// Model priority: explicit summarizer override > LLM entry default > global Ollama default.
	model := ""
	if s.cfg.Session.Summarizer.Model != "" {
		model = s.cfg.Session.Summarizer.Model
	} else if llm.Model != "" {
		model = llm.Model
	} else {
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
