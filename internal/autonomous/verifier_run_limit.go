package autonomous

import "time"

// VerifierRunLimit is a progress-blind cap on how long one verifier run may
// accumulate run time. It is a pure clock: it never reads wall time itself,
// so the caller passes `now` to every call and tests drive it with a fake
// clock.
//
// Run time accumulates between observations. The interval since the previous
// observation is attributed to the slot state seen at the current observation,
// and it is excluded when that state is SlotWaiting: time spent queued for a
// capacity slot does not count toward the limit. The cap ignores progress
// signals entirely; it trips at the limit even if the verifier keeps working.
type VerifierRunLimit struct {
	// limit is the maximum accumulated run time. Zero means no cap.
	limit time.Duration

	// started reports whether an observation has been recorded, so the first
	// Decide call has no interval to attribute.
	started bool
	// last is the time of the most recent observation.
	last time.Time
	// elapsed is the accumulated run time, excluding waiting time.
	elapsed time.Duration
}

// NewVerifierRunLimit returns a run-limit clock with the given cap. A limit of
// zero (or less) means the run is never capped.
func NewVerifierRunLimit(limit time.Duration) *VerifierRunLimit {
	return &VerifierRunLimit{limit: limit}
}

// Decide records an observation at `now` with the verifier's slot state and
// reports whether the accumulated run time has reached the limit. Once it
// returns true it keeps returning true for later observations, because the
// accumulated run time never decreases. A zero limit never returns true.
func (l *VerifierRunLimit) Decide(now time.Time, slot SlotState) bool {
	if l.started && now.After(l.last) && slot != SlotWaiting {
		l.elapsed += now.Sub(l.last)
	}
	l.started = true
	l.last = now
	if l.limit <= 0 {
		return false
	}
	return l.elapsed >= l.limit
}

// Elapsed returns the accumulated run time as of the most recent observation,
// for the decision record. The `now` argument is accepted so callers pass the
// same clock reading they use for Decide; it does not add time on its own,
// because the slot state for the interval after the last observation is not
// known yet.
func (l *VerifierRunLimit) Elapsed(now time.Time) time.Duration {
	_ = now
	return l.elapsed
}
