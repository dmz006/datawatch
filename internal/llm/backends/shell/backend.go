// Package shell implements a configurable shell script LLM backend.
// When script_path is empty, it opens an interactive shell session in tmux.
// When script_path is set, the script receives the task as $1 and project dir as $2.
package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// isShellBinary returns true if the path looks like a shell binary (not a script).
func isShellBinary(path string) bool {
	base := filepath.Base(path)
	shells := []string{"bash", "zsh", "fish", "sh", "dash", "tcsh", "ksh"}
	for _, s := range shells {
		if base == s {
			return true
		}
	}
	return false
}

func (b *Backend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	escapedDir := strings.ReplaceAll(projectDir, "'", `'\''`)
	var cmd string
	// If script_path is set to a shell binary (e.g. /usr/bin/bash), treat as interactive
	isScript := b.scriptPath != "" && !isShellBinary(b.scriptPath)
	if isScript && task != "" {
		escapedTask := strings.ReplaceAll(task, "'", `'\''`)
		cmd = fmt.Sprintf("'%s' '%s' '%s'; echo 'DATAWATCH_COMPLETE: shell done'",
			strings.ReplaceAll(b.scriptPath, "'", `'\''`), escapedTask, escapedDir)
	} else {
		// Interactive shell: cd to project dir and start bash with a known prompt.
		// Use --rcfile with a custom init that sets PS1 so prompt detection works.
		// This ensures PS1 persists even if .bashrc would override it.
		shell := b.resolveShell()
		if task != "" {
			escapedTask := strings.ReplaceAll(task, "'", `'\''`)
			cmd = fmt.Sprintf("cd '%s' && echo '# %s' && PROMPT_COMMAND='' PS1='datawatch:\\w$ ' %s --norc", escapedDir, escapedTask, shell)
		} else {
			cmd = fmt.Sprintf("cd '%s' && PROMPT_COMMAND='' PS1='datawatch:\\w$ ' %s --norc", escapedDir, shell)
		}
	}
	return exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, cmd, "Enter").Run()
}
