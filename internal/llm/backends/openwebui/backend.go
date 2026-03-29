// Package openwebui implements the LLM backend for OpenWebUI (OpenAI-compatible API).
// It streams chat completions into a tmux session via curl.
package openwebui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

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
func (b *Backend) PromptRequired() bool           { return true }
func (b *Backend) Version() string {
	if b.baseURL == "" || b.apiKey == "" {
		return ""
	}
	// Probe /api/v1/models to check connectivity
	req, _ := http.NewRequest("GET", b.baseURL+"/api/v1/models", nil)
	if b.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+b.apiKey)
	}
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil || resp.StatusCode != 200 {
		return ""
	}
	resp.Body.Close()
	return b.baseURL
}

// ListModels queries the OpenWebUI/OpenAI-compatible API for available models.
func ListModels(baseURL, apiKey string) ([]string, error) {
	if baseURL == "" {
		baseURL = "http://localhost:3000"
	}
	req, err := http.NewRequest("GET", baseURL+"/api/v1/models", nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", baseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API returned %d", resp.StatusCode)
	}
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	names := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		names = append(names, m.ID)
	}
	return names, nil
}

// Launch streams an OpenWebUI chat completion response into the tmux session.
func (b *Backend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	if task == "" {
		return fmt.Errorf("openwebui requires a prompt (single-shot API call)")
	}
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
