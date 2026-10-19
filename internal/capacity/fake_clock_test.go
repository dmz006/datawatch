package capacity

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeClock is a manually advanced Clock. Its After channels fire only when
// Advance or FireAll is called, so a test decides exactly when a wait expires.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	pending []fakeTimer
}

type fakeTimer struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)}
}

// Now returns the current fake time.
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// After registers a timer that fires once the fake time reaches now+d. A
// non-positive d fires at once, as time.After does.
func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.pending = append(c.pending, fakeTimer{at: c.now.Add(d), ch: ch})
	return ch
}

// Advance moves the fake time forward by d and fires every pending timer that
// is now due.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	kept := c.pending[:0]
	for _, t := range c.pending {
		if !t.at.After(c.now) {
			t.ch <- c.now
			continue
		}
		kept = append(kept, t)
	}
	c.pending = kept
}

// FireAll fires every pending timer immediately, whatever its deadline,
// without moving the fake time.
func (c *fakeClock) FireAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range c.pending {
		t.ch <- c.now
	}
	c.pending = nil
}

// Pending reports how many After channels have not fired yet.
func (c *fakeClock) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pending)
}

// waitPending blocks until at least n timers are registered, so a test does
// not advance time before a goroutine has started waiting.
func waitPending(t *testing.T, c *fakeClock, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for c.Pending() < n {
		if time.Now().After(deadline) {
			t.Fatalf("want %d pending timers, have %d", n, c.Pending())
		}
		time.Sleep(time.Millisecond)
	}
}

func TestFakeClockFiresOnlyDueTimers(t *testing.T) {
	c := newFakeClock()
	start := c.Now()
	short := c.After(5 * time.Second)
	long := c.After(10 * time.Second)
	c.Advance(5 * time.Second)
	select {
	case at := <-short:
		if !at.Equal(start.Add(5 * time.Second)) {
			t.Fatalf("short timer fired at %v", at)
		}
	default:
		t.Fatal("short timer should have fired")
	}
	select {
	case <-long:
		t.Fatal("long timer fired early")
	default:
	}
	if c.Pending() != 1 {
		t.Fatalf("pending = %d, want 1", c.Pending())
	}
	c.FireAll()
	select {
	case <-long:
	default:
		t.Fatal("FireAll must fire the remaining timer")
	}
	if c.Pending() != 0 {
		t.Fatalf("pending after FireAll = %d, want 0", c.Pending())
	}
}

// The ledger's wait deadline and poll ticks come from the injected clock, so
// a timeout expires on fake time without any real sleep.
func TestFakeClockDrivesAcquireTimeout(t *testing.T) {
	clk := newFakeClock()
	l := New(Options{PollInterval: time.Minute, Clock: clk})
	l.SetLimit("p", 1)
	ctx := context.Background()
	if err := l.Acquire(ctx, Request{Holder: "seed", Pools: []string{"p"}}, 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- l.Acquire(ctx, Request{Holder: "w", Pools: []string{"p"}}, 10*time.Minute, nil, nil)
	}()
	waitPending(t, clk, 2) // wait deadline and first poll tick
	if got := l.Snapshot().Waiting; len(got) != 1 || !got[0].Since.Equal(clk.Now()) {
		t.Fatalf("waiter must be stamped with fake time, got %+v", got)
	}
	clk.Advance(9 * time.Minute)
	select {
	case err := <-done:
		t.Fatalf("returned before the deadline: %v", err)
	default:
	}
	clk.Advance(time.Minute)
	if err := <-done; !errors.Is(err, ErrWaitTimeout) {
		t.Fatalf("want timeout on fake deadline, got %v", err)
	}
}
