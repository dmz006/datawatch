package session

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// TmuxAPI is the subset of tmux operations the session Manager depends
// on. Extracted (BL89) so tests can substitute a FakeTmux without
// spawning real tmux server processes.
type TmuxAPI interface {
	NewSessionWithSize(name string, cols, rows int) error
	SessionExists(name string) bool
	SendKeys(session, keys string) error
	SendKeysWithSettle(session, keys string, settle time.Duration) error
	SendKeysLiteral(session, data string) error
	ResizePane(session string, cols, rows int) error
	CapturePaneVisible(session string) (string, error)
	CapturePaneLiveTail(session string) (string, error) // v5.27.6 — state detection
	CapturePaneANSI(session string) (string, error)
	PipeOutput(session, logFile string) error
	RepipeOutput(session, logFile string) error // BL263 / v6.11.9 — re-establish after daemon restart
	KillSession(name string) error
	SetEnvironment(session string, env map[string]string) error
}

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

// trimTrailingNewlines strips trailing \r and \n bytes. Callers of
// tmux send-keys that follow text with an explicit "Enter" keypress
// must first strip any trailing newline in the payload itself,
// otherwise claude-code, ink, and other bracketed-paste TUIs see the
// trailing \n as a "newline inside input" (they stay in multi-line
// compose mode) and the following explicit Enter then adds a second
// blank line instead of submitting. The operator then has to press
// Enter again manually for execution to start.
func trimTrailingNewlines(s string) string {
	for len(s) > 0 {
		c := s[len(s)-1]
		if c != '\n' && c != '\r' {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}

// defaultSendSettle is the small delay between the -l literal text
// push and the explicit Enter keypress. Without it, bracketed-paste
// TUIs (claude-code, ink, opencode, any Textual app) often fold the
// Enter into the paste event and the operator has to press Enter a
// second time manually (bug B34, v4.0.4). The value is small enough
// to be imperceptible for interactive use but large enough to win
// the race against every TUI I've tested.
const defaultSendSettle = 120 * time.Millisecond

// SendKeys sends keystrokes to a tmux session followed by Enter.
// Trailing newlines in `keys` are stripped before the explicit Enter
// is appended, and the push is always split into two tmux calls
// (literal text, then Enter) with a small settle between them so
// modern TUIs accept the Enter as "submit" rather than "newline
// inside input". Equivalent to SendKeysWithSettle(session, keys,
// defaultSendSettle).
func (t *TmuxManager) SendKeys(session, keys string) error {
	return t.SendKeysWithSettle(session, keys, defaultSendSettle)
}

// SendKeysWithSettle (B30) splits the text push and the Enter into
// two tmux calls with a settle delay between them. Fixes TUIs that
// start accepting input slightly after their prompt state transition
// fires, where the single-call form landed the text but the Enter
// was swallowed as part of the prompt's bracketed-paste/raw-mode
// setup.
//
// Trailing newlines in `keys` are stripped — the -l literal send
// would otherwise land the \n inside the TUI's input buffer, and the
// later explicit Enter would merely add a blank line.
//
// settle <= 0 is clamped to defaultSendSettle so the two-step pattern
// always runs; the previous "fall back to single-call" branch caused
// the B34 extra-Enter regression against modern TUIs.
func (t *TmuxManager) SendKeysWithSettle(session, keys string, settle time.Duration) error {
	if settle <= 0 {
		settle = defaultSendSettle
	}
	keys = trimTrailingNewlines(keys)
	if err := exec.Command("tmux", "send-keys", "-t", session, "-l", keys).Run(); err != nil {
		return err
	}
	time.Sleep(settle)
	return exec.Command("tmux", "send-keys", "-t", session, "Enter").Run()
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

// CapturePaneLiveTail (v5.27.6 — BL211) captures the live bottom of the
// pane regardless of tmux copy-mode (scrollback browsing). State
// detection MUST use this — operators scrolling up otherwise put the
// daemon's prompt/completion checks on stale content and the session
// stays in `running` even after claude prints "✻ Crunched for Xm" and
// waits.  PWA display continues using CapturePaneVisible because the
// operator-friendly scroll behaviour is the right shape for that path.
func (t *TmuxManager) CapturePaneLiveTail(session string) (string, error) {
	out, err := exec.Command("tmux", "capture-pane", "-e", "-p", "-t", session).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// CapturePaneVisible captures the visible pane content with ANSI escape sequences
// preserved. In tmux copy-mode (scrollback browsing), captures the scrolled view
// by using the scroll_position offset; otherwise captures the current visible pane.
func (t *TmuxManager) CapturePaneVisible(session string) (string, error) {
	// Check if pane is in copy-mode (scrollback browsing)
	modeOut, _ := exec.Command("tmux", "display-message", "-t", session, "-p", "#{pane_in_mode} #{scroll_position} #{pane_height}").Output()
	fields := strings.Fields(string(modeOut))
	if len(fields) == 3 && fields[0] == "1" {
		// In copy-mode — capture the scrolled view using offset
		scrollPos := fields[1]
		height := fields[2]
		sp, _ := strconv.Atoi(scrollPos)
		h, _ := strconv.Atoi(height)
		if sp > 0 && h > 0 {
			start := fmt.Sprintf("%d", -sp)
			end := fmt.Sprintf("%d", -sp+h-1)
			out, err := exec.Command("tmux", "capture-pane", "-e", "-p", "-S", start, "-E", end, "-t", session).Output()
			if err != nil {
				return "", err
			}
			return string(out), nil
		}
	}
	// Normal mode — capture visible pane
	out, err := exec.Command("tmux", "capture-pane", "-e", "-p", "-t", session).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// CapturePaneANSI captures the full pane content including scrollback with ANSI preserved.
// Used for state detection and initial snapshot (includes all history).
func (t *TmuxManager) CapturePaneANSI(session string) (string, error) {
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
// Uses tmux pipe-pane to append to the file. Uses -o (toggle): only opens
// a pipe if no pipe is in effect, which is correct for the create path.
func (t *TmuxManager) PipeOutput(session, logFile string) error {
	cmd := fmt.Sprintf("cat >> %s", logFile)
	return exec.Command("tmux", "pipe-pane", "-o", "-t", session, cmd).Run()
}

// RepipeOutput re-establishes the pipe-pane bridge for a session whose
// tmux survived a daemon restart. Different from PipeOutput because it
// MUST replace any existing (orphaned) pipe-pane child the previous
// daemon left behind. Two-step:
//
//  1. `tmux pipe-pane -t SESS` with no command — closes any pipe in effect.
//  2. `tmux pipe-pane -t SESS "cat >> log"` — starts a fresh pipe.
//
// Without this, the pipe-pane child the previous daemon spawned either:
//   - died with the daemon (no pipe in effect — new daemon's PipeOutput
//     with -o would work) OR
//   - survived (writing to a closed FD — daemon receives nothing; -o on
//     PipeOutput would TOGGLE it CLOSED, making things worse)
//
// RepipeOutput handles both cases unconditionally (BL263 / v6.11.9).
func (t *TmuxManager) RepipeOutput(session, logFile string) error {
	// Close any existing pipe-pane (no-op if none in effect).
	_ = exec.Command("tmux", "pipe-pane", "-t", session).Run()
	// Start a fresh pipe.
	cmd := fmt.Sprintf("cat >> %s", logFile)
	return exec.Command("tmux", "pipe-pane", "-t", session, cmd).Run()
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

// ShowEnvironment captures all environment variables from a tmux session.
// Returns a map of key→value pairs. Only includes variables explicitly set
// in the tmux session environment (not inherited from the global environment).
func (t *TmuxManager) ShowEnvironment(session string) (map[string]string, error) {
	out, err := exec.Command("tmux", "show-environment", "-t", session).Output()
	if err != nil {
		return nil, err
	}
	env := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "-") {
			continue // skip removed vars (prefixed with -)
		}
		if idx := strings.IndexByte(line, '='); idx > 0 {
			env[line[:idx]] = line[idx+1:]
		}
	}
	return env, nil
}

// SetEnvironment sets environment variables in a tmux session.
func (t *TmuxManager) SetEnvironment(session string, env map[string]string) error {
	for k, v := range env {
		if err := exec.Command("tmux", "set-environment", "-t", session, k, v).Run(); err != nil {
			return fmt.Errorf("set-environment %s: %w", k, err)
		}
	}
	return nil
}

// AttachCommand returns the shell command a user should run to attach to the session.
func (t *TmuxManager) AttachCommand(session string) string {
	return fmt.Sprintf("tmux attach -t %s", session)
}
