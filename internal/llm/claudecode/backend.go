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
// When task is empty, claude is started in interactive mode (no task argument).
// When task is provided, it is passed as the initial prompt.
func (b *Backend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	var flags string
	if b.skipPermissions {
		flags = " --dangerously-skip-permissions"
	}

	var cmd string
	if task == "" {
		// Interactive mode: no task argument, user will interact through the session.
		cmd = fmt.Sprintf("cd %s && NO_COLOR=1 %s --add-dir %s%s",
			shellQuote(projectDir), b.binaryPath, shellQuote(projectDir), flags)
	} else {
		// Non-interactive: pass task as the initial prompt.
		escaped := escapeForShell(task)
		cmd = fmt.Sprintf("cd %s && NO_COLOR=1 %s --add-dir %s%s '%s'",
			shellQuote(projectDir), b.binaryPath, shellQuote(projectDir), flags, escaped)
	}

	err := exec.CommandContext(ctx,
		"tmux", "send-keys", "-t", tmuxSession, cmd, "Enter",
	).Run()
	if err != nil {
		return fmt.Errorf("launch claude-code in %s: %w", tmuxSession, err)
	}
	return nil
}

// LaunchResume resumes a prior claude-code conversation using --resume SESSION_ID.
func (b *Backend) LaunchResume(ctx context.Context, task, tmuxSession, projectDir, logFile, resumeID string) error {
	var flags string
	if b.skipPermissions {
		flags = " --dangerously-skip-permissions"
	}
	var cmd string
	if task == "" {
		cmd = fmt.Sprintf("cd %s && NO_COLOR=1 %s --add-dir %s%s --resume %s",
			shellQuote(projectDir), b.binaryPath, shellQuote(projectDir), flags,
			shellQuote(resumeID))
	} else {
		escaped := escapeForShell(task)
		cmd = fmt.Sprintf("cd %s && NO_COLOR=1 %s --add-dir %s%s --resume %s '%s'",
			shellQuote(projectDir), b.binaryPath, shellQuote(projectDir), flags,
			shellQuote(resumeID), escaped)
	}
	return exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, cmd, "Enter").Run()
}

func escapeForShell(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
