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
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/session"
)

func newReserveWiringTest() (*autonomous.Backoff, *autonomous.BackoffReserve, func(pool string, role capacity.Role, heldByRole map[capacity.Role]int) (bool, string)) {
	bo := autonomous.NewBackoff()
	gate := autonomous.NewBackoffReserve(time.Now, func() int { return 1 }, nil)
	return bo, gate, backoffReserveBackpressure(bo, gate)
}

// The wiring closure: healthy backends never consult the reserve; during a
// back-off the worker is refused with the classic reason and the review
// roles are admitted through the slot count.
func TestBackoffReserve_WiringDecides(t *testing.T) {
	bo, gate, roleGate := newReserveWiringTest()
	free := map[capacity.Role]int{}

	// No back-off: nothing blocks, no event is queued.
	if blocked, _ := roleGate("llm:claude-code", capacity.RoleVerifier, free); blocked {
		t.Error("a healthy backend must never be gated by the reserve")
	}
	if got := gate.DrainEvents(); len(got) != 0 {
		t.Errorf("no verdict should be queued outside a back-off, got %d", len(got))
	}

	bo.Set("claude-code", time.Now().Add(time.Hour), "rate-limit", autonomous.BackoffSourceRateLimit)

	if blocked, why := roleGate("llm:claude-code", capacity.RoleWorker, free); !blocked || !strings.HasPrefix(why, "backoff: ") {
		t.Errorf("worker during back-off = %v %q, want a backoff: block", blocked, why)
	}
	if blocked, why := roleGate("llm:claude-code", capacity.RoleVerifier, free); blocked {
		t.Errorf("verifier should take the free reserve slot, got %q", why)
	}
	if blocked, why := roleGate("llm:claude-code", capacity.RolePlanner, free); blocked {
		t.Errorf("planner should take the free reserve slot, got %q", why)
	}
	// A held slot is the ledger's count: the reserve is in use.
	if blocked, why := roleGate("llm:claude-code", capacity.RoleVerifier, map[capacity.Role]int{capacity.RoleVerifier: 1}); !blocked || why != autonomous.BackoffReserveWaitReason {
		t.Errorf("held reserve = %v %q, want %q", blocked, why, autonomous.BackoffReserveWaitReason)
	}
	// The planner's held slot counts against the same pool of slots.
	if blocked, _ := roleGate("llm:claude-code", capacity.RoleVerifier, map[capacity.Role]int{capacity.RolePlanner: 1}); !blocked {
		t.Error("a planner-held slot exhausts the one-slot reserve")
	}
	// Only llm: pools are gated.
	for _, pool := range []string{"host", "node:claude-code", "ext:claude-code"} {
		if blocked, _ := roleGate(pool, capacity.RoleVerifier, free); blocked {
			t.Errorf("pool %q must never be gated by the reserve", pool)
		}
	}
	bo.Clear("claude-code")
	if blocked, _ := roleGate("llm:claude-code", capacity.RoleWorker, free); blocked {
		t.Error("once the flag clears the worker passes again")
	}
}

func TestBackoffReserve_WiringRefusesAllAtZero(t *testing.T) {
	zero := 0
	if got := config.ReserveForReviewSlots(&zero); got != 0 {
		t.Fatalf("explicit zero stays zero, got %d", got)
	}
	neg := -3
	if got := config.ReserveForReviewSlots(&neg); got != 0 {
		t.Fatalf("negative explicit values clamp to zero, got %d", got)
	}
	var unset *int
	if got := config.ReserveForReviewSlots(unset); got != 1 {
		t.Fatalf("unset reads the default 1, got %d", got)
	}

	bo := autonomous.NewBackoff()
	bo.Set("claude-code", time.Now().Add(time.Hour), "rate-limit", autonomous.BackoffSourceRateLimit)
	gate := autonomous.NewBackoffReserve(time.Now, func() int { return 0 }, nil)
	roleGate := backoffReserveBackpressure(bo, gate)
	if blocked, why := roleGate("llm:claude-code", capacity.RoleVerifier, map[capacity.Role]int{}); !blocked || why != autonomous.BackoffReserveWaitReason {
		t.Errorf("zero slots refuses the verifier too: %v %q", blocked, why)
	}
}

// The ledger admits a session that reviews a backed-off backend on the slot
// count, holds out a second reviewer while that slot is used, and keeps the
// worker behind the back-off itself until the flag expires.
func TestBackoffReserve_LedgerAdmitsReviewerWhileWorkerWaits(t *testing.T) {
	clk := &backoffTestClock{now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
	bo := autonomous.NewBackoff()
	bo.Clock = clk.Now
	bo.Set("claude-code", clk.Now().Add(time.Hour), "rate-limit", autonomous.BackoffSourceRateLimit)
	gate := autonomous.NewBackoffReserve(clk.Now, func() int { return 1 }, nil)
	led := capacity.New(capacity.Options{
		PollInterval:     time.Second,
		Clock:            clk,
		RoleBackpressure: backoffReserveBackpressure(bo, gate),
	})
	pools := []string{"llm:claude-code"}
	led.SetLimit(pools[0], 3)

	err := led.Acquire(context.Background(), capacity.Request{
		Holder: "verifier-1", Role: capacity.RoleVerifier, Pools: pools,
	}, time.Second, nil, nil)
	if err != nil {
		t.Fatalf("reviewer must be admitted on the reserve during the back-off: %v", err)
	}

	var mu sync.Mutex
	var secondReason, workerReason string
	doneSecond := make(chan bool, 1)
	doneWorker := make(chan bool, 1)
	go func() {
		doneSecond <- okOrErr(t, led, capacity.Request{Holder: "verifier-2", Role: capacity.RoleVerifier, Pools: pools}, func(r string) {
			mu.Lock()
			secondReason = r
			mu.Unlock()
		})
	}()
	go func() {
		doneWorker <- okOrErr(t, led, capacity.Request{Holder: "worker-1", Role: capacity.RoleWorker, Pools: pools}, func(r string) {
			mu.Lock()
			workerReason = r
			mu.Unlock()
		})
	}()
	waitFor(t, "both waiting with reasons", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return secondReason != "" && workerReason != ""
	})
	mu.Lock()
	second, worker := secondReason, workerReason
	mu.Unlock()
	if !strings.Contains(second, autonomous.BackoffReserveWaitReason) {
		t.Errorf("second reviewer reason = %q, want %q", second, autonomous.BackoffReserveWaitReason)
	}
	if !strings.Contains(worker, "backoff: ") || strings.Contains(worker, autonomous.BackoffReserveWaitReason) {
		t.Errorf("worker reason = %q, want the bare back-off (not the reserve)", worker)
	}

	// The first reviewer steps aside: the slot is free for the second
	// reviewer, but the worker is still behind the back-off for its hour.
	led.Release("verifier-1")
	select {
	case admitted := <-doneSecond:
		if !admitted {
			t.Fatal("second reviewer should take the freed reserve slot")
		}
		defer led.Release("verifier-2")
	case <-time.After(3 * time.Second):
		t.Fatal("second reviewer not admitted after the first released")
	}

	// The pool now holds one reviewer and one waiting worker: the worker
	// passes only once the flag itself expires.
	clk.Advance(time.Hour + time.Minute)
	select {
	case admitted := <-doneWorker:
		if !admitted {
			t.Fatal("worker should be admitted after the back-off expires (headroom in the pool)")
		}
		defer led.Release("worker-1")
	case <-time.After(3 * time.Second):
		t.Fatal("worker not admitted after the back-off expired")
	}
}

// okOrErr runs a zero-timeout Acquire and reports whether it admitted
// instead of failing the test (waiting is the expected outcome).
func okOrErr(t *testing.T, led *capacity.Ledger, req capacity.Request, onWait func(string)) bool {
	t.Helper()
	err := led.Acquire(context.Background(), req, 0, nil, onWait)
	return err == nil
}

// drainBackoffReserveAudit emits the verdicts queued on the last decisions,
// with the right action and subject.
func TestDrainBackoffReserveAudit(t *testing.T) {
	l, err := auditpkg.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	setBackoffAuditLog(l)
	defer setBackoffAuditLog(nil)

	clk := &backoffTestClock{now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
	gate := autonomous.NewBackoffReserve(clk.Now, func() int { return 1 }, nil)
	gate.Admit("claude-code", capacity.RoleVerifier, 0)
	gate.Admit("claude-code", capacity.RoleWorker, 0)

	drainBackoffReserveAudit(gate)

	es, err := l.Read(auditpkg.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 2 {
		t.Fatalf("want 2 audit entries, got %d", len(es))
	}
	actions := map[string]bool{}
	for _, e := range es {
		if e.Actor != "autonomous" {
			t.Errorf("%s actor = %q, want autonomous", e.Action, e.Actor)
		}
		if e.Subject != "claude-code" {
			t.Errorf("%s subject = %q, want claude-code", e.Action, e.Subject)
		}
		if e.Details["role"] == nil || e.Details["slots"] == nil || e.Details["used_slots"] == nil {
			t.Errorf("%s details missing role/slots: %v", e.Action, e.Details)
		}
		actions[e.Action] = true
	}
	if !actions[auditpkg.ActionUsageBackoffReserveAdmitted] || !actions[auditpkg.ActionUsageBackoffReserveRefused] {
		t.Errorf("want one admitted and one refused entry, got %v", actions)
	}

	drainBackoffReserveAudit(gate) // draining an empty queue emits nothing
	es, _ = l.Read(auditpkg.QueryFilter{})
	if len(es) != 2 {
		t.Errorf("an empty drain must emit nothing, got %d entries", len(es))
	}
}

// recordUsage folds a session's token spend into its backend's rolling
// reserve budget alongside the other usage sinks.
func TestRecordUsageFeedsReserveTokens(t *testing.T) {
	clk := &backoffTestClock{now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
	gate := autonomous.NewBackoffReserve(clk.Now, func() int { return 1 }, func() int64 { return 1000 })
	reg := inference.NewRegistry()
	if err := reg.Add(&inference.LLM{Name: "claude-code", Kind: inference.KindClaudeCode}); err != nil {
		t.Fatal(err)
	}
	hook := recordUsage(nil, func() *inference.Registry { return reg }, nil, gate)

	hook(session.UsageEvent{SessionID: "s1", Backend: "claude-code", At: clk.Now(), TokensIn: 400, TokensOut: 100})
	if got := gate.UsedTokens("claude-code"); got != 500 {
		t.Fatalf("reserve tokens = %d, want 500", got)
	}

	// The same spend refuses the review role once the rolling budget is
	// over its cap, and admits once it has aged out of the hour.
	if admit, _ := gate.Admit("claude-code", capacity.RoleVerifier, 0); admit {
		// 500 < 1000: not full yet
	} else {
		t.Error("500 of 1000 tokens must not yet refuse the reserve")
	}
	hook(session.UsageEvent{SessionID: "s1", Backend: "claude-code", At: clk.Now(), TokensIn: 600, TokensOut: 0})
	if admit, _ := gate.Admit("claude-code", capacity.RoleVerifier, 0); admit {
		t.Error("1100 of 1000 tokens must refuse the review role")
	}
	clk.Advance(time.Hour + time.Second)
	if admit, _ := gate.Admit("claude-code", capacity.RoleVerifier, 0); !admit {
		t.Error("stamps outside the rolling hour stop counting")
	}
}
