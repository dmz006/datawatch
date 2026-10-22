package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	auditpkg "github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/session"
)

// budgetTestClock is a manually advanced capacity.Clock; After channels fire
// when Advance reaches their deadline.
type budgetTestClock struct {
	mu      sync.Mutex
	now     time.Time
	pending []budgetTestTimer
}

type budgetTestTimer struct {
	at time.Time
	ch chan time.Time
}

func (c *budgetTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *budgetTestClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.pending = append(c.pending, budgetTestTimer{at: c.now.Add(d), ch: ch})
	return ch
}

func (c *budgetTestClock) Advance(d time.Duration) {
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

func (c *budgetTestClock) pendingCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pending)
}

// newFullBudgetLedger returns a ledger on clk where llm "claude-code" has a
// $5 one-hour rolling budget and has already spent $5 (the window is full).
func newFullBudgetLedger(clk *budgetTestClock) *autonomous.BudgetLedger {
	bl := autonomous.NewBudgetLedger("")
	bl.Clock = clk.Now
	bl.Budgets = func(llm string) *inference.BudgetWindow {
		if llm == "claude-code" {
			return &inference.BudgetWindow{CeilingUSD: 5, Hours: 1}
		}
		return nil
	}
	bl.Record("claude-code", clk.Now(), 5, 100, 100)
	return bl
}

func TestBudgetAdmissionWaitsAndResumes(t *testing.T) {
	clk := &budgetTestClock{now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
	bl := newFullBudgetLedger(clk)
	led := capacity.New(capacity.Options{
		PollInterval:           time.Second,
		Clock:                  clk,
		AutonomousBackpressure: budgetBackpressure(bl),
	})
	pools := []string{"host", "llm:claude-code"}

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
	if !strings.Contains(reason, "budget window full") {
		t.Errorf("wait reason should name the full budget window, got %q", reason)
	}
	mu.Unlock()

	// Still inside the window: the request stays queued.
	clk.Advance(30 * time.Minute)
	select {
	case err := <-done:
		t.Fatalf("request granted while the window was still full: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	clk.Advance(31 * time.Minute)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("admission after rollover: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("request not admitted after the window rolled over")
	}
}

func TestBudgetPressureComposed(t *testing.T) {
	clk := &budgetTestClock{now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
	bl := newFullBudgetLedger(clk)
	bo := autonomous.NewBackoff()
	bo.Clock = clk.Now
	bo.Set("pi-prompt", clk.Now().Add(time.Minute), "rate-limit", autonomous.BackoffSourceRateLimit)
	bo.Set("claude-code", clk.Now().Add(time.Minute), "rate-limit", autonomous.BackoffSourceRateLimit)
	gate := composeBackpressure(backoffBackpressure(bo), budgetBackpressure(bl))

	if blocked, why := gate("llm:pi-prompt"); !blocked || !strings.HasPrefix(why, "backoff: ") {
		t.Errorf("llm:pi-prompt should be blocked by its back-off, got %v %q", blocked, why)
	}
	// Both gates block claude-code: the first one's answer wins.
	if blocked, why := gate("llm:claude-code"); !blocked || !strings.HasPrefix(why, "backoff: ") {
		t.Errorf("llm:claude-code should report the back-off first, got %v %q", blocked, why)
	}
	bo.Clear("claude-code")
	if blocked, why := gate("llm:claude-code"); !blocked || !strings.Contains(why, "budget window full") {
		t.Errorf("llm:claude-code should be blocked by the budget once the back-off clears, got %v %q", blocked, why)
	}
	for _, pool := range []string{"llm:unbudgeted", "host", "node:claude-code", "ext:claude-code"} {
		if blocked, why := gate(pool); blocked {
			t.Errorf("pool %q must not be blocked, got %q", pool, why)
		}
	}
	if blocked, _ := budgetBackpressure(nil)("llm:claude-code"); blocked {
		t.Error("a nil budget ledger blocks nothing")
	}
	if blocked, _ := composeBackpressure(nil, budgetBackpressure(bl))("host"); blocked {
		t.Error("nil gates are skipped and host is never blocked")
	}
}

func TestBudgetInteractiveNeverGated(t *testing.T) {
	clk := &budgetTestClock{now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
	bl := newFullBudgetLedger(clk)
	if blocked, _ := bl.Blocked("claude-code"); !blocked {
		t.Fatal("test setup: budget window should be full")
	}
	led := capacity.New(capacity.Options{
		PollInterval:           time.Second,
		Clock:                  clk,
		AutonomousBackpressure: budgetBackpressure(bl),
	})
	req := capacity.Request{Holder: "interactive-1", Pools: []string{"host", "llm:claude-code"}, Interactive: true}
	if err := led.Acquire(context.Background(), req, time.Second, nil, nil); err != nil {
		t.Fatalf("an interactive start must not wait on a full budget: %v", err)
	}
}

func TestBudgetTransitionsAuditedOncePerChange(t *testing.T) {
	clk := &budgetTestClock{now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
	bl := newFullBudgetLedger(clk)
	bl.LLMs = func() []string { return []string{"claude-code"} }
	var actions []string
	audit := func(action string, details map[string]any) {
		if details["llm"] != "claude-code" {
			t.Errorf("audit details missing llm: %v", details)
		}
		actions = append(actions, action)
	}
	full := map[string]bool{}

	for i := 0; i < 3; i++ {
		auditBudgetTransitions(bl, full, audit)
	}
	if len(actions) != 1 || actions[0] != auditpkg.ActionBudgetCeilingWait {
		t.Fatalf("a full window should emit exactly one ceiling-wait event, got %v", actions)
	}

	clk.Advance(2 * time.Hour)
	for i := 0; i < 3; i++ {
		auditBudgetTransitions(bl, full, audit)
	}
	if len(actions) != 2 || actions[1] != auditpkg.ActionBudgetWindowResumed {
		t.Fatalf("a rolled-over window should emit exactly one resumed event, got %v", actions)
	}
}

func TestUsageSpendRecordedBeforeBudgetExists(t *testing.T) {
	clk := &budgetTestClock{now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
	bl := autonomous.NewBudgetLedger("")
	bl.Clock = clk.Now
	hook := recordUsageSpend(bl, func() *inference.Registry { return nil })

	hook(session.UsageEvent{Backend: "claude-code", At: clk.Now(), TokensIn: 10, TokensOut: 10, USD: 2})
	hook(session.UsageEvent{Backend: "claude-code", At: clk.Now(), TokensIn: 10, TokensOut: 10, USD: 0})

	bl.Budgets = func(llm string) *inference.BudgetWindow {
		return &inference.BudgetWindow{CeilingUSD: 10, Hours: 1}
	}
	if got := bl.SpendInWindow("claude-code"); got != 2 {
		t.Errorf("spend recorded before the budget existed = %v, want 2", got)
	}
}
