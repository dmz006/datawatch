// Package gemini implements the LLM backend for Google's Gemini CLI.
package gemini

import (
	"context"
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
func (b *Backend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	escaped := strings.ReplaceAll(task, "'", `'\''`)
	cmd := fmt.Sprintf("cd '%s' && %s -p '%s'; echo 'DATAWATCH_COMPLETE: gemini done'",
		strings.ReplaceAll(projectDir, "'", `'\''`), b.binary, escaped)
	return exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, cmd, "Enter").Run()
}
