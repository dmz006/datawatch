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
	binaryPath      string
	skipPermissions bool // pass --dangerously-skip-permissions
	channelEnabled  bool // pass --channels server:datawatch --dangerously-load-development-channels
}

// New creates a claude-code backend. binaryPath defaults to "claude".
func New(binaryPath string) llm.Backend {
	if binaryPath == "" {
		binaryPath = "claude"
	}
	return &Backend{binaryPath: binaryPath}
}

// NewWithOptions creates a claude-code backend with options.
func NewWithOptions(binaryPath string, skipPermissions bool, channelEnabled bool) llm.Backend {
	if binaryPath == "" {
		binaryPath = "claude"
	}
	return &Backend{binaryPath: binaryPath, skipPermissions: skipPermissions, channelEnabled: channelEnabled}
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
// preFlagsStr returns flags that must appear BEFORE --add-dir (variadic flags like --channels).
func (b *Backend) preFlagsStr() string {
	var flags string
	if b.channelEnabled {
		// --dangerously-load-development-channels is variadic; it must come before --add-dir
		// so --add-dir terminates the variadic argument list.
		flags += " --dangerously-load-development-channels server:datawatch"
	}
	return flags
}

// postFlagsStr returns flags that go after --add-dir.
func (b *Backend) postFlagsStr() string {
	var flags string
	if b.skipPermissions {
		flags += " --dangerously-skip-permissions"
	}
	return flags
}

func (b *Backend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	pre := b.preFlagsStr()
	post := b.postFlagsStr()

	var cmd string
	if task == "" || b.channelEnabled {
		// Interactive mode: no task argument, user will interact through the session.
		// In channel mode the task is delivered via the MCP channel once Claude is ready
		// (i.e. after "Listening for channel messages" appears in output).
		cmd = fmt.Sprintf("cd %s && NO_COLOR=1 %s%s --add-dir %s%s",
			shellQuote(projectDir), b.binaryPath, pre, shellQuote(projectDir), post)
	} else {
		// Non-interactive: pass task as the initial prompt.
		escaped := escapeForShell(task)
		cmd = fmt.Sprintf("cd %s && NO_COLOR=1 %s%s --add-dir %s%s '%s'",
			shellQuote(projectDir), b.binaryPath, pre, shellQuote(projectDir), post, escaped)
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
	pre := b.preFlagsStr()
	post := b.postFlagsStr()
	var cmd string
	if task == "" {
		cmd = fmt.Sprintf("cd %s && NO_COLOR=1 %s%s --add-dir %s%s --resume %s",
			shellQuote(projectDir), b.binaryPath, pre, shellQuote(projectDir), post,
			shellQuote(resumeID))
	} else {
		escaped := escapeForShell(task)
		cmd = fmt.Sprintf("cd %s && NO_COLOR=1 %s%s --add-dir %s%s --resume %s '%s'",
			shellQuote(projectDir), b.binaryPath, pre, shellQuote(projectDir), post,
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
