package capacity

import (
	"context"
	"strings"
	"testing"
	"time"
)

// nqPoll is the poll interval for the node-queue tests. It is long enough that
// no poll tick fires unless a test advances the fake clock past it.
const nqPoll = time.Minute

// nqLedger builds a ledger on the fake clock with the node:alpha pool limited
// to limit. Every other pool stays unlimited.
func nqLedger(clk *fakeClock, limit int) *Ledger {
	l := New(Options{PollInterval: nqPoll, Clock: clk})
	l.SetLimit("node:alpha", limit)
	return l
}

// nqAcquire runs Acquire in a goroutine with no timeout and returns the channel
// its result arrives on.
func nqAcquire(l *Ledger, req Request) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- l.Acquire(context.Background(), req, 0, nil, nil)
	}()
	return done
}

// nqExpectGranted waits for a grant. The grant itself is synchronous inside
// Release or Reap, so this receive only waits for the goroutine to return; the
// guard timer is a failsafe so a broken ledger fails the test instead of hanging.
func nqExpectGranted(t *testing.T, done <-chan error, holder string) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s: Acquire returned %v, want grant", holder, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: not granted", holder)
	}
}

// nqWaitingReason returns the reason holder is queued with, or fails the test
// if holder is not in the wait queue.
func nqWaitingReason(t *testing.T, l *Ledger, holder string) string {
	t.Helper()
	for _, w := range l.Snapshot().Waiting {
		if w.Holder == holder {
			return w.Reason
		}
	}
	t.Fatalf("%s is not waiting; waiting = %+v", holder, l.Snapshot().Waiting)
	return ""
}

func TestNodeQueueSecondRequestWaitsThenGrantedOnRelease(t *testing.T) {
	clk := newFakeClock()
	l := nqLedger(clk, 1)
	ctx := context.Background()
	if err := l.Acquire(ctx, Request{Holder: "first", PRDID: "prd-1", Pools: []string{"node:alpha"}}, 0, nil, nil); err != nil {
		t.Fatal(err)
	}

	done := nqAcquire(l, Request{Holder: "second", PRDID: "prd-1", Pools: []string{"node:alpha"}})
	waitPending(t, clk, 1)

	if l.Held("second") {
		t.Fatal("second holds a lease while node:alpha is full")
	}
	if reason := nqWaitingReason(t, l, "second"); !strings.Contains(reason, "node alpha") || !strings.Contains(reason, "held by first") {
		t.Fatalf("wait reason = %q, want it to name node alpha and first", reason)
	}

	l.Release("first")
	nqExpectGranted(t, done, "second")
	if !l.Held("second") {
		t.Fatal("second has no lease after the grant")
	}
}

func TestNodeQueueDifferentNodesRunTogether(t *testing.T) {
	clk := newFakeClock()
	l := nqLedger(clk, 1)
	l.SetLimit("node:beta", 1)
	ctx := context.Background()

	alpha := Request{Holder: "alpha-task", PRDID: "prd-1", Pools: []string{"node:alpha"}}
	beta := Request{Holder: "beta-task", PRDID: "prd-2", Pools: []string{"node:beta"}}
	if err := l.Acquire(ctx, alpha, 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	// beta is on a different pool with its own slot, so it is admitted at once
	// and does not wait behind alpha.
	if err := l.Acquire(ctx, beta, 0, nil, nil); err != nil {
		t.Fatalf("beta-task should run alongside alpha-task: %v", err)
	}
	if !l.Held("alpha-task") || !l.Held("beta-task") {
		t.Fatal("both node tasks should hold a lease")
	}
	if got := l.Snapshot().Waiting; len(got) != 0 {
		t.Fatalf("nothing should be waiting, got %+v", got)
	}
}

func TestNodeQueueLimitTwoAdmitsTwoAndQueuesThird(t *testing.T) {
	clk := newFakeClock()
	l := nqLedger(clk, 2)
	ctx := context.Background()
	for _, h := range []string{"h1", "h2"} {
		if err := l.Acquire(ctx, Request{Holder: h, PRDID: "prd-1", Pools: []string{"node:alpha"}}, 0, nil, nil); err != nil {
			t.Fatalf("%s should be admitted under limit 2: %v", h, err)
		}
	}

	done := nqAcquire(l, Request{Holder: "h3", PRDID: "prd-1", Pools: []string{"node:alpha"}})
	waitPending(t, clk, 1)
	if l.Held("h3") {
		t.Fatal("h3 must wait while two holders fill node:alpha")
	}
	if reason := nqWaitingReason(t, l, "h3"); !strings.Contains(reason, "2/2") {
		t.Fatalf("wait reason = %q, want it to show 2/2 in use", reason)
	}

	// A poll tick fires with the pool still full: h3 must keep waiting.
	clk.Advance(nqPoll)
	if l.Held("h3") {
		t.Fatal("h3 was admitted on a poll tick while the pool was still full")
	}

	l.Release("h1")
	nqExpectGranted(t, done, "h3")
	if !l.Held("h2") || !l.Held("h3") {
		t.Fatal("h2 and h3 should hold leases once h1 is released")
	}
}

func TestNodeQueueVerifierBeatsEarlierPlanner(t *testing.T) {
	clk := newFakeClock()
	l := nqLedger(clk, 1)
	ctx := context.Background()
	if err := l.Acquire(ctx, Request{Holder: "running", PRDID: "prd-0", Pools: []string{"node:alpha"}}, 0, nil, nil); err != nil {
		t.Fatal(err)
	}

	// The planner queues first, so FIFO alone would grant it first. Its role
	// must rank it behind the verifier that queues after it.
	planner := nqAcquire(l, Request{Holder: "planner", PRDID: "prd-1", Pools: []string{"node:alpha"}, Role: RolePlanner})
	waitPending(t, clk, 1)
	verifier := nqAcquire(l, Request{Holder: "verifier", PRDID: "prd-2", Pools: []string{"node:alpha"}, Role: RoleVerifier})
	waitPending(t, clk, 2)

	l.Release("running")
	nqExpectGranted(t, verifier, "verifier")
	if l.Held("planner") {
		t.Fatal("planner was granted ahead of the verifier")
	}

	l.Release("verifier")
	nqExpectGranted(t, planner, "planner")
}

func TestNodeQueueReapFreesDeadHolderForNextWaiter(t *testing.T) {
	clk := newFakeClock()
	l := nqLedger(clk, 1)
	ctx := context.Background()
	if err := l.Acquire(ctx, Request{Holder: "dead", PRDID: "prd-1", Pools: []string{"node:alpha"}}, 0, nil, nil); err != nil {
		t.Fatal(err)
	}

	done := nqAcquire(l, Request{Holder: "next", PRDID: "prd-2", Pools: []string{"node:alpha"}})
	waitPending(t, clk, 1)

	// The liveness check reports only "dead" as gone; the lease must be dropped
	// and the waiter granted in the same Reap call.
	released := l.Reap(func(ls Lease) bool { return ls.Holder != "dead" })
	if len(released) != 1 || released[0] != "dead" {
		t.Fatalf("Reap released %v, want [dead]", released)
	}
	if l.Held("dead") {
		t.Fatal("dead still holds a lease after Reap")
	}
	nqExpectGranted(t, done, "next")
	if !l.Held("next") {
		t.Fatal("next has no lease after Reap freed the slot")
	}
}
