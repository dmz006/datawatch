// Package aider implements the LLM backend for aider (https://aider.chat).
// Runs `aider --yes --message '<task>'` in a tmux session.
package aider

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/llm/scratch"
)

// writeTaskFile writes the task text to a scratch file instead of embedding
// it inline in the shell command sent to the pane. Mirrors
// pi.writeTaskFile for the same reason: typing a multi-KB task spec inline
// via `tmux send-keys` can make the send-keys command exceed tmux's roughly
// 16 KB limit, so the task is read back from the file with
// `$(cat 'file')` — the *typed* line stays short while the agent still
// receives the exact same content.
func writeTaskFile(tmuxSession, task string) (string, error) {
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, tmuxSession)
	f, err := os.CreateTemp("", "datawatch-aider-task-"+safe+"-*.txt")
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck
	if _, err := f.WriteString(task); err != nil {
		os.Remove(f.Name()) //nolint:errcheck
		return "", err
	}
	// Session-end ownership: record the task-capture file under the session's
	// tmux name so its end path releases any the run left behind.
	scratch.RecordPath(tmuxSession, f.Name())
	return f.Name(), nil
}

// Backend runs aider in a tmux session.
type Backend struct{ binary string }

// New creates an aider backend. binary defaults to "aider".
func New(binary string) llm.Backend {
	if binary == "" {
		binary = "aider"
	}
	return &Backend{binary: binary}
}

func (b *Backend) Name() string                   { return "aider" }
func (b *Backend) SupportsInteractiveInput() bool { return false }
func (b *Backend) HasToolAccess() bool            { return true }
func (b *Backend) Version() string {
	out, err := exec.Command(b.binary, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Launch starts aider on the given task. The task text is delivered via a
// scratch file (`--message "$(cat '...')"`) rather than typed inline —
// see writeTaskFile's doc comment; only if writing the scratch file fails
// does it fall back to the old inline form.
func (b *Backend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	escaped := strings.ReplaceAll(task, "'", `'\''`)
	message := escaped // fallback: inline
	cleanup := ""
	if path, err := writeTaskFile(tmuxSession, task); err == nil {
		escapedPath := strings.ReplaceAll(path, "'", `'\''`)
		message = "$(cat '" + escapedPath + "')"
		cleanup = "rm -f '" + escapedPath + "'; "
	}
	cmd := fmt.Sprintf("cd '%s' && %s --yes --message '%s'; %secho 'DATAWATCH_COMPLETE: aider done'",
		strings.ReplaceAll(projectDir, "'", `'\''`), b.binary, message, cleanup)
	return exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, cmd, "Enter").Run()
}
