package autonomous

import "time"

// JudgeClock returns "now" for one judge attempt. Production passes
// time.Now; tests pass a clock they advance by hand, so the timing rules
// can be exercised without real waiting.
type JudgeClock func() time.Time

// JudgeRunTimer measures the time of one judge attempt against its
// capacity slot. The time limit starts when the judge's run starts — that
// is, when the slot is granted, by Grant — and never from when the judge
// began waiting for the slot. The wait is recorded separately, by Waiting,
// and is never counted against the limit; the run is recorded separately,
// by Elapsed, and is what the limit bounds. One timer per attempt.
type JudgeRunTimer struct {
	// Clock returns "now"; nil means time.Now.
	Clock JudgeClock
	// Limit bounds the run. Zero or negative means the run has no limit.
	Limit time.Duration

	waitStart time.Time
	waiting   bool
	runStart  time.Time
	running   bool
	waited    time.Duration
}

func (t *JudgeRunTimer) now() time.Time {
	if t.Clock != nil {
		return t.Clock()
	}
	return time.Now()
}

// BeginWait opens the wait window for a capacity slot. Calling it again
// before Grant opens a new wait window (the judge moved to another
// backend and waits there too); the earlier wait stays counted as
// waiting.
func (t *JudgeRunTimer) BeginWait() {
	if t.waiting {
		t.waited += t.now().Sub(t.waitStart)
	}
	t.waitStart = t.now()
	t.waiting = true
}

// Grant marks the slot granted: the wait window closes out into Waiting
// and the run starts now. The limit is measured from this moment, never
// from the wait that preceded it.
func (t *JudgeRunTimer) Grant() {
	if t.waiting {
		t.waited += t.now().Sub(t.waitStart)
		t.waiting = false
	}
	t.runStart = t.now()
	t.running = true
}

// Waiting reports how long the judge has waited for its capacity slot.
// It is never counted against the limit.
func (t *JudgeRunTimer) Waiting() time.Duration {
	if t.waiting {
		return t.waited + t.now().Sub(t.waitStart)
	}
	return t.waited
}

// Elapsed reports the run time since Grant. It is zero while the judge is
// still waiting for a slot: the run has not started.
func (t *JudgeRunTimer) Elapsed() time.Duration {
	if !t.running {
		return 0
	}
	return t.now().Sub(t.runStart)
}

// Expired reports whether the run has reached the limit, at which point
// the judge is ended and recorded as timed out. A judge that is still
// waiting for its slot never expires, and neither does a judge whose run
// has no limit.
func (t *JudgeRunTimer) Expired() bool {
	if t.Limit <= 0 || !t.running {
		return false
	}
	return t.Elapsed() >= t.Limit
}
