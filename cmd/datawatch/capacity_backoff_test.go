package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
)

// backoffTestClock is a manually advanced capacity.Clock; After channels fire
// when Advance reaches their deadline.
type backoffTestClock struct {
	mu      sync.Mutex
	now     time.Time
	pending []backoffTestTimer
}

type backoffTestTimer struct {
	at time.Time
	ch chan time.Time
}

func (c *backoffTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *backoffTestClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.pending = append(c.pending, backoffTestTimer{at: c.now.Add(d), ch: ch})
	return ch
}

func (c *backoffTestClock) Advance(d time.Duration) {
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

func (c *backoffTestClock) pendingCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pending)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s not reached", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestBackoffBackpressure_GatesOnlyLLMPools(t *testing.T) {
	clk := &backoffTestClock{now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
	bo := autonomous.NewBackoff()
	bo.Clock = clk.Now
	bo.Set("claude-code", clk.Now().Add(time.Minute), "rate-limit", autonomous.BackoffSourceRateLimit)
	bp := backoffBackpressure(bo)

	blocked, why := bp("llm:claude-code")
	if !blocked || !strings.HasPrefix(why, "backoff: ") || !strings.Contains(why, "claude-code") {
		t.Fatalf("llm:claude-code should be blocked with a backoff reason, got %v %q", blocked, why)
	}
	for _, pool := range []string{"llm:pi-prompt", "host", "node:claude-code", "ext:claude-code"} {
		if b, _ := bp(pool); b {
			t.Errorf("pool %q must not be blocked by claude-code's back-off", pool)
		}
	}
	clk.Advance(2 * time.Minute)
	if b, _ := bp("llm:claude-code"); b {
		t.Error("llm:claude-code must pass once the clock is past until")
	}
	if b, _ := backoffBackpressure(nil)("llm:claude-code"); b {
		t.Error("a nil registry blocks nothing")
	}
}

// An interactive session start is admitted while its backend is backed off;
// an autonomous request for the same pool waits and is released at expiry.
func TestBackoffBackpressure_InteractiveStartNeverBlocked(t *testing.T) {
	clk := &backoffTestClock{now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
	bo := autonomous.NewBackoff()
	bo.Clock = clk.Now
	bo.Set("claude-code", clk.Now().Add(time.Minute), "rate-limit", autonomous.BackoffSourceRateLimit)
	led := capacity.New(capacity.Options{
		PollInterval:           time.Second,
		Clock:                  clk,
		AutonomousBackpressure: backoffBackpressure(bo),
	})
	pools := []string{"host", "llm:claude-code"}

	if err := led.Acquire(context.Background(), capacity.Request{Holder: "interactive-1", Pools: pools, Interactive: true}, time.Second, nil, nil); err != nil {
		t.Fatalf("interactive start must not wait on a back-off: %v", err)
	}

	var reason string
	var mu sync.Mutex
	done := make(chan error, 1)
	go func() {
		done <- led.Acquire(context.Background(), capacity.Request{Holder: "task-1", Pools: pools}, 0, nil, func(r string) {
			mu.Lock()
			reason = r
			mu.Unlock()
		})
	}()
	waitFor(t, "task waiting", func() bool { mu.Lock(); defer mu.Unlock(); return reason != "" && clk.pendingCount() >= 1 })
	mu.Lock()
	if !strings.Contains(reason, "backoff:") {
		t.Errorf("autonomous wait reason should be the back-off, got %q", reason)
	}
	mu.Unlock()

	clk.Advance(2 * time.Minute)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("autonomous admission after until: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("autonomous request not admitted after the back-off expired")
	}
}
