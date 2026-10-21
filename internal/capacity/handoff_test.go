package capacity

import (
	"context"
	"testing"
)

// TestHandoffKeepsHeldCountConstant verifies the core guarantee: a worker
// lease handed to the verifier's name still holds exactly one slot, the
// waiters never see the slot free, and the slot is granted (in order) when
// the verifier release it.
func TestHandoffKeepsHeldCountConstant(t *testing.T) {
	clk := newFakeClock()
	l := nqLedger(clk, 1) // node:alpha limited to one
	ctx := context.Background()

	if err := l.Acquire(ctx, Request{Holder: "worker", PRDID: "P", Pools: []string{"node:alpha"}, Node: "alpha"}, 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	second := nqAcquire(l, Request{Holder: "second", PRDID: "Q", Pools: []string{"node:alpha"}})
	waitPending(t, clk, 1)

	verify := "verify:worker"
	if !l.Handoff("worker", verify) {
		t.Fatal("handoff of a held lease must succeed")
	}
	if l.Held("worker") {
		t.Fatal("worker still holds a lease after handoff")
	}
	if !l.Held(verify) {
		t.Fatal("verifier holds no lease after handoff")
	}
	st := l.Snapshot()
	if len(st.Leases) != 1 {
		t.Fatalf("handoff must keep exactly one lease, got %d: %+v", len(st.Leases), st.Leases)
	}
	found := false
	for _, p := range st.Pools {
		if p.Name == "node:alpha" {
			found = true
			if p.Held != 1 {
				t.Fatalf("node:alpha held = %d after handoff, want 1 (handoff must not change pool counts)", p.Held)
			}
		}
	}
	if !found {
		t.Fatal("node:alpha missing from snapshot")
	}
	if l.Held("second") {
		t.Fatal("a waiter must not be admitted while the slot is in hand-off (no slot was freed)")
	}

	// The hand-off must not re-order the queue either: releasing the
	// verifier lease grants the already-queued waiter.
	l.Release(verify)
	nqExpectGranted(t, second, "second")
	if !l.Held("second") {
		t.Fatal("second holds no lease after its grant")
	}
}

// TestHandoffRefusedLeavesNothingChanged covers the false paths: unknown
// source, empty or equal names, and hand-off into an already-held name.
func TestHandoffRefusedLeavesNothingChanged(t *testing.T) {
	l := nqLedger(newFakeClock(), 1)
	if l.Handoff("nobody", "x") {
		t.Fatal("handoff from a non-holder must be refused")
	}
	if l.Handoff("", "x") || l.Handoff("x", "") || l.Handoff("x", "x") {
		t.Fatal("empty or equal holder names must be refused")
	}
	if n := len(l.Snapshot().Leases); n != 0 {
		t.Fatalf("refused handoffs must not create leases, got %d", n)
	}

	ctx := context.Background()
	// Three distinct leases on distinct (unlimited) pools.
	for _, c := range []struct{ holder, pool string }{{"a", "pool-a"}, {"b", "pool-b"}, {"c", "pool-c"}} {
		if err := l.Acquire(ctx, Request{Holder: c.holder, PRDID: "P", Pools: []string{c.pool}}, 0, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if l.Handoff("c", "a") {
		t.Fatal("handoff into an already-held name must be refused")
	}
	if !l.Held("a") || !l.Held("c") {
		t.Fatal("a held lease must survive a refused handoff into it")
	}
}

// TestHandoffDoubleReleaseIsSafe: releasing under the old name after a
// hand-off is a no-op, and releasing the new name twice grants the queue
// exactly once.
func TestHandoffDoubleReleaseIsSafe(t *testing.T) {
	clk := newFakeClock()
	l := nqLedger(clk, 1)
	ctx := context.Background()
	if err := l.Acquire(ctx, Request{Holder: "worker", PRDID: "P", Pools: []string{"node:alpha"}}, 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	next := nqAcquire(l, Request{Holder: "next", PRDID: "Q", Pools: []string{"node:alpha"}})
	waitPending(t, clk, 1)

	verify := "verify:worker"
	if !l.Handoff("worker", verify) {
		t.Fatal("handoff must succeed")
	}
	l.Release("worker") // old-name release: no-op
	if !l.Held(verify) {
		t.Fatal("releasing the old holder name must not free the verifier's lease")
	}
	if l.Held("next") {
		t.Fatal("waiter admitted early by the old-name release")
	}

	l.Release(verify)
	nqExpectGranted(t, next, "next")
	l.Release(verify) // second release: no-op
	st := l.Snapshot()
	if len(st.Leases) != 1 || !l.Held("next") {
		t.Fatalf("state after double release = %+v; want exactly next's lease", st)
	}
	for _, p := range st.Pools {
		if p.Name == "node:alpha" && p.Held != 1 {
			t.Fatalf("node:alpha held = %d, want 1 (double release must not double-free)", p.Held)
		}
	}
}

// TestHandoffPreservesSessionBinding: the reaper and anything else that keys
// on the lease's session must keep seeing the same session after the re-key.
func TestHandoffPreservesSessionBinding(t *testing.T) {
	l := nqLedger(newFakeClock(), 1)
	if err := l.Acquire(context.Background(), Request{Holder: "worker", PRDID: "P", Pools: []string{"node:alpha"}}, 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	const sess = "s-123"
	l.Bind("worker", sess)

	verify := "verify:worker"
	if !l.Handoff("worker", verify) {
		t.Fatal("handoff must succeed")
	}
	ls := l.LeaseOf(verify)
	if ls == nil {
		t.Fatal("LeaseOf(verify holder) = nil after handoff")
	}
	if ls.SessionID != sess {
		t.Fatalf("session binding = %q, want %q — Reap must still see the worker's session", ls.SessionID, sess)
	}
	if ls.PRDID != "P" || len(ls.Pools) != 1 || ls.Pools[0] != "node:alpha" {
		t.Fatalf("lease fields did not travel with the hand-off: %+v", ls)
	}
	if l.LeaseOf("worker") != nil {
		t.Fatal("old holder name must not resolve to a lease after handoff")
	}
}

// TestHandoffKeepsWaiterRoleOrdering: hand-off must not touch the wait
// queue at all — when the verifier releases, role ranking (verifier ahead of
// planner) still decides who is granted first.
func TestHandoffKeepsWaiterRoleOrdering(t *testing.T) {
	clk := newFakeClock()
	l := nqLedger(clk, 1)
	ctx := context.Background()
	if err := l.Acquire(ctx, Request{Holder: "worker", PRDID: "P", Pools: []string{"node:alpha"}, Role: RoleWorker}, 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	planner := nqAcquire(l, Request{Holder: "planner", PRDID: "P1", Pools: []string{"node:alpha"}, Role: RolePlanner})
	waitPending(t, clk, 1)
	verifier := nqAcquire(l, Request{Holder: "verifier", PRDID: "P2", Pools: []string{"node:alpha"}, Role: RoleVerifier})
	waitPending(t, clk, 2)

	if !l.Handoff("worker", "verify:worker") {
		t.Fatal("handoff must succeed")
	}
	// A poll tick while the slot is held by nobody-new must admit no one.
	clk.Advance(nqPoll)
	if l.Held("planner") || l.Held("verifier") {
		t.Fatal("a waited-in waiter was admitted while the pool is still full")
	}

	l.Release("verify:worker")
	nqExpectGranted(t, verifier, "verifier") // role rank beats the planner's earlier queue position
	if l.Held("planner") {
		t.Fatal("planner must not hold a slot while the verifier does")
	}
	// The planner is granted only once the verifier frees its slot.
	l.Release("verifier")
	nqExpectGranted(t, planner, "planner")
}
