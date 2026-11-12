// Restore record for the back-off registry: after the daemon reinstalls the
// unexpired flags saved by the previous run (Backoff.Load), this records each
// one so the operator can see why an Automaton is waiting right after a
// restart. It writes a decision-log entry, an operator alert and a capacity
// audit event, and never starts, ends or signals a session.

package autonomous

import (
	"fmt"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
)

// bootResumeBackoffRestored is the BootResumeEntry outcome of a restored flag.
const bootResumeBackoffRestored = "backoff_restored"

// BackoffRestored records a back-off flag read back from disk at daemon start.
func (r *CapacityAudit) BackoffRestored(e Entry) {
	r.Record(AuditActorAutonomous, audit.ActionUsageBackoffRestored, e.Backend, map[string]any{
		"backend": e.Backend,
		"until":   e.Until.UTC().Format(time.RFC3339),
		"source":  e.Source,
	})
}

// RecordBackoffRestored records every restored flag: a decision-log entry (the
// boot-resume report), an operator alert and a capacity audit event. Work for
// a restored backend is held by the normal capacity admission, exactly as for
// a flag set during this run; sessions already alive are left untouched.
func (m *Manager) RecordBackoffRestored(entries []Entry) {
	if len(entries) == 0 {
		return
	}
	m.mu.Lock()
	alert, capAudit := m.alertFn, m.capAudit
	m.mu.Unlock()
	now := time.Now()
	for _, e := range entries {
		until := e.Until.Local().Format("15:04 Jan 2")
		reason := fmt.Sprintf("back-off for backend %s restored after the restart, active until %s (%s): new Automaton work for that backend waits for the flag to clear, and sessions already running on it are left alone", e.Backend, until, e.Reason)
		m.recordBootResume(BootResumeEntry{Outcome: bootResumeBackoffRestored, Reason: reason, At: now})
		if alert != nil {
			alert("Back-off restored after restart",
				fmt.Sprintf("Backend %s is still backed off until %s. Automaton work for it waits for the flag to clear; sessions already running on it were not touched.", e.Backend, until))
		}
		capAudit.BackoffRestored(e)
	}
}
