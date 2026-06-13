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
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
)

// DefaultSummarizerPrompt is the default system prompt for the summarizer.
// Tests reference this constant directly so the expected prompt stays in sync
// with whatever the service uses.
const DefaultSummarizerPrompt = "Compress the following AI coding assistant output into exactly 3 short sentences (under 15 words each) suitable for a car dashboard or phone notification. Sentence 1: what was done. Sentence 2: did it succeed or fail. Sentence 3: what comes next. No code, no markdown, no bullet points."

const dualSummaryPrompt = `Summarize the terminal session below in plain spoken English for a car dashboard.

Begin with ===SHORT=== on its own line. Write exactly 3 plain English sentences (each under 15 words): what task was worked on, whether it succeeded or failed, what comes next.
Then write ===LONG=== on its own line. Write 3 to 5 plain English sentences describing what was done and the outcome in more detail.

Critical rules — these apply to BOTH sections:
- Write ONLY plain English words a non-programmer would understand
- Do NOT copy file names, function names, variable names, test names, or identifiers from the output
- Do NOT include error codes, line numbers, or paths like "manager.go:145"
- Do NOT use code terms like "undefined", "panic", "goroutine", "exit status"
- Describe actions in human terms: "the code was fixed" not "manager.go:1304 was patched"
- No markdown, no bullet points, no numbered lists, no code blocks

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

	// Cap input to the most recent 6000 characters so small models don't
	// get overwhelmed by long session logs; the tail is most relevant.
	const maxInputChars = 6000
	inputText := text
	if len(inputText) > maxInputChars {
		// Trim to the most recent portion, starting at a line boundary.
		trimmed := inputText[len(inputText)-maxInputChars:]
		if idx := strings.IndexByte(trimmed, '\n'); idx >= 0 {
			trimmed = trimmed[idx+1:]
		}
		inputText = trimmed
	}

	// Only include prevShort when it differs significantly from the text the
	// model will see — prevents the feedback loop where the model copies prevShort
	// verbatim and it becomes the next prevShort, repeating indefinitely.
	prev := strings.TrimSpace(prevShort)
	prefix := dualSummaryPrompt
	if prev != "" && !strings.Contains(strings.ToLower(inputText), strings.ToLower(prev[:min(len(prev), 40)])) {
		prefix = "Previously summarized (do not repeat):\n" + prev + "\n\n" + dualSummaryPrompt
	}
	fullPrompt := prefix + inputText

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
			log.Printf("[summarizer] dual input_len=%d raw=%q short=%q long_len=%d",
				len(text), truncateLog(raw, 120), truncateLog(short, 80), len(long))
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
		log.Printf("[summarizer] dual input_len=%d raw=%q short=%q long_len=%d",
			len(text), truncateLog(raw, 120), truncateLog(short, 80), len(long))
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

// truncateLog returns s truncated to maxLen runes with "…" appended if cut.
func truncateLog(s string, maxLen int) string {
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen]) + "…"
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
			if capped := extractFirstNSentences(s, 3); capped != "" {
				s = capped
			}
			// When LONG section is empty (model wrote nothing after the marker),
			// fall back to using the short text as the long so current_status_long
			// is always populated.
			if l == "" {
				l = strings.TrimSpace(s)
			}
			return cleanShort(s), sanitizeForSpeech(l)
		}
	}

	// Line-header fallback: look for "SHORT" / "LONG" on their own lines
	// (strips surrounding whitespace, punctuation, markdown, ===, [], *).
	if s, l, ok := splitByLineHeaders(raw); ok {
		if capped := extractFirstNSentences(s, 3); capped != "" {
			s = capped
		}
		return cleanShort(s), sanitizeForSpeech(l)
	}

	// Blank-line paragraph split: first paragraph → short, remainder → long.
	// The short is capped at 3 sentences in case the model wrote a long
	// single-paragraph intro instead of using the marker format.
	parts := strings.SplitN(strings.TrimSpace(raw), "\n\n", 2)
	if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
		shortPart := strings.TrimSpace(parts[0])
		if s := extractFirstNSentences(shortPart, 3); s != "" && s != shortPart {
			shortPart = s
		}
		return cleanShort(shortPart), sanitizeForSpeech(strings.TrimSpace(parts[1]))
	}

	// Sentence-split last resort: model didn't use any structural format.
	// Derive short from first 3 sentences; use full response as long so
	// current_status_long is always populated even for unstructured output.
	text := strings.TrimSpace(raw)
	if s := extractFirstNSentences(text, 3); s != "" && s != text {
		return cleanShort(s), sanitizeForSpeech(text)
	}
	return cleanShort(text), ""
}

// cleanShort strips markdown artifacts and template labels from a short summary,
// producing plain text safe for Android Auto TTS and mobile notifications.
func cleanShort(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	var kept []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Drop pure horizontal-rule / separator lines ("---", "===", "***", etc.).
		if isSeparatorLine(trimmed) {
			continue
		}
		// Drop section-marker lines like "===SHORT===", "===LONG===".
		if strings.HasPrefix(trimmed, "===") && strings.HasSuffix(trimmed, "===") {
			continue
		}
		// Strip leading markdown heading (#), blockquote (>), and bullet (- * +) chars.
		stripped := strings.TrimLeft(line, " \t")
		stripped = strings.TrimLeft(stripped, "#>")
		stripped = strings.TrimSpace(stripped)
		// Strip single leading bullet character followed by a space.
		if len(stripped) >= 2 && (stripped[0] == '-' || stripped[0] == '*' || stripped[0] == '+') && stripped[1] == ' ' {
			stripped = stripped[2:]
		}
		// Strip leading numbered list prefixes: "1. ", "2. ", etc.
		if len(stripped) >= 3 && stripped[0] >= '0' && stripped[0] <= '9' && stripped[1] == '.' && stripped[2] == ' ' {
			stripped = stripped[3:]
		} else if len(stripped) >= 4 && stripped[0] >= '0' && stripped[0] <= '9' && stripped[1] >= '0' && stripped[1] <= '9' && stripped[2] == '.' && stripped[3] == ' ' {
			stripped = stripped[4:]
		}
		kept = append(kept, strings.TrimSpace(stripped))
	}
	result := strings.TrimSpace(strings.Join(kept, " "))

	// Strip backticks — TTS reads them as "backtick", ruining car/phone audio.
	result = strings.ReplaceAll(result, "`", "")

	// Strip bold/italic markdown decorators: **word** → word, __word__ → word,
	// *word* → word, _word_ → word. Replace pairs first (longer patterns win).
	for _, pair := range []string{"**", "__"} {
		result = stripMarkdownPair(result, pair)
	}

	// Strip outer [...] wrapping — models that treat [placeholder] as a template
	// to fill in sometimes wrap the entire short in brackets.
	if len(result) > 2 && result[0] == '[' && result[len(result)-1] == ']' {
		result = strings.TrimSpace(result[1 : len(result)-1])
	}
	// Strip common template labels that models copy from prompt format specs.
	for _, label := range []string{
		"What happened: ", "What happened:",
		"Did it succeed or fail: ", "Did it succeed or fail:",
		"What comes next: ", "What comes next:",
		"Sentence 1: ", "Sentence 2: ", "Sentence 3: ",
	} {
		result = strings.TrimPrefix(result, label)
	}

	// Final pass: remove technical artifacts (file paths, URLs, error codes)
	// that small LLMs sometimes copy verbatim from the session output.
	return sanitizeForSpeech(result)
}

// stripMarkdownPair removes surrounding marker pairs (e.g. "**" or "__") from
// a string. "**hello**" → "hello". Unpaired markers are left untouched.
func stripMarkdownPair(s, marker string) string {
	mlen := len(marker)
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if i+mlen <= len(s) && s[i:i+mlen] == marker {
			// Look ahead for a closing marker.
			j := strings.Index(s[i+mlen:], marker)
			if j >= 0 {
				// Write content between the markers, skip both markers.
				b.WriteString(s[i+mlen : i+mlen+j])
				i = i + mlen + j + mlen
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// reFilePath matches file paths: tokens containing at least one "/" with a
// file-extension-like suffix, or colon-number suffixes like "file.go:145:3".
var reFilePath = regexp.MustCompile(`\S+/\S+\.\w+(?::\d+)*|\S+\.\w{1,4}:\d+`)

// reErrorCode matches standalone error-code patterns: "1304:3", "0x1abc", "N:M".
var reErrorCode = regexp.MustCompile(`\b\d+:\d+\b|\b0x[0-9a-fA-F]{4,}\b`)

// reURL matches http(s):// URLs.
var reURL = regexp.MustCompile(`https?://\S+`)

// sanitizeForSpeech removes technical artifacts from summary text that would
// sound wrong when read aloud by TTS (Android Auto, phone notifications).
// It strips file paths, URLs, error codes, and code-like tokens. It is applied
// to both the short and long sections after parseDualSummary extracts them.
// The function works line-by-line, preserving paragraph structure.
func sanitizeForSpeech(s string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	var kept []string
	for _, line := range lines {
		// Apply pattern removals per-line so newlines are never collapsed.
		cleaned := reURL.ReplaceAllString(line, "")
		cleaned = reFilePath.ReplaceAllString(cleaned, "")
		cleaned = reErrorCode.ReplaceAllString(cleaned, "")
		// Collapse multiple spaces introduced by removals within this line.
		cleaned = strings.Join(strings.Fields(cleaned), " ")

		if cleaned == "" {
			kept = append(kept, "")
			continue
		}
		// Count alphabetic runes vs total runes in the cleaned line.
		total, alpha := 0, 0
		for _, r := range cleaned {
			total++
			if unicode.IsLetter(r) {
				alpha++
			}
		}
		// Keep lines where at least 55% of characters are letters.
		// This drops pure-code remnants (lone "{", "}}", "::", "->", "=>")
		// while preserving normal prose.
		if total > 0 && float64(alpha)/float64(total) >= 0.55 {
			kept = append(kept, cleaned)
		}
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// isSeparatorLine reports whether a line consists only of decoration characters
// (horizontal rule, marker fence, etc.) with no actual content.
func isSeparatorLine(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r != '-' && r != '=' && r != '*' && r != '_' && r != '~' && r != ' ' && r != '\t' {
			return false
		}
	}
	return true
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

// ollamaChatSystemPrompt is the system message injected when using the chat
// endpoint. A system message is more forceful than a user prompt for small
// models — they're trained to follow system instructions strictly.
const ollamaChatSystemPrompt = `You are a car-dashboard session summarizer. You translate AI coding session terminal output into plain spoken English for a driver. Write as if explaining to a non-technical person.

Respond with ONLY this format:

===SHORT===
Three plain English sentences, each under 15 words, suitable for reading aloud in a car.
Sentence 1: describe what computing task was worked on (e.g. "The team fixed a bug in the login system").
Sentence 2: say whether the work succeeded or failed.
Sentence 3: say what comes next.
===LONG===
Three to five plain English sentences with more context about what was done and the outcome.

Strict rules — violations make the summary unusable on a car display:
- Use ONLY plain English words. No technical jargon.
- NEVER copy file names, function names, variable names, test names, or code identifiers.
- NEVER include file paths (no slashes), line numbers, error codes, or hex values.
- NEVER use words like: undefined, panic, goroutine, nil, stdout, stderr, FAIL, PASS, exit.
- Describe technology work in human terms: "the build succeeded", "the tests passed", "a bug was fixed".
- Start immediately with ===SHORT===. No preamble, no explanation, no labels.`

// callOllamaRaw sends to {host}/api/chat (preferred) with a strict system
// message, falling back to /api/generate if chat is unavailable.
// For qwen3 and deepseek-r reasoning models, think:false suppresses
// chain-of-thought so the model replies directly without <think> blocks.
func (s *Service) callOllamaRaw(ctx context.Context, host, model, prompt string) (string, error) {
	if model == "" {
		return "", fmt.Errorf("summarizer: no model configured for Ollama")
	}
	isThinkingModel := strings.Contains(strings.ToLower(model), "qwen3") ||
		strings.Contains(strings.ToLower(model), "deepseek-r")

	// Use chat endpoint with a system message — gives much better instruction
	// following on small models compared to /api/generate single-turn prompts.
	type chatMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	chatPayload := map[string]interface{}{
		"model": model,
		"messages": []chatMessage{
			{Role: "system", Content: ollamaChatSystemPrompt},
			{Role: "user", Content: prompt},
		},
		"stream": false,
	}
	if isThinkingModel {
		// think:false disables chain-of-thought for qwen3/deepseek-r, reducing
		// latency and preventing <think> blocks from polluting the output.
		chatPayload["think"] = false
	}
	body, _ := json.Marshal(chatPayload)
	chatURL := strings.TrimRight(host, "/") + "/api/chat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("summarizer: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err == nil && resp.StatusCode == http.StatusOK {
		defer resp.Body.Close() //nolint:errcheck
		var out struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		if decErr := json.NewDecoder(resp.Body).Decode(&out); decErr == nil {
			return strings.TrimSpace(out.Message.Content), nil
		}
		resp.Body.Close() //nolint:errcheck
	} else if resp != nil {
		resp.Body.Close() //nolint:errcheck
	}

	// Fallback: /api/generate (older Ollama or chat unavailable).
	genPayload := map[string]interface{}{
		"model":  model,
		"prompt": ollamaChatSystemPrompt + "\n\n" + prompt,
		"stream": false,
	}
	if isThinkingModel {
		genPayload["think"] = false
	}
	body, _ = json.Marshal(genPayload)
	genURL := strings.TrimRight(host, "/") + "/api/generate"
	req2, err := http.NewRequestWithContext(ctx, http.MethodPost, genURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("summarizer: build request: %w", err)
	}
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		return "", fmt.Errorf("summarizer: ollama request: %w", err)
	}
	defer resp2.Body.Close() //nolint:errcheck
	if resp2.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(io.LimitReader(resp2.Body, 512))
		return "", fmt.Errorf("summarizer: ollama HTTP %d: %s", resp2.StatusCode, string(buf))
	}
	var out2 struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&out2); err != nil {
		return "", fmt.Errorf("summarizer: ollama decode: %w", err)
	}
	return strings.TrimSpace(out2.Response), nil
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
