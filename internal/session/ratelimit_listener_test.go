package session

import (
	"context"
	"sync"
	"testing"
	"time"
)

type rlEvent struct {
	backend  string
	resetAt  time.Time
	hasReset bool
}

func driveRateLimitLine(t *testing.T, mgr *Manager, sess *Session, line string) {
	t.Helper()
	var lastOutput, lastPrompt time.Time
	var pending []string
	mgr.processOutputLine(context.Background(), sess, nil, line, &lastOutput, &pending, &lastPrompt, func() *Tracker { return nil })
}

func newRateLimitTestSession(t *testing.T, mgr *Manager, suffix, backend string) *Session {
	t.Helper()
	s := newTestSession(t, mgr, suffix)
	s.BackendFamily = backend
	s.TmuxSession = "cs-" + suffix
	if err := mgr.store.Save(s); err != nil {
		t.Fatalf("save: %v", err)
	}
	return s
}

func TestRateLimitListener_FiresOncePerTransition(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)

	var mu sync.Mutex
	var first, second []rlEvent
	mgr.AddRateLimitListener(func(b string, r time.Time, h bool) {
		mu.Lock()
		first = append(first, rlEvent{b, r, h})
		mu.Unlock()
	})
	mgr.AddRateLimitListener(func(b string, r time.Time, h bool) {
		mu.Lock()
		second = append(second, rlEvent{b, r, h})
		mu.Unlock()
	})

	// The existing single-slot handler must keep working alongside.
	var stateChanges int
	mgr.SetStateChangeHandler(func(_ *Session, _ State) { stateChanges++ })

	sess := newRateLimitTestSession(t, mgr, "rl01", "pi-prompt")
	resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	driveRateLimitLine(t, mgr, sess, "DATAWATCH_RATE_LIMITED: resets at "+resetAt.Format(time.RFC3339))

	// A repeat line while already rate-limited is not a new transition.
	driveRateLimitLine(t, mgr, sess, "DATAWATCH_RATE_LIMITED: resets at "+resetAt.Format(time.RFC3339))

	mu.Lock()
	defer mu.Unlock()
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("listeners fired first=%d second=%d, want 1 each", len(first), len(second))
	}
	ev := first[0]
	if ev.backend != "pi-prompt" || !ev.hasReset || !ev.resetAt.Equal(resetAt) {
		t.Errorf("event = %+v, want backend pi-prompt, hasReset, resetAt %v", ev, resetAt)
	}
	if stateChanges != 1 {
		t.Errorf("existing state-change handler fired %d times, want 1", stateChanges)
	}
	if got, _ := mgr.store.Get(sess.FullID); got.State != StateRateLimited {
		t.Errorf("state = %s, want rate_limited", got.State)
	}
}

func TestRateLimitListener_NoResetTime(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	var got []rlEvent
	mgr.AddRateLimitListener(func(b string, r time.Time, h bool) { got = append(got, rlEvent{b, r, h}) })

	sess := newRateLimitTestSession(t, mgr, "rl02", "pi-prompt")
	driveRateLimitLine(t, mgr, sess, "DATAWATCH_RATE_LIMITED: resets at unknown")

	if len(got) != 1 {
		t.Fatalf("listener fired %d times, want 1", len(got))
	}
	if got[0].backend != "pi-prompt" || got[0].hasReset || !got[0].resetAt.IsZero() {
		t.Errorf("event = %+v, want backend pi-prompt, hasReset=false, zero resetAt", got[0])
	}
}

func TestRateLimitListener_NilIgnored(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	mgr.AddRateLimitListener(nil)
	sess := newRateLimitTestSession(t, mgr, "rl03", "pi-prompt")
	driveRateLimitLine(t, mgr, sess, "DATAWATCH_RATE_LIMITED: resets at unknown")
}
