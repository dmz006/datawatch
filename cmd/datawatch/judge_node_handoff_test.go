package main

import (
	"context"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
)

func judgeHandoffManager(t *testing.T, led *capacity.Ledger, worker string) *autonomouspkg.Manager {
	t.Helper()
	mgr, err := autonomouspkg.NewManager(t.TempDir(), autonomouspkg.Config{CapacityWaitTimeoutSeconds: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	mgr.SetCapacity(led, func(string, string) ([]string, string) {
		return []string{"node:n1"}, "n1"
	})
	mgr.SetNodeLoad(func(context.Context, string, string, func(string)) error { return nil }, nil)
	if err := led.Acquire(context.Background(), capacity.Request{
		Holder: worker, PRDID: "prd-g", Pools: []string{"node:n1"}, Node: "n1", Role: capacity.RoleWorker,
	}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	return mgr
}

// The guardrail judge of a unit whose worker lease still holds the pool's
// only slot is handed that lease instead of queueing behind it — with the
// default multi-hour capacity wait that ordering is the pool self-deadlock
// (the judge waits on a slot its own just-finished worker holds).
func TestGuardrailJudgeHandsOffItsOwnWorkersLease(t *testing.T) {
	led := capacity.New(capacity.Options{PollInterval: 2 * time.Millisecond})
	led.SetLimit("node:n1", 1)
	const unit = "unit-1"
	mgr := judgeHandoffManager(t, led, unit)
	defer led.Release(unit)

	release, _, err := judgeNodeAdmitter(mgr, newVerifierRegistry())(
		context.Background(), "pi", "ollama/llama3.2:1b", unit, "prd-g")
	if err != nil {
		t.Fatalf("judge admission should hand over the unit's own worker lease, got %v", err)
	}
	defer release()
	// A one-node entry returns "" as the placement node; the lease says where
	// the judge actually landed.
	jl := led.LeaseOf(autonomouspkg.JudgeHolder(unit))
	if jl == nil {
		t.Fatalf("the judge holds no lease: %+v", led.Snapshot().Leases)
	}
	if jl.Node != "n1" {
		t.Fatalf("judge lease is on %q, want n1 (the worker's node): %+v", jl.Node, *jl)
	}
	if led.Held(unit) {
		t.Fatalf("the worker's lease is still held after the hand-off: %+v", led.Snapshot().Leases)
	}
	if len(led.Snapshot().Leases) != 1 {
		t.Fatalf("the unit must hold exactly one lease after the hand-off, got %d", len(led.Snapshot().Leases))
	}
}

// A unit that does not own the slot cannot steal it: its judge waits (and is
// released by the short bound) without taking the worker's lease.
func TestGuardrailJudgeCannotTakeAWorkerSlotItDoesNotOwn(t *testing.T) {
	led := capacity.New(capacity.Options{PollInterval: 2 * time.Millisecond})
	led.SetLimit("node:n1", 1)
	mgr := judgeHandoffManager(t, led, "unit-1")
	defer led.Release("unit-1")

	release, _, err := judgeNodeAdmitter(mgr, newVerifierRegistry())(
		context.Background(), "pi", "ollama/llama3.2:1b", "unit-2", "prd-g")
	if err == nil {
		release()
		t.Fatal("a foreign unit's judge was admitted onto a slot owned by another worker")
	}
	if led.Held("unit-2") {
		t.Fatalf("stray unit-2 lease after a refused admission: %+v", led.Snapshot().Leases)
	}
	if !led.Held("unit-1") {
		t.Fatalf("unit-1 lost its slot to a foreign judge's admission attempt: %+v", led.Snapshot().Leases)
	}
}
