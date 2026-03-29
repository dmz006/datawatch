// Package opencode implements the LLM backend for opencode (https://github.com/sst/opencode).
package opencode

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	return &Backend{binary: resolveBinary(binary)}
}

// resolveBinary returns the given binary if it's an absolute path or found in PATH,
// otherwise checks common install locations.
func resolveBinary(binary string) string {
	if filepath.IsAbs(binary) {
		return binary
	}
	if _, err := exec.LookPath(binary); err == nil {
		return binary
	}
	// Check common install locations
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".opencode", "bin", binary),
		filepath.Join(home, ".local", "bin", binary),
		filepath.Join("/usr/local/bin", binary),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return binary
}

func (b *Backend) Name() string                  { return "opencode" }
func (b *Backend) SupportsInteractiveInput() bool { return false }
func (b *Backend) PromptRequired() bool           { return false }

// PromptBackend runs opencode in single-prompt mode (-p). Always requires a task.
type PromptBackend struct{ binary string }

// NewPrompt creates an opencode-prompt backend that runs opencode -p.
func NewPrompt(binary string) llm.Backend {
	if binary == "" {
		binary = "opencode"
	}
	return &PromptBackend{binary: resolveBinary(binary)}
}

func (b *PromptBackend) Name() string                  { return "opencode-prompt" }
func (b *PromptBackend) SupportsInteractiveInput() bool { return false }
func (b *PromptBackend) PromptRequired() bool           { return true }
func (b *PromptBackend) Version() string {
	out, err := exec.Command(b.binary, "--version").Output()
	if err != nil { return "" }
	return strings.TrimSpace(string(out))
}

func (b *PromptBackend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	if task == "" {
		return fmt.Errorf("opencode-prompt requires a task (prompt mode)")
	}
	escapedDir := strings.ReplaceAll(projectDir, "'", `'\''`)
	escaped := strings.ReplaceAll(task, "'", `'\''`)
	cmd := fmt.Sprintf("cd '%s' && %s run '%s'; echo 'DATAWATCH_COMPLETE: opencode done'",
		escapedDir, b.binary, escaped)
	return exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, cmd, "Enter").Run()
}
func (b *Backend) Version() string {
	out, err := exec.Command(b.binary, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
func (b *Backend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	escapedDir := strings.ReplaceAll(projectDir, "'", `'\''`)
	var cmd string
	if task == "" {
		// Interactive TUI mode when no task given
		cmd = fmt.Sprintf("cd '%s' && %s", escapedDir, b.binary)
	} else {
		// Non-interactive run mode with task
		escaped := strings.ReplaceAll(task, "'", `'\''`)
		cmd = fmt.Sprintf("cd '%s' && %s run '%s'; echo 'DATAWATCH_COMPLETE: opencode done'",
			escapedDir, b.binary, escaped)
	}
	return exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, cmd, "Enter").Run()
}

// LaunchResume resumes a prior opencode session using -s SESSION_ID.
func (b *Backend) LaunchResume(ctx context.Context, task, tmuxSession, projectDir, logFile, resumeID string) error {
	escapedDir := strings.ReplaceAll(projectDir, "'", `'\''`)
	resumeEsc := strings.ReplaceAll(resumeID, "'", `'\''`)
	var cmd string
	if task == "" {
		// Interactive resume — just open the session in TUI
		cmd = fmt.Sprintf("cd '%s' && %s -s '%s'", escapedDir, b.binary, resumeEsc)
	} else {
		escaped := strings.ReplaceAll(task, "'", `'\''`)
		cmd = fmt.Sprintf("cd '%s' && %s -s '%s' -p '%s'; echo 'DATAWATCH_COMPLETE: opencode done'",
			escapedDir, b.binary, resumeEsc, escaped)
	}
	return exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, cmd, "Enter").Run()
}
