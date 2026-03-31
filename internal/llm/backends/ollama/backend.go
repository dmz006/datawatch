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
	// When a remote host is configured, probe the HTTP API instead of the local binary —
	// the binary may not be installed on this machine.
	if b.host != "" {
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(b.host + "/api/tags")
		if err != nil || resp.StatusCode != http.StatusOK {
			return ""
		}
		resp.Body.Close()
		return "remote:" + b.host
	}
	out, err := exec.Command(b.binary, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Launch sends the ollama run command into the tmux session.
func (b *Backend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	projEscaped := strings.ReplaceAll(projectDir, "'", `'\''`)
	hostEnv := ""
	if b.host != "" {
		hostEnv = fmt.Sprintf("OLLAMA_HOST=%s ", strings.ReplaceAll(b.host, "'", `'\''`))
	}
	// Always start in interactive mode — send task as first prompt after launch.
	// ollama shows ">>> " prompt when ready for input.
	cmd := fmt.Sprintf("cd '%s' && %s%s run %s",
		projEscaped, hostEnv, b.binary, b.model)
	if err := exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, cmd, "Enter").Run(); err != nil {
		return err
	}
	// If a task was provided, send it after a brief delay for the model to load
	if task != "" {
		go func() {
			time.Sleep(2 * time.Second)
			escaped := strings.ReplaceAll(task, "'", `'\''`)
			_ = exec.Command("tmux", "send-keys", "-t", tmuxSession, escaped, "Enter").Run()
		}()
	}
	return nil
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
