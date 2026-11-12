package autonomous

import (
	"context"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// fitsFixture wires a real ledger with pi-prompt on llm:pi-prompt (limit 2) and
// node:datawatch, and claude-code on llm:claude-code (limit 2).
func fitsFixture(t *testing.T) (*Manager, *capacity.Ledger) {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	led.SetLimit("llm:pi-prompt", 2)
	led.SetLimit("llm:claude-code", 2)
	led.SetLimit("node:datawatch", 2)
	m.SetCapacity(led, func(backend, model string) ([]string, string) {
		switch backend {
		case "pi-prompt":
			return []string{"llm:pi-prompt", "node:datawatch"}, "datawatch"
		case "claude-code":
			return []string{"llm:claude-code"}, ""
		}
		return nil, ""
	})
	return m, led
}

func fitsHold(t *testing.T, led *capacity.Ledger, holder string) {
	t.Helper()
	req := capacity.Request{
		Holder: holder, Pools: []string{"llm:pi-prompt", "node:datawatch"}, Node: "datawatch",
		Role: capacity.RoleWorker,
	}
	if err := led.Acquire(context.Background(), req, time.Second, func() bool { return false }, func(string) {}); err != nil {
		t.Fatalf("acquire %s: %v", holder, err)
	}
}

func TestManagerJudgeFitsFullPool(t *testing.T) {
	m, led := fitsFixture(t)
	fitsHold(t, led, "w1")
	fitsHold(t, led, "w2")
	j := JudgeAdmit{Holder: "judge", Backend: "pi-prompt", Model: "q"}
	if m.JudgeFits(j) {
		t.Fatal("JudgeFits true with llm:pi-prompt at 2 of 2")
	}
	// A held worker lease is ignored: it does not make the judge fit.
	j.WorkerHolder = "w1"
	if m.JudgeFits(j) {
		t.Fatal("JudgeFits true because of WorkerHolder")
	}
	if led.LeaseOf("w1") == nil {
		t.Fatal("JudgeFits released the worker lease")
	}
}

func TestManagerJudgeFitsFreeSlots(t *testing.T) {
	m, led := fitsFixture(t)
	fitsHold(t, led, "w1")
	fitsHold(t, led, "w2")
	if !m.JudgeFits(JudgeAdmit{Holder: "judge", Backend: "claude-code", Model: "m"}) {
		t.Fatal("JudgeFits false for claude-code with free slots")
	}
}

func TestManagerJudgeFitsAfterRelease(t *testing.T) {
	m, led := fitsFixture(t)
	fitsHold(t, led, "w1")
	fitsHold(t, led, "w2")
	j := JudgeAdmit{Holder: "judge", Backend: "pi-prompt", Model: "q"}
	if m.JudgeFits(j) {
		t.Fatal("JudgeFits true while full")
	}
	led.Release("w1")
	if !m.JudgeFits(j) {
		t.Fatal("JudgeFits false after a holder was released")
	}
}

func TestManagerJudgeFitsLeavesLedgerUntouched(t *testing.T) {
	m, led := fitsFixture(t)
	fitsHold(t, led, "w1")
	fitsHold(t, led, "w2")
	before := led.Snapshot()
	j := JudgeAdmit{Holder: "judge", Backend: "pi-prompt", Model: "q"}
	m.JudgeFits(j)
	m.JudgeFits(JudgeAdmit{Holder: "judge2", Backend: "claude-code", Model: "m"})
	after := led.Snapshot()
	if led.LeaseOf("judge") != nil || led.LeaseOf("judge2") != nil {
		t.Fatal("JudgeFits took a lease")
	}
	if len(after.Waiting) != 0 {
		t.Fatalf("JudgeFits queued a waiter: %+v", after.Waiting)
	}
	if len(after.Leases) != len(before.Leases) {
		t.Fatalf("leases changed: %d -> %d", len(before.Leases), len(after.Leases))
	}
}

func TestManagerJudgeFitsNoLedger(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	if !m.JudgeFits(JudgeAdmit{Holder: "judge", Backend: "pi-prompt", Model: "q"}) {
		t.Fatal("JudgeFits false with no ledger wired")
	}
}

func TestManagerCapacityChangedFiresOnRelease(t *testing.T) {
	m, led := fitsFixture(t)
	fitsHold(t, led, "w1")
	ch := m.CapacityChanged()
	if ch == nil {
		t.Fatal("CapacityChanged nil with a ledger wired")
	}
	select {
	case <-ch:
		t.Fatal("CapacityChanged ready before any change")
	default:
	}
	led.Release("w1")
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("CapacityChanged did not fire after Release")
	}
}

func TestManagerCapacityChangedNoLedger(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	ch := m.CapacityChanged()
	select {
	case <-ch:
		t.Fatal("CapacityChanged fired with no ledger")
	case <-time.After(50 * time.Millisecond):
	}
}
