package autonomous

import (
	"fmt"
	"time"
)

// DescendantStallTracker detects a worker in descendant-only silence: the
// pi process has exited but a child process (workDescendants > 0) is still
// alive and no other liveness has been observed. The clock is the latest of
// the tracker start, the previous observation, the log mtime and the newest
// workdir file mtime, so any real write activity resets it. It uses the same
// shape as ExitSilenceTracker but conditions on descendant count and adds the
// workdir newest-file signal.
type DescendantStallTracker struct {
	limit        time.Duration
	lastActivity time.Time
}

// NewDescendantStallTracker returns a tracker measuring silence from start
// with the given limit. limit <= 0 disables detection (never stalled).
func NewDescendantStallTracker(start time.Time, limit time.Duration) *DescendantStallTracker {
	return &DescendantStallTracker{limit: limit, lastActivity: start}
}

// Observe reports whether a worker with live descendant processes has
// produced no new evidence past the limit. workDescendants == 0 means the
// worker is not in descendant-only silence, so the clock simply moves to now
// and it returns not stalled. Otherwise the clock is the latest of
// lastActivity, logModTime and workdirNewest (zero times ignored), and it
// stalls when now minus that clock is greater than limit.
func (t *DescendantStallTracker) Observe(now time.Time, workDescendants int, logModTime time.Time, workdirNewest time.Time) (bool, string) {
	if workDescendants == 0 {
		t.lastActivity = now
		return false, ""
	}

	last := t.lastActivity
	if !logModTime.IsZero() && logModTime.After(last) {
		last = logModTime
	}
	if !workdirNewest.IsZero() && workdirNewest.After(last) {
		last = workdirNewest
	}
	t.lastActivity = last

	if t.limit <= 0 {
		return false, ""
	}
	silent := now.Sub(last)
	if silent <= t.limit {
		return false, ""
	}
	return true, fmt.Sprintf(
		"worker in descendant-only silence for %s (limit %s); the only liveness was a child process",
		silent.Round(time.Second), t.limit)
}

// DescendantStallError returns an error that wraps ErrWorkerStalled with the
// reason, so the executor's stall retry path handles a descendant-only
// silence the same way it handles a session-idle or SSE stall.
func DescendantStallError(reason string) error {
	return fmt.Errorf("%s: %w", reason, ErrWorkerStalled)
}

// Reset restarts the silence clock from now.
func (t *DescendantStallTracker) Reset(now time.Time) {
	t.lastActivity = now
}
