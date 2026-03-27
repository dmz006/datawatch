// Package goose implements the LLM backend for Block's goose agent (https://github.com/block/goose).
package goose

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/dmz006/datawatch/internal/llm"
)

// Backend runs goose in a tmux session.
type Backend struct{ binary string }

// New creates a goose backend. binary defaults to "goose".
func New(binary string) llm.Backend {
	if binary == "" {
		binary = "goose"
	}
	return &Backend{binary: binary}
}

func (b *Backend) Name() string                  { return "goose" }
func (b *Backend) SupportsInteractiveInput() bool { return false }
func (b *Backend) Version() string {
	out, err := exec.Command(b.binary, "version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
func (b *Backend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	escaped := strings.ReplaceAll(task, "'", `'\''`)
	cmd := fmt.Sprintf("cd '%s' && %s run --text '%s'; echo 'DATAWATCH_COMPLETE: goose done'",
		strings.ReplaceAll(projectDir, "'", `'\''`), b.binary, escaped)
	return exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, cmd, "Enter").Run()
}
