// Package aider implements the LLM backend for aider (https://aider.chat).
// Runs `aider --yes --message '<task>'` in a tmux session.
package aider

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/dmz006/datawatch/internal/llm"
)

// Backend runs aider in a tmux session.
type Backend struct{ binary string }

// New creates an aider backend. binary defaults to "aider".
func New(binary string) llm.Backend {
	if binary == "" {
		binary = "aider"
	}
	return &Backend{binary: binary}
}

func (b *Backend) Name() string                  { return "aider" }
func (b *Backend) SupportsInteractiveInput() bool { return false }
func (b *Backend) Version() string {
	out, err := exec.Command(b.binary, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
func (b *Backend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	escaped := strings.ReplaceAll(task, "'", `'\''`)
	cmd := fmt.Sprintf("cd '%s' && %s --yes --message '%s'; echo 'DATAWATCH_COMPLETE: aider done'",
		strings.ReplaceAll(projectDir, "'", `'\''`), b.binary, escaped)
	return exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, cmd, "Enter").Run()
}
