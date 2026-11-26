// How long the daemon's prompt-line and state-change responders keep at a new
// session's start-up screens.

package main

import (
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

const (
	// startupRespondWindow leaves time to answer a dialog once it has settled,
	// and once more if the keys did not take.
	startupRespondWindow = 20 * time.Second
	startupRespondPause  = 500 * time.Millisecond
)

// respondToStartupScreens answers the start-up dialogs of session sessID until
// one has been answered and no dialog is left on the live screen, or the window
// ends. A dialog whose keys did not take is still there, and is tried again.
func respondToStartupScreens(m *session.Manager, sessID string, respond func(*session.Manager, string) bool, window, pause time.Duration) {
	answered := false
	for end := time.Now().Add(window); time.Now().Before(end); time.Sleep(pause) {
		if respond(m, sessID) {
			answered = true
		}
		if _, shown := m.LiveStartupDialog(sessID); answered && !shown {
			return
		}
	}
}
