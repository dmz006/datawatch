package autonomous

import (
	"testing"
	"time"
)

// runLimitClock is a manually advanced clock used to drive VerifierRunLimit.
type runLimitClock struct{ now time.Time }

func (c *runLimitClock) advance(d time.Duration) time.Time {
	c.now = c.now.Add(d)
	return c.now
}

func TestVerifierRunLimitProgressingVerifierEndedAtCap(t *testing.T) {
	clock := &runLimitClock{now: time.Unix(1000, 0)}
	limit := 10 * time.Minute
	l := NewVerifierRunLimit(limit)
	if l.Decide(clock.now, SlotHeld) {
		t.Fatal("ended before any run time accumulated")
	}
	// The verifier keeps making progress every minute; progress is not an
	// input, so the cap still trips at exactly the limit.
	for i := 1; i < 10; i++ {
		if l.Decide(clock.advance(time.Minute), SlotHeld) {
			t.Fatalf("ended early at %d minutes", i)
		}
	}
	if !l.Decide(clock.advance(time.Minute), SlotHeld) {
		t.Fatal("progressing verifier not ended at the cap")
	}
	if got := l.Elapsed(clock.now); got != limit {
		t.Fatalf("Elapsed = %v, want %v", got, limit)
	}
	if !l.Decide(clock.advance(time.Minute), SlotHeld) {
		t.Fatal("cap released after tripping; it must stay tripped")
	}
}

func TestVerifierRunLimitFinishingBeforeCapNeverEnded(t *testing.T) {
	clock := &runLimitClock{now: time.Unix(1000, 0)}
	l := NewVerifierRunLimit(time.Hour)
	l.Decide(clock.now, SlotNone)
	// The verifier finishes after 5 minutes. No further observation is made,
	// so the cap is never consulted with a tripped result.
	for i := 0; i < 5; i++ {
		if l.Decide(clock.advance(time.Minute), SlotNone) {
			t.Fatalf("ended after %d minutes, before the one-hour cap", i+1)
		}
	}
	if got := l.Elapsed(clock.now); got != 5*time.Minute {
		t.Fatalf("Elapsed = %v, want 5m0s", got)
	}
}

func TestVerifierRunLimitZeroNeverEnds(t *testing.T) {
	clock := &runLimitClock{now: time.Unix(1000, 0)}
	l := NewVerifierRunLimit(0)
	for i := 0; i < 1000; i++ {
		if l.Decide(clock.advance(time.Hour), SlotHeld) {
			t.Fatalf("zero limit ended the run at observation %d", i)
		}
	}
	// The first observation has no earlier one to measure from, so 1000
	// observations one hour apart span 999 hours.
	if got := l.Elapsed(clock.now); got != 999*time.Hour {
		t.Fatalf("Elapsed = %v, want 999h0m0s; a zero limit still records run time", got)
	}
}

func TestVerifierRunLimitWaitingForSlotDoesNotCount(t *testing.T) {
	clock := &runLimitClock{now: time.Unix(1000, 0)}
	limit := 10 * time.Minute
	l := NewVerifierRunLimit(limit)
	l.Decide(clock.now, SlotWaiting)
	// Two hours queued for a slot must not count toward the 10-minute cap.
	for i := 0; i < 120; i++ {
		if l.Decide(clock.advance(time.Minute), SlotWaiting) {
			t.Fatalf("waiting time counted toward the limit at minute %d", i+1)
		}
	}
	if got := l.Elapsed(clock.now); got != 0 {
		t.Fatalf("Elapsed while only waiting = %v, want 0", got)
	}
	// Once the slot is held, the run time counts again and the cap trips
	// after exactly the limit of held time.
	for i := 1; i < 10; i++ {
		if l.Decide(clock.advance(time.Minute), SlotHeld) {
			t.Fatalf("ended after %d held minutes, before the limit", i)
		}
	}
	if !l.Decide(clock.advance(time.Minute), SlotHeld) {
		t.Fatal("not ended at the limit of held run time")
	}
}

func TestVerifierRunLimitIgnoresBackwardClock(t *testing.T) {
	clock := &runLimitClock{now: time.Unix(1000, 0)}
	l := NewVerifierRunLimit(time.Minute)
	l.Decide(clock.now, SlotHeld)
	if l.Decide(clock.now.Add(-time.Hour), SlotHeld) {
		t.Fatal("a backward clock step ended the run")
	}
	if got := l.Elapsed(clock.now); got != 0 {
		t.Fatalf("backward step added run time: %v", got)
	}
}
