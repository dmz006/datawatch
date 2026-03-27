// Package claudecode implements the LLM backend for Anthropic's claude-code CLI.
package claudecode

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/dmz006/datawatch/internal/llm"
)

func init() {
	llm.Register(New("claude"))
}

// Backend runs claude-code in a tmux session.
type Backend struct {
	binaryPath         string
	skipPermissions    bool // pass --dangerously-skip-permissions
}

// New creates a claude-code backend. binaryPath defaults to "claude".
func New(binaryPath string) llm.Backend {
	if binaryPath == "" {
		binaryPath = "claude"
	}
	return &Backend{binaryPath: binaryPath}
}

// NewWithOptions creates a claude-code backend with options.
func NewWithOptions(binaryPath string, skipPermissions bool) llm.Backend {
	if binaryPath == "" {
		binaryPath = "claude"
	}
	return &Backend{binaryPath: binaryPath, skipPermissions: skipPermissions}
}

func (b *Backend) Name() string                  { return "claude-code" }
func (b *Backend) SupportsInteractiveInput() bool { return true }

func (b *Backend) Version() string {
	out, err := exec.Command(b.binaryPath, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Launch sends the claude command into the tmux session, running in projectDir.
// It uses --add-dir to grant claude-code permission to the project directory.
// Set NO_COLOR=1 so output is clean text without ANSI escape sequences.
func (b *Backend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	// Build command: cd to project dir, then run claude with task.
	// NO_COLOR=1 disables color output for cleaner log files and messaging.
	escaped := escapeForShell(task)

	var flags string
	if b.skipPermissions {
		flags = " --dangerously-skip-permissions"
	}

	cmd := fmt.Sprintf("cd %s && NO_COLOR=1 %s --add-dir %s%s '%s'",
		shellQuote(projectDir), b.binaryPath, shellQuote(projectDir), flags, escaped)

	err := exec.CommandContext(ctx,
		"tmux", "send-keys", "-t", tmuxSession, cmd, "Enter",
	).Run()
	if err != nil {
		return fmt.Errorf("launch claude-code in %s: %w", tmuxSession, err)
	}
	return nil
}

func escapeForShell(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
