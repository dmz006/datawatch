package session

// The runner registry records which sessions the daemon currently holds a
// runner for: a goroutine or process the daemon itself drives for a session
// that has no terminal pane (a subprocess session, a chat-mode session, or
// any other session without a terminal). Code that needs to know whether a
// session without a pane is still being driven asks HasRunner.

// RegisterRunner records that the daemon holds a runner for the session with
// the given full ID. Registering an ID that is already present is a no-op.
func (m *Manager) RegisterRunner(fullID string) {
	m.runnersMu.Lock()
	defer m.runnersMu.Unlock()
	if m.runners == nil {
		m.runners = make(map[string]struct{})
	}
	m.runners[fullID] = struct{}{}
}

// ReleaseRunner records that the daemon no longer holds a runner for the
// session with the given full ID. Releasing an unknown ID is a no-op.
func (m *Manager) ReleaseRunner(fullID string) {
	m.runnersMu.Lock()
	defer m.runnersMu.Unlock()
	delete(m.runners, fullID)
}

// HasRunner reports whether the daemon currently holds a runner for the
// session with the given full ID.
func (m *Manager) HasRunner(fullID string) bool {
	m.runnersMu.Lock()
	defer m.runnersMu.Unlock()
	_, ok := m.runners[fullID]
	return ok
}
