// Package ollama implements the LLM backend for Ollama local models.
// It runs `ollama run <model> '<task>'` in a tmux session.
package ollama

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/dmz006/datawatch/internal/llm"
)

// Backend runs an Ollama model in a tmux session.
type Backend struct {
	model  string
	binary string
}

// New creates a new Ollama backend. binary defaults to "ollama", model defaults to "llama3".
func New(model, binary string) llm.Backend {
	if binary == "" {
		binary = "ollama"
	}
	if model == "" {
		model = "llama3"
	}
	return &Backend{model: model, binary: binary}
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
	cmd := fmt.Sprintf("cd '%s' && %s run %s '%s'; echo 'DATAWATCH_COMPLETE: ollama done'",
		projEscaped, b.binary, b.model, escaped)
	return exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, cmd, "Enter").Run()
}
