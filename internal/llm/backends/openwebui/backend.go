// Package openwebui implements the LLM backend for OpenWebUI (OpenAI-compatible API).
// It streams chat completions into a tmux session via curl.
package openwebui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/dmz006/datawatch/internal/llm"
)

// Backend calls the OpenWebUI OpenAI-compatible API from a tmux session via curl.
type Backend struct {
	baseURL string
	apiKey  string
	model   string
}

// New creates a new OpenWebUI backend.
func New(baseURL, apiKey, model string) llm.Backend {
	if baseURL == "" {
		baseURL = "http://localhost:3000"
	}
	if model == "" {
		model = "llama3"
	}
	return &Backend{baseURL: baseURL, apiKey: apiKey, model: model}
}

func (b *Backend) Name() string                  { return "openwebui" }
func (b *Backend) SupportsInteractiveInput() bool { return false }
func (b *Backend) Version() string               { return "" }

// Launch streams an OpenWebUI chat completion response into the tmux session.
func (b *Backend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	escaped := strings.ReplaceAll(task, `"`, `\"`)
	projEscaped := strings.ReplaceAll(projectDir, "'", `'\''`)

	authHeader := ""
	if b.apiKey != "" {
		authHeader = fmt.Sprintf(`-H 'Authorization: Bearer %s'`, b.apiKey)
	}

	payload := fmt.Sprintf(`{"model":"%s","messages":[{"role":"user","content":"%s"}],"stream":true}`,
		b.model, escaped)

	curlCmd := fmt.Sprintf(
		`cd '%s' && curl -s -N %s -H 'Content-Type: application/json' `+
			`-d '%s' '%s/api/chat/completions' `+
			`| python3 -c "import sys,json; [print(json.loads(l).get('choices',[{}])[0].get('delta',{}).get('content',''),end='',flush=True) for l in sys.stdin if l.startswith('data:') and l.strip() != 'data: [DONE]' for l in [l[5:].strip()]]"`+
			`; echo; echo 'DATAWATCH_COMPLETE: openwebui done'`,
		projEscaped, authHeader, payload, b.baseURL)

	return exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, curlCmd, "Enter").Run()
}
