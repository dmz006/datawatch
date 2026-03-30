package session

import (
	"fmt"
	"os/exec"
	"strings"
)

// TmuxManager wraps tmux operations used by the session manager.
type TmuxManager struct{}

// NewSession creates a new detached tmux session with the given name.
// Starts at a conservative 80x24 default; the web client sends a resize_term
// message once xterm.js FitAddon computes the actual container dimensions.
// Starting wider than the web terminal causes escape-sequence content to wrap
// incorrectly when replayed in xterm.js (the #1 rendering bug).
// Returns an error if the session already exists or tmux fails.
func (t *TmuxManager) NewSession(name string) error {
	return t.NewSessionWithSize(name, 80, 24)
}

// NewSessionWithSize creates a detached tmux session at the given column/row size.
// aggressive-resize prevents tmux from inheriting attached client dimensions.
func (t *TmuxManager) NewSessionWithSize(name string, cols, rows int) error {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	colStr := fmt.Sprintf("%d", cols)
	rowStr := fmt.Sprintf("%d", rows)
	if err := exec.Command("tmux", "new-session", "-d", "-s", name, "-x", colStr, "-y", rowStr).Run(); err != nil {
		return err
	}
	exec.Command("tmux", "set-option", "-t", name, "aggressive-resize", "on").Run()  //nolint:errcheck
	exec.Command("tmux", "resize-window", "-t", name, "-x", colStr, "-y", rowStr).Run()  //nolint:errcheck
	return nil
}

// SessionExists reports whether a tmux session with the given name exists.
func (t *TmuxManager) SessionExists(name string) bool {
	err := exec.Command("tmux", "has-session", "-t", name).Run()
	return err == nil
}

// SendKeys sends keystrokes to a tmux session followed by Enter.
func (t *TmuxManager) SendKeys(session, keys string) error {
	return exec.Command("tmux", "send-keys", "-t", session, keys, "Enter").Run()
}

// SendText sends text to a tmux session without pressing Enter.
func (t *TmuxManager) SendText(session, text string) error {
	return exec.Command("tmux", "send-keys", "-t", session, text, "").Run()
}

// ResizePane resizes a tmux pane to match the web terminal dimensions.
func (t *TmuxManager) ResizePane(session string, cols, rows int) error {
	return exec.Command("tmux", "resize-window", "-t", session,
		"-x", fmt.Sprintf("%d", cols), "-y", fmt.Sprintf("%d", rows)).Run()
}

// CapturePaneANSI captures the visible pane content with ANSI escape sequences preserved.
// This is used to re-capture pane content after a resize so the xterm.js terminal
// can display it at the correct column width instead of replaying stale buffered output.
func (t *TmuxManager) CapturePaneANSI(session string) (string, error) {
	// -e preserves ANSI escape sequences, -p prints to stdout, -S - captures from start of scrollback
	out, err := exec.Command("tmux", "capture-pane", "-e", "-p", "-t", session, "-S", "-").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// SendKeysLiteral sends literal bytes to a tmux session using -l flag.
// This preserves special characters and doesn't append Enter.
func (t *TmuxManager) SendKeysLiteral(session, data string) error {
	return exec.Command("tmux", "send-keys", "-t", session, "-l", data).Run()
}

// PipeOutput configures the tmux session to pipe all output to a log file.
// Uses tmux pipe-pane to append to the file.
func (t *TmuxManager) PipeOutput(session, logFile string) error {
	cmd := fmt.Sprintf("cat >> %s", logFile)
	return exec.Command("tmux", "pipe-pane", "-o", "-t", session, cmd).Run()
}

// KillSession terminates a tmux session by name.
func (t *TmuxManager) KillSession(name string) error {
	return exec.Command("tmux", "kill-session", "-t", name).Run()
}

// ListSessions returns names of all tmux sessions matching a prefix.
func (t *TmuxManager) ListSessions(prefix string) ([]string, error) {
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		// tmux returns error if no sessions exist
		return nil, nil
	}
	var names []string
	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if name != "" && strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	return names, nil
}

// AttachCommand returns the shell command a user should run to attach to the session.
func (t *TmuxManager) AttachCommand(session string) string {
	return fmt.Sprintf("tmux attach -t %s", session)
}
