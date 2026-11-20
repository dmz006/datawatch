package session

import (
	"os"
	"regexp"
	"strconv"
	"strings"
)

// LocalUsage is what a local-model backend (pi, ollama) reported for a session.
// Reported is false when the backend gave no token counts at all.
type LocalUsage struct {
	TokensIn  int64
	TokensOut int64
	ToolCalls int64
	Reported  bool
}

// LocalUsageReader reads local backend usage for a session. Tests inject fakes.
type LocalUsageReader interface {
	ReadLocalUsage(backendFamily, logFile string) LocalUsage
}

// IsLocalBackend reports whether a backend family is a local-model family
// (pi*, ollama*) whose tokens must never be mixed with or priced as Claude's.
func IsLocalBackend(family string) bool {
	f := strings.ToLower(family)
	return f == "ollama" || f == "pi" || strings.HasPrefix(f, "pi-") || strings.HasPrefix(f, "ollama-")
}

// LogUsageReader is the default reader: it scans the session output log.
type LogUsageReader struct{}

var (
	reOllamaPrompt = regexp.MustCompile(`"prompt_eval_count"\s*:\s*(\d+)`)
	reOllamaEval   = regexp.MustCompile(`"eval_count"\s*:\s*(\d+)`)
	rePiInput      = regexp.MustCompile(`"(?:input|prompt_tokens|inputTokens)"\s*:\s*(\d+)`)
	rePiOutput     = regexp.MustCompile(`"(?:output|completion_tokens|outputTokens)"\s*:\s*(\d+)`)
	reToolCall     = regexp.MustCompile(`"type"\s*:\s*"(?:toolCall|tool_call|tool_use|tool_execution_start)"`)
)

func sumMatches(re *regexp.Regexp, s string) (int64, bool) {
	var total int64
	found := false
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		if n, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			total += n
			found = true
		}
	}
	return total, found
}

// ParseLocalUsage extracts counts from backend output text.
func ParseLocalUsage(family, text string) LocalUsage {
	var u LocalUsage
	var inOK, outOK bool
	if strings.HasPrefix(strings.ToLower(family), "ollama") {
		u.TokensIn, inOK = sumMatches(reOllamaPrompt, text)
		u.TokensOut, outOK = sumMatches(reOllamaEval, text)
	} else {
		u.TokensIn, inOK = sumMatches(rePiInput, text)
		u.TokensOut, outOK = sumMatches(rePiOutput, text)
	}
	u.Reported = inOK || outOK
	u.ToolCalls = int64(len(reToolCall.FindAllString(text, -1)))
	return u
}

// ReadLocalUsage implements LocalUsageReader.
func (LogUsageReader) ReadLocalUsage(family, logFile string) LocalUsage {
	if logFile == "" {
		return LocalUsage{}
	}
	b, err := os.ReadFile(logFile)
	if err != nil {
		return LocalUsage{}
	}
	return ParseLocalUsage(family, string(b))
}
