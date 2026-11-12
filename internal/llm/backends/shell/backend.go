// Package shell implements a configurable shell script LLM backend.
// When script_path is empty, it opens an interactive shell session in tmux.
// When script_path is set, the script receives the task as $1 and project dir as $2.
// If the inline script-form command would exceed llm.MaxSendKeysCommand, the task
// is written to a file in the project's .datawatch folder and the script receives
// that file path as $1 instead. The command removes the file after the script exits.
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

func (b *Backend) Name() string                   { return "shell" }
func (b *Backend) SupportsInteractiveInput() bool { return true }
func (b *Backend) HasToolAccess() bool            { return true }
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
	var cmd, taskFile string
	// If script_path is set to a shell binary (e.g. /usr/bin/bash), treat as interactive
	isScript := b.scriptPath != "" && !isShellBinary(b.scriptPath)
	if isScript && task != "" {
		escapedTask := strings.ReplaceAll(task, "'", `'\''`)
		escapedScript := strings.ReplaceAll(b.scriptPath, "'", `'\''`)
		cmd = fmt.Sprintf("'%s' '%s' '%s'; echo 'DATAWATCH_COMPLETE: shell done'",
			escapedScript, escapedTask, escapedDir)
		if len(cmd) > llm.MaxSendKeysCommand {
			// Oversize task: hand the script a file path instead of inline text.
			path, err := writeTaskFile(projectDir, tmuxSession, task)
			if err != nil {
				return fmt.Errorf("shell launch: write oversize task file: %w", err)
			}
			taskFile = path
			escapedFile := strings.ReplaceAll(taskFile, "'", `'\''`)
			cmd = fmt.Sprintf("'%s' '%s' '%s'; rm -f '%s'; echo 'DATAWATCH_COMPLETE: shell done'",
				escapedScript, escapedFile, escapedDir, escapedFile)
		}
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
	// Delivery goes through the shared llm.SendKeys helper: a tmux failure
	// carries tmux's own stderr text in the returned error (not a bare
	// exit status), and the shared MaxSendKeysCommand guard is the
	// explicit length check for this branch's intentionally inline forms
	// (the interactive echo-of-task and the script argv) — oversize
	// commands are rejected up front, so nothing is ever sent to tmux.
	if err := llm.SendKeys(ctx, tmuxSession, cmd); err != nil {
		return err
	}
	emitRecord(CommandRecord{Session: tmuxSession, Command: cmd, Start: recordNow(), TaskFile: taskFile})
	return nil
}
