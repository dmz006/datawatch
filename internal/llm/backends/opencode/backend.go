// Package opencode implements the LLM backend for opencode (https://github.com/sst/opencode).
package opencode

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/dmz006/datawatch/internal/llm"
)

// Backend runs opencode in a tmux session.
type Backend struct{ binary string }

// New creates an opencode backend. binary defaults to "opencode".
func New(binary string) llm.Backend {
	if binary == "" {
		binary = "opencode"
	}
	return &Backend{binary: binary}
}

func (b *Backend) Name() string                  { return "opencode" }
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
	// opencode supports -p/--print for non-interactive mode
	cmd := fmt.Sprintf("cd '%s' && %s -p '%s'; echo 'DATAWATCH_COMPLETE: opencode done'",
		strings.ReplaceAll(projectDir, "'", `'\''`), b.binary, escaped)
	return exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, cmd, "Enter").Run()
}

// LaunchResume resumes a prior opencode session using -s SESSION_ID.
func (b *Backend) LaunchResume(ctx context.Context, task, tmuxSession, projectDir, logFile, resumeID string) error {
	escaped := strings.ReplaceAll(task, "'", `'\''`)
	resumeEsc := strings.ReplaceAll(resumeID, "'", `'\''`)
	cmd := fmt.Sprintf("cd '%s' && %s -s '%s' -p '%s'; echo 'DATAWATCH_COMPLETE: opencode done'",
		strings.ReplaceAll(projectDir, "'", `'\''`), b.binary, resumeEsc, escaped)
	return exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, cmd, "Enter").Run()
}
