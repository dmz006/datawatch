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

const dualSummaryPrompt = `Analyze the following AI session terminal output. Your response must contain ONLY the two sections below, beginning immediately with the ===SHORT=== marker:

===SHORT===
Exactly 3 sentences, each under 15 words. Sentence 1: what was done. Sentence 2: did it succeed or fail. Sentence 3: what comes next or what the user last asked. No code, no markdown, no lists.

===LONG===
A narrative of 3-5 sentences (under 60 words each) covering: what was worked on, key decisions or findings, current status, any errors or blockers, and the last user request. Plain English only.

Do not add any text before ===SHORT=== or after the long summary.

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

// parseDualSummary splits an LLM response into short and long summaries.
// It strips <think>...</think> blocks (from reasoning models), then tries
// the primary ===SHORT===/ ===LONG=== markers, several common alternate
// formats, and finally falls back to a paragraph split so that even models
// that ignore the format still populate current_status_long.
func parseDualSummary(raw string) (short, long string) {
	raw = stripThinkTags(raw)

	// Primary and common alternate explicit-marker formats.
	markerPairs := [][2]string{
		{"===SHORT===", "===LONG==="},
		{"[SHORT]", "[LONG]"},
		{"**SHORT**", "**LONG**"},
	}
	for _, pair := range markerPairs {
		if s, l, ok := splitByMarkers(raw, pair[0], pair[1]); ok {
			return s, l
		}
	}

	// Line-header fallback: look for "SHORT" / "LONG" on their own lines
	// (strips surrounding whitespace, punctuation, markdown, ===, [], *).
	if s, l, ok := splitByLineHeaders(raw); ok {
		return s, l
	}

	// Blank-line paragraph split: first paragraph → short, remainder → long.
	parts := strings.SplitN(strings.TrimSpace(raw), "\n\n", 2)
	if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}

	// Sentence-split last resort: model didn't use any structural format.
	// Derive short from first 3 sentences; use full response as long so
	// current_status_long is always populated even for unstructured output.
	text := strings.TrimSpace(raw)
	if s := extractFirstNSentences(text, 3); s != "" && s != text {
		return s, text
	}
	return text, ""
}

// extractFirstNSentences returns the first n sentences from text by scanning
// for sentence-ending punctuation (. ! ?) followed by whitespace or end of string.
func extractFirstNSentences(text string, n int) string {
	count := 0
	runes := []rune(text)
	for i, r := range runes {
		if r == '.' || r == '!' || r == '?' {
			count++
			if count == n {
				// Include the punctuation mark itself.
				return strings.TrimSpace(string(runes[:i+1]))
			}
		}
	}
	return text
}

// stripThinkTags removes <think>...</think> blocks produced by reasoning
// models (qwen3, deepseek-r1, etc.) before parsing the dual-summary response.
func stripThinkTags(s string) string {
	for {
		start := strings.Index(s, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(s[start:], "</think>")
		if end == -1 {
			// Unclosed tag — drop everything from it onward.
			s = strings.TrimSpace(s[:start])
			break
		}
		s = s[:start] + s[start+end+len("</think>"):]
	}
	return strings.TrimSpace(s)
}

// splitByMarkers tries to split raw on an exact markerShort then markerLong pair.
func splitByMarkers(raw, markerShort, markerLong string) (short, long string, ok bool) {
	shortIdx := strings.Index(raw, markerShort)
	longIdx := strings.Index(raw, markerLong)
	if shortIdx == -1 || longIdx == -1 || longIdx <= shortIdx {
		return "", "", false
	}
	shortContent := raw[shortIdx+len(markerShort) : longIdx]
	longContent := raw[longIdx+len(markerLong):]
	return strings.TrimSpace(shortContent), strings.TrimSpace(longContent), true
}

// splitByLineHeaders looks for "SHORT" and "LONG" as sole content on a line
// (after stripping surrounding whitespace, = [ ] * # : characters).
// Handles formats like "## SHORT", "[SHORT]", "===SHORT===", "SHORT:", etc.
// Also handles inline forms where the label and content share a line:
// "SHORT: sentence one. ..." / "LONG: narrative ..." — content after the
// colon is extracted directly instead of from subsequent lines.
func splitByLineHeaders(raw string) (short, long string, ok bool) {
	lines := strings.Split(raw, "\n")
	shortStart, longStart := -1, -1
	shortInline, longInline := "", ""
	trimChars := " \t=-*#[]():."
	for i, line := range lines {
		trimmed := strings.ToLower(strings.Trim(line, trimChars))
		if shortStart == -1 {
			if trimmed == "short" {
				shortStart = i
			} else if s, found := extractInlineLabel(line, "short"); found {
				shortStart = i
				shortInline = s
			}
		} else if longStart == -1 {
			if trimmed == "long" {
				longStart = i
			} else if s, found := extractInlineLabel(line, "long"); found {
				longStart = i
				longInline = s
			}
		}
	}
	if shortStart == -1 || longStart == -1 || longStart <= shortStart {
		return "", "", false
	}

	var shortContent, longContent string
	if shortInline != "" {
		// Inline label: content starts on the label line; collect subsequent
		// lines up to the long marker and append them.
		extra := strings.Join(lines[shortStart+1:longStart], "\n")
		shortContent = shortInline
		if t := strings.TrimSpace(extra); t != "" {
			shortContent += "\n" + t
		}
	} else {
		shortContent = strings.Join(lines[shortStart+1:longStart], "\n")
	}
	if longInline != "" {
		extra := strings.Join(lines[longStart+1:], "\n")
		longContent = longInline
		if t := strings.TrimSpace(extra); t != "" {
			longContent += "\n" + t
		}
	} else {
		longContent = strings.Join(lines[longStart+1:], "\n")
	}

	if strings.TrimSpace(shortContent) == "" || strings.TrimSpace(longContent) == "" {
		return "", "", false
	}
	return strings.TrimSpace(shortContent), strings.TrimSpace(longContent), true
}

// extractInlineLabel checks if line matches "LABEL: content" (case-insensitive,
// with optional surrounding decoration stripped). Returns the content after the
// colon and true when matched.
func extractInlineLabel(line, label string) (content string, ok bool) {
	// Strip leading decoration characters before the label word.
	stripped := strings.TrimLeft(line, " \t=-*#[]()")
	// Check for "LABEL:" or "LABEL :" prefix, case-insensitive.
	upper := strings.ToUpper(label)
	lower := strings.ToLower(label)
	var rest string
	for _, prefix := range []string{upper + ":", lower + ":", strings.Title(lower) + ":"} { //nolint:staticcheck
		if strings.HasPrefix(stripped, prefix) {
			rest = strings.TrimSpace(stripped[len(prefix):])
			if rest != "" {
				return rest, true
			}
		}
	}
	return "", false
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
