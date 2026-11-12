package autonomous

import (
	"fmt"
	"time"
)

// ExitSilenceTracker detects a pi print run that exited silently: no
// completion, no verdict, and no further output. Unlike
// SessionIdleObservation.Stalled, which only fires while the pane state is
// waiting_input/idle, this tracker is state-independent. It only counts
// silence after the runner process has stopped, so a session whose pane
// state flickers after pi exits still accumulates toward the limit. It uses
// two signals that do not depend on session state: whether a runner child
// process is alive and the mtime of the session log. lastActivity is the
// latest of the tracker start, the last tick that saw the runner alive, and
// the latest non-zero log mtime observed, so log growth resets the clock.
type ExitSilenceTracker struct {
	limit        time.Duration
	lastActivity time.Time
}

// NewExitSilenceTracker returns a tracker measuring silence from start with
// the given limit. limit <= 0 disables detection (never stalled).
func NewExitSilenceTracker(start time.Time, limit time.Duration) *ExitSilenceTracker {
	return &ExitSilenceTracker{limit: limit, lastActivity: start}
}

// Observe reports whether the run has been silent past the limit without
// the runner alive. runnerAlive==true or a newer log mtime moves the
// clock forward. A pane that has not yet launched pi is not flagged because
// the clock starts at the tracker's start time.
func (t *ExitSilenceTracker) Observe(now time.Time, runnerAlive bool, logModTime time.Time) (bool, string) {
	last := t.lastActivity
	if runnerAlive && now.After(last) {
		last = now
	}
	if !logModTime.IsZero() && logModTime.After(last) {
		last = logModTime
	}
	t.lastActivity = last

	if t.limit <= 0 || runnerAlive {
		return false, ""
	}
	silent := now.Sub(last)
	if silent <= t.limit {
		return false, ""
	}
	return true, fmt.Sprintf(
		"pi print run exited without a completion or verdict (silent for %s after the runner stopped, limit %s)",
		silent.Round(time.Second), t.limit)
}

// ExitSilenceError returns an error that wraps ErrWorkerStalled with the
// reason, so the executor's stall retry path handles a silent exit the same
// way it handles a session-idle or SSE stall.
func ExitSilenceError(reason string) error {
	return fmt.Errorf("%s: %w", reason, ErrWorkerStalled)
}

// Reset restarts the silence clock from now. Design §6.ii: when a parked
// (await-external) worker is resumed, the silence accumulated while it
// intentionally waited was by design, not a stall.
func (t *ExitSilenceTracker) Reset(now time.Time) {
	t.lastActivity = now
}
