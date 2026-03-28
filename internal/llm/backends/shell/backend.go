// Package shell implements a configurable shell script LLM backend.
// When script_path is empty, it opens an interactive shell session in tmux.
// When script_path is set, the script receives the task as $1 and project dir as $2.
package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/dmz006/datawatch/internal/llm"
)

// Backend runs a configurable shell script (or interactive shell) in a tmux session.
type Backend struct{ scriptPath string }

// New creates a shell backend. If scriptPath is empty, starts an interactive shell.
func New(scriptPath string) llm.Backend {
	return &Backend{scriptPath: scriptPath}
}

func (b *Backend) Name() string                  { return "shell" }
func (b *Backend) SupportsInteractiveInput() bool { return true }
func (b *Backend) Version() string {
	shell := b.resolveShell()
	out, err := exec.Command(shell, "--version").Output()
	if err != nil {
		// Most shells support --version; fall back to reporting the shell path
		return shell
	}
	// Return just the first line
	v := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	if v == "" {
		return shell
	}
	return v
}

func (b *Backend) resolveShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return "bash"
}

func (b *Backend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	escapedDir := strings.ReplaceAll(projectDir, "'", `'\''`)
	var cmd string
	if b.scriptPath != "" {
		escapedTask := strings.ReplaceAll(task, "'", `'\''`)
		cmd = fmt.Sprintf("'%s' '%s' '%s'; echo 'DATAWATCH_COMPLETE: shell done'",
			strings.ReplaceAll(b.scriptPath, "'", `'\''`), escapedTask, escapedDir)
	} else {
		// Interactive shell: cd to project dir and start a shell
		// Print task as a comment so it's visible in the terminal
		if task != "" {
			escapedTask := strings.ReplaceAll(task, "'", `'\''`)
			cmd = fmt.Sprintf("cd '%s' && echo '# %s' && %s", escapedDir, escapedTask, b.resolveShell())
		} else {
			cmd = fmt.Sprintf("cd '%s' && %s", escapedDir, b.resolveShell())
		}
	}
	return exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, cmd, "Enter").Run()
}
