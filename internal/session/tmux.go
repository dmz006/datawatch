package session

import (
	"fmt"
	"os/exec"
	"strings"
)

// TmuxManager wraps tmux operations used by the session manager.
type TmuxManager struct{}

// NewSession creates a new detached tmux session with the given name.
// Sets a wide terminal (220x50) so TUI applications like claude-code render
// without wrapping artifacts in the captured output log.
// Returns an error if the session already exists or tmux fails.
func (t *TmuxManager) NewSession(name string) error {
	return exec.Command("tmux", "new-session", "-d", "-s", name, "-x", "220", "-y", "50").Run()
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
