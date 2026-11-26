// Notices about sessions nothing else tells the operator about: those found
// dead when the daemon starts, and running ones that have gone silent.

package session

import (
	"fmt"
	"strings"
	"time"
)

// SetSessionNoticeHook sets the function that raises one operator notice.
func (m *Manager) SetSessionNoticeHook(fn func(title, body string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionNotice = fn
}

func noticeLine(snap Session, detail string) string {
	if snap.Name != "" {
		return fmt.Sprintf("%s [%s]: %s", snap.Name, snap.ID, detail)
	}
	return fmt.Sprintf("%s: %s", snap.ID, detail)
}

// noteEndedAtStart remembers a session that ResumeMonitors found dead. It runs
// before the daemon can send notices.
func (m *Manager) noteEndedAtStart(sess *Session, was State) {
	line := noticeLine(m.store.Snapshot(sess), fmt.Sprintf("was %s, now failed: its process is gone", was))
	m.mu.Lock()
	defer m.mu.Unlock()
	m.endedAtStart = append(m.endedAtStart, line)
}

// ReportBootEnded sends one notice naming every session found dead at daemon
// start. The daemon calls it once notices can be delivered.
func (m *Manager) ReportBootEnded() {
	m.mu.Lock()
	lines, hook := m.endedAtStart, m.sessionNotice
	if hook != nil {
		m.endedAtStart = nil
	}
	m.mu.Unlock()
	if len(lines) == 0 || hook == nil {
		return
	}
	hook(fmt.Sprintf("%d session(s) found dead at daemon start", len(lines)), strings.Join(lines, "\n"))
}

// CheckSilentSessions sends one notice for the running sessions of this host
// that have shown nothing for longer than threshold and have not been reported
// for this silence yet.
func (m *Manager) CheckSilentSessions(now time.Time, threshold time.Duration) {
	m.mu.Lock()
	hook := m.sessionNotice
	m.mu.Unlock()
	if hook == nil || threshold <= 0 {
		return
	}
	var lines []string
	for _, sess := range m.store.List() {
		snap := m.store.Snapshot(sess)
		if snap.Hostname != m.hostname || !IsStale(&snap, threshold, now) {
			continue
		}
		m.mu.Lock()
		if m.silentNoticed == nil {
			m.silentNoticed = map[string]time.Time{}
		}
		last, told := m.silentNoticed[snap.FullID]
		if !told || !last.Equal(snap.UpdatedAt) {
			m.silentNoticed[snap.FullID] = snap.UpdatedAt
			lines = append(lines, noticeLine(snap, fmt.Sprintf("running, no change since %s", snap.UpdatedAt.Local().Format("2006-01-02 15:04"))))
		}
		m.mu.Unlock()
	}
	if len(lines) > 0 {
		hook(fmt.Sprintf("%d running session(s) silent for more than %s", len(lines), threshold), strings.Join(lines, "\n"))
	}
}

// checkSilentSessions is the schedule timer's pass, with the configured limit.
func (m *Manager) checkSilentSessions(now time.Time) {
	if m.cfg == nil {
		return
	}
	m.CheckSilentSessions(now, time.Duration(m.cfg.Session.StaleTimeoutSeconds)*time.Second)
}
