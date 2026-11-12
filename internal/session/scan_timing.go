package session

import "time"

const (
	// defaultRestartGrace is how long after a restart the re-rendered pane
	// history is distrusted for protocol markers.
	defaultRestartGrace = 30 * time.Second
	// defaultTaskEchoWindow is how long after task delivery the TUI's echo of
	// the task text is distrusted for protocol markers.
	defaultTaskEchoWindow = 5 * time.Second
	// defaultScanInterval is the monitorOutput idle/pane scan cadence.
	defaultScanInterval = 2 * time.Second
)

// now is the clock behind every scan-timing decision (grace, echo window,
// prompt debounce). Production uses wall time; tests inject Manager.clock.
func (m *Manager) now() time.Time {
	if m.clock != nil {
		return m.clock()
	}
	return time.Now()
}

func (m *Manager) restartGraceWindow() time.Duration {
	if m.restartGrace > 0 {
		return m.restartGrace
	}
	return defaultRestartGrace
}

func (m *Manager) echoWindow() time.Duration {
	if m.taskEchoWindow > 0 {
		return m.taskEchoWindow
	}
	return defaultTaskEchoWindow
}

func (m *Manager) scanTick() time.Duration {
	if m.scanInterval > 0 {
		return m.scanInterval
	}
	return defaultScanInterval
}
