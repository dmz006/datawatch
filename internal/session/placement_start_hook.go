package session

// Placement start hook: an optional callback the daemon installs so that every
// successful Manager.Start, and every successful Manager.Restart (and so
// RestartSession), can have the session's placement (system) tag set applied. The hook is stored under Manager.mu and invoked after the
// session record exists, with the lock released, so a hook may call back into
// the Manager without deadlocking. A nil hook is a no-op.

// SetPlacementStartHook installs fn to be called with each newly started
// session. Passing nil clears the hook.
func (m *Manager) SetPlacementStartHook(fn func(*Session)) {
	m.mu.Lock()
	m.placementStartHook = fn
	m.mu.Unlock()
}

// runPlacementStartHook invokes the installed hook, if any, with sess. It must
// be called without m.mu held.
func (m *Manager) runPlacementStartHook(sess *Session) {
	m.mu.Lock()
	fn := m.placementStartHook
	m.mu.Unlock()
	if fn != nil {
		fn(sess)
	}
}
