// Notices about sessions nothing else tells the operator about: those found
// dead when the daemon starts, and running ones that have gone silent.

package session

import (
	"fmt"
	"os"
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

// ReportStartupDialogLeft raises the notice that session fullID is left at a
// start-up dialog for a person. title says so; the body names the session and
// holds nothing from its screen.
func (m *Manager) ReportStartupDialogLeft(fullID, title string) {
	sess, ok := m.store.Get(fullID)
	if !ok {
		return
	}
	snap := m.store.Snapshot(sess)
	name := snap.ID
	if snap.Name != "" {
		name = fmt.Sprintf("%s [%s]", snap.Name, snap.ID)
	}
	fmt.Printf("[claude] %s: %s\n", fullID, title)
	m.mu.Lock()
	hook := m.sessionNotice
	m.mu.Unlock()
	if hook != nil {
		hook(title, "session "+name)
	}
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

// lastSign is the latest sign of life of a session: a change of its record, a
// channel event, or output written to its log. Steady output on a running
// session changes nothing in the record.
func lastSign(snap Session) time.Time {
	at := snap.UpdatedAt
	if snap.LastChannelEventAt.After(at) {
		at = snap.LastChannelEventAt
	}
	if snap.LogFile != "" {
		// The second name is where output goes when it is sealed.
		for _, p := range []string{snap.LogFile, snap.LogFile + ".enc"} {
			if info, err := os.Stat(p); err == nil && info.ModTime().After(at) {
				at = info.ModTime()
			}
		}
	}
	return at
}

// CheckSilentSessions sends one notice for the running sessions of this host
// that have shown no sign of life for longer than threshold and have not been
// reported for this silence yet. A threshold of 0 or less turns it off: set
// session.stale_timeout_seconds negative (0 in the file means the default).
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
		if snap.Hostname != m.hostname || snap.State != StateRunning {
			continue
		}
		sign := lastSign(snap)
		if now.Sub(sign) <= threshold {
			continue
		}
		m.mu.Lock()
		if m.silentNoticed == nil {
			m.silentNoticed = map[string]time.Time{}
		}
		last, told := m.silentNoticed[snap.FullID]
		if !told || !last.Equal(sign) {
			m.silentNoticed[snap.FullID] = sign
			lines = append(lines, noticeLine(snap, fmt.Sprintf("running, no output or event since %s", sign.Local().Format("2006-01-02 15:04"))))
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
