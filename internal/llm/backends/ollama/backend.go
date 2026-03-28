// Package ollama implements the LLM backend for Ollama local models.
// It runs `ollama run <model> '<task>'` in a tmux session, optionally
// setting OLLAMA_HOST so requests go to a remote server.
package ollama

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

// Backend runs an Ollama model in a tmux session.
type Backend struct {
	model  string
	binary string
	host   string // OLLAMA_HOST base URL, e.g. "http://localhost:11434"
}

// New creates a new Ollama backend. binary defaults to "ollama", model defaults to "llama3".
func New(model, binary string) llm.Backend {
	return NewWithHost(model, binary, "")
}

// NewWithHost creates an Ollama backend with an explicit host URL.
func NewWithHost(model, binary, host string) llm.Backend {
	if binary == "" {
		binary = "ollama"
	}
	if model == "" {
		model = "llama3"
	}
	return &Backend{model: model, binary: binary, host: host}
}

func (b *Backend) Name() string                  { return "ollama" }
func (b *Backend) SupportsInteractiveInput() bool { return false }

func (b *Backend) Version() string {
	out, err := exec.Command(b.binary, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Launch sends the ollama run command into the tmux session.
func (b *Backend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	escaped := strings.ReplaceAll(task, "'", `'\''`)
	projEscaped := strings.ReplaceAll(projectDir, "'", `'\''`)
	hostEnv := ""
	if b.host != "" {
		hostEnv = fmt.Sprintf("OLLAMA_HOST=%s ", strings.ReplaceAll(b.host, "'", `'\''`))
	}
	cmd := fmt.Sprintf("cd '%s' && %s%s run %s '%s'; echo 'DATAWATCH_COMPLETE: ollama done'",
		projEscaped, hostEnv, b.binary, b.model, escaped)
	return exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, cmd, "Enter").Run()
}

// ListModels queries the Ollama HTTP API for available models.
// host defaults to http://localhost:11434 if empty.
func ListModels(host string) ([]string, error) {
	if host == "" {
		host = "http://localhost:11434"
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(host + "/api/tags")
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", host, err)
	}
	defer resp.Body.Close()
	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	names := make([]string, 0, len(result.Models))
	for _, m := range result.Models {
		names = append(names, m.Name)
	}
	return names, nil
}
