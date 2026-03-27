// Package shell implements a configurable shell script LLM backend.
// Set session.shell_script in config to the path of your script.
// The script receives the task as $1 and the project dir as $2.
package shell

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/dmz006/datawatch/internal/llm"
)

// Backend runs a configurable shell script in a tmux session.
type Backend struct{ scriptPath string }

// New creates a shell script backend with the given script path.
func New(scriptPath string) llm.Backend {
	return &Backend{scriptPath: scriptPath}
}

func (b *Backend) Name() string                  { return "shell" }
func (b *Backend) SupportsInteractiveInput() bool { return false }
func (b *Backend) Version() string               { return "" }

func (b *Backend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	script := b.scriptPath
	if script == "" {
		return fmt.Errorf("shell backend: no script_path configured (set session.shell_script in config)")
	}
	escapedTask := strings.ReplaceAll(task, "'", `'\''`)
	escapedDir := strings.ReplaceAll(projectDir, "'", `'\''`)
	cmd := fmt.Sprintf("'%s' '%s' '%s'; echo 'DATAWATCH_COMPLETE: shell done'",
		strings.ReplaceAll(script, "'", `'\''`), escapedTask, escapedDir)
	return exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, cmd, "Enter").Run()
}
