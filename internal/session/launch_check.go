// A check that can refuse a launch before anything of it exists.

package session

import "fmt"

// SetLaunchCheck sets a check that can refuse a launch. It runs before a
// session's pane is made and before the pre-launch callback, so a refused start
// leaves no pane, session record or credential. A refused restart changes
// nothing: the record stays as it was and a live pane is not closed.
func (m *Manager) SetLaunchCheck(fn func(*Session) error) {
	m.launchCheck = fn
}

func (m *Manager) checkLaunch(sess *Session) error {
	if m.launchCheck == nil {
		return nil
	}
	if err := m.launchCheck(sess); err != nil {
		return fmt.Errorf("launch refused: %w", err)
	}
	return nil
}
