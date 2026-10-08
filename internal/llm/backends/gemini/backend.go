// Package gemini implements the LLM backend for Google's Gemini CLI.
package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/dmz006/datawatch/internal/llm"
)

// Backend runs gemini CLI in a tmux session.
type Backend struct{ binary string }

// New creates a gemini backend. binary defaults to "gemini".
func New(binary string) llm.Backend {
	if binary == "" {
		binary = "gemini"
	}
	return &Backend{binary: binary}
}

func (b *Backend) Name() string                  { return "gemini" }
func (b *Backend) SupportsInteractiveInput() bool { return false }
func (b *Backend) Version() string {
	out, err := exec.Command(b.binary, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// B98 — usageFn reports real per-turn token usage so the session's
// running TokensIn/TokensOut/EstCostUSD counters (internal/session/
// cost.go) actually populate for gemini-backed sessions. gemini has no
// side-channel export the way claude-code/opencode/goose do, and its
// default text-mode output has no usage footer the way aider's does
// (confirmed by searching the installed @google/gemini-cli package's
// own bundle for a non-streaming "stats" print in text mode -- none
// exists). The only usage-bearing mode is `--output-format json`, which
// is why Launch below is Go-mediated (like openwebui's backend) instead
// of a raw shell invocation: it runs gemini with -o json, parses the
// structured result, and re-prints just the response text to the pane
// itself, rather than dumping raw JSON into the tmux transcript.
var usageFn func(tmuxSession string, tokensIn, tokensOut int)

// SetUsageFn registers the callback for per-turn token usage.
func SetUsageFn(fn func(tmuxSession string, tokensIn, tokensOut int)) {
	usageFn = fn
}

// jsonResult is gemini CLI's `--output-format json` top-level shape,
// confirmed directly from the installed package's own JsonFormatter.format()
// source (packages/cli JsonFormatter class): {session_id, response, stats,
// error, warnings}. Live-verified end to end for the error path only (no
// Google Cloud credentials were available in the environment this was
// written in -- GEMINI_API_KEY/GOOGLE_GENAI_USE_VERTEXAI/GOOGLE_GENAI_USE_GCA
// all unset produces a clean {"session_id":...,"error":{...}} response,
// confirming this exact shape and that session_id is present even on
// failure). The success-path "response"/"stats" fields are read from the
// same formatter's source, not guessed, but have NOT been exercised
// against a real successful call -- re-verify once real credentials are
// available.
type jsonResult struct {
	SessionID string          `json:"session_id"`
	Response  string          `json:"response"`
	Stats     json.RawMessage `json:"stats"`
	Error     *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
}

// extractUsage reads only a flat, top-level pair of token-count fields
// from stats, trying both the snake_case and camelCase spellings seen
// elsewhere in gemini-cli's own source (StreamJsonFormatter's
// convertToStreamStats produces inputTokens/outputTokens at its top
// level alongside a separate per-model "models" breakdown of the same
// numbers). Deliberately does NOT recurse into nested objects like
// "models": that breakdown and the aggregate total both exist in the
// same object, and a generic tree-wide scan would double-count by
// matching both. Safer to extract nothing (0, 0) than to guess at a
// nested shape never confirmed against a real response.
func extractUsage(stats json.RawMessage) (tokensIn, tokensOut int) {
	if len(stats) == 0 {
		return 0, 0
	}
	var flat map[string]float64
	if json.Unmarshal(stats, &flat) != nil {
		return 0, 0
	}
	for _, k := range []string{"input_tokens", "inputTokens", "prompt_tokens", "promptTokenCount"} {
		if v, ok := flat[k]; ok {
			tokensIn = int(v)
			break
		}
	}
	for _, k := range []string{"output_tokens", "outputTokens", "candidates_tokens", "candidatesTokenCount"} {
		if v, ok := flat[k]; ok {
			tokensOut = int(v)
			break
		}
	}
	return
}

// printLines writes text to the tmux pane one line at a time via printf,
// same mechanism openwebui's sendAndStream uses to avoid send-keys
// executing arbitrary pane content as shell commands.
func printLines(tmuxSession, text string) {
	for _, line := range strings.Split(text, "\n") {
		escaped := strings.ReplaceAll(line, "'", `'\''`)
		_ = exec.Command("tmux", "send-keys", "-t", tmuxSession,
			fmt.Sprintf("printf '%%s\\n' '%s'", escaped), "Enter").Run()
	}
}

func (b *Backend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	displayTask := task
	if len(displayTask) > 200 {
		displayTask = displayTask[:197] + "..."
	}
	_ = exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession,
		fmt.Sprintf("echo '[gemini] %s'", strings.ReplaceAll(displayTask, "'", "'\\''")), "Enter").Run()

	go func() {
		cmd := exec.CommandContext(ctx, b.binary, "-p", task, "--output-format", "json")
		cmd.Dir = projectDir
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		runErr := cmd.Run()

		// Live-verified: on error, gemini writes its JSON result to
		// stderr, not stdout (stdout is empty) -- presumably so a
		// successful machine-readable result can be safely piped on
		// stdout while diagnostics go to stderr. Try stdout first (the
		// documented success path per JsonFormatter.format()'s source),
		// falling back to stderr for the confirmed error case.
		var result jsonResult
		parseErr := json.Unmarshal(stdout.Bytes(), &result)
		if parseErr != nil {
			parseErr = json.Unmarshal(stderr.Bytes(), &result)
		}
		switch {
		case parseErr != nil:
			printLines(tmuxSession, fmt.Sprintf("[gemini] error: failed to parse output (%v)\nstdout: %s\nstderr: %s", runErr, stdout.String(), stderr.String()))
		case result.Error != nil:
			printLines(tmuxSession, fmt.Sprintf("[gemini] error: %s", result.Error.Message))
		default:
			printLines(tmuxSession, result.Response)
			if usageFn != nil {
				if in, out := extractUsage(result.Stats); in > 0 || out > 0 {
					usageFn(tmuxSession, in, out)
				}
			}
		}

		exec.Command("tmux", "send-keys", "-t", tmuxSession, "echo 'DATAWATCH_COMPLETE: gemini done'", "Enter").Run() //nolint:errcheck
	}()

	return nil
}
