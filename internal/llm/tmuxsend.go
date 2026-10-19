package llm

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// MaxSendKeysCommand is the byte limit for a single tmux send-keys payload.
// Measured against a live tmux server: 9000-byte commands pass and
// 17000-byte commands fail with "command too long", so the true ceiling is
// ~16 KB. 12288 sits comfortably below it, leaving headroom for the extra
// argv tmux appends around the payload.
const MaxSendKeysCommand = 12288

// SendKeys sends cmd to the given tmux session via `tmux send-keys`,
// terminating with Enter.
//
// Two caller-visible guarantees:
//
//   - Commands longer than MaxSendKeysCommand bytes are rejected up front
//     with an error stating both the allowed limit and the actual byte
//     count; nothing is executed.
//   - If tmux itself fails (e.g. its own "command too long" rejection),
//     the returned error wraps the exec error AND appends tmux's stderr
//     text, so the failure is visible to every caller — REST, CLI, PWA —
//     instead of a bare "exit status 1".
func SendKeys(ctx context.Context, tmuxSession, cmd string) error {
	if len(cmd) > MaxSendKeysCommand {
		return fmt.Errorf("send-keys: command too long: allowed %d bytes, got %d bytes", MaxSendKeysCommand, len(cmd))
	}

	var stderr bytes.Buffer
	command := exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, cmd, "Enter")
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("tmux send-keys: %w: %s", err, msg)
		}
		return fmt.Errorf("tmux send-keys: %w", err)
	}
	return nil
}

// ResumeKeepsTask is an optional interface a backend can implement. A
// backend implementing it signals that a resumed conversation already
// holds the original task, so a no-override restart should not resend
// the full task text.
type ResumeKeepsTask interface {
	ResumeKeepsTask() bool
}
