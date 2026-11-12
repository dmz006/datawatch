package capacity

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// nrPlacement is the placement a test-controlled Refresh returns. Swapping it
// stands in for the compute_nodes list changing while a waiter is queued.
type nrPlacement struct{ p atomic.Pointer[Request] }

func (n *nrPlacement) set(r Request) { n.p.Store(&r) }

// refresh is the Request.Refresh closure: it returns the current placement.
func (n *nrPlacement) refresh() Request { return *n.p.Load() }

// nrBound is the single-node shape a caller produces today: the node chosen up
// front, its pool already in Pools.
func nrBound(holder, node string) Request {
	return Request{
		Holder: holder, PRDID: "prd-1", Model: "m1",
		Pools: []string{"host", "node:" + node}, Node: node,
	}
}

// nrChoices is the same request with both nodes offered, a at priority 1 and
// b at priority 5.
func nrChoices(holder string) Request {
	r := ncRequest(holder)
	r.Pools = []string{"host"}
	return r
}

// nrWaiter starts an Acquire whose Refresh follows pl and waits until it is
// queued with its first poll timer registered.
func nrWaiter(t *testing.T, l *Ledger, clk *fakeClock, pl *nrPlacement, pending int) <-chan error {
	t.Helper()
	req := pl.refresh()
	req.Refresh = pl.refresh
	done := nqAcquire(l, req)
	waitPending(t, clk, pending)
	return done
}

// nrWaitReason polls until holder's wait reason contains want. A poll tick is
// handled by the waiter goroutine, so the reason changes a moment after Advance.
func nrWaitReason(t *testing.T, l *Ledger, holder, want string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		reason := ""
		for _, w := range l.Snapshot().Waiting {
			if w.Holder == holder {
				reason = w.Reason
			}
		}
		if strings.Contains(reason, want) {
			return reason
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: wait reason = %q, want it to contain %q", holder, reason, want)
		}
		time.Sleep(time.Millisecond)
	}
}

// nrOnNode counts the listed holders whose lease sits on node.
func nrOnNode(l *Ledger, node string, holders ...string) int {
	n := 0
	for _, h := range holders {
		if ls := l.LeaseOf(h); ls != nil && ls.Node == node {
			n++
		}
	}
	return n
}

// A waiter queued on node a with the bound shape moves to the free node b once
// its Refresh offers both, and the grant lands on b alone.
func TestWaiterReplaceMovesToFreeNode(t *testing.T) {
	clk := newFakeClock()
	l := ncLedger(clk)
	ncHold(t, l, "holder-a", "a")

	pl := &nrPlacement{}
	pl.set(nrBound("w", "a"))
	done := nrWaiter(t, l, clk, pl, 1)
	nrWaitReason(t, l, "w", "node a")

	pl.set(nrChoices("w"))
	clk.Advance(nqPoll)
	nqExpectGranted(t, done, "w")

	ls := l.LeaseOf("w")
	if ls == nil || ls.Node != "b" {
		t.Fatalf("lease = %+v, want it on node b", ls)
	}
	if !hasPool(ls, "node:b") || hasPool(ls, "node:a") {
		t.Fatalf("Pools = %v, want node:b and not node:a", ls.Pools)
	}
	used := map[string]int{}
	for _, p := range l.Snapshot().Pools {
		used[p.Name] = p.Held
	}
	if used["node:b"] != 1 || used["node:a"] != 1 {
		t.Fatalf("used = %v, want node:a 1 (holder-a) and node:b 1 (w)", used)
	}
}

// With both nodes at their limit the waiter does not move: its reason names
// both nodes, and it is granted on whichever node frees.
func TestWaiterReplaceStaysWhenBothBusy(t *testing.T) {
	clk := newFakeClock()
	l := ncLedger(clk)
	ncHold(t, l, "holder-a", "a")
	ncHold(t, l, "holder-b", "b")

	pl := &nrPlacement{}
	pl.set(nrBound("w", "a"))
	done := nrWaiter(t, l, clk, pl, 1)

	pl.set(nrChoices("w"))
	clk.Advance(nqPoll)
	reason := nrWaitReason(t, l, "w", "nodes")
	for _, want := range []string{"b, a", "held by holder-a", "held by holder-b"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("wait reason = %q, want it to contain %q", reason, want)
		}
	}
	if l.Held("w") {
		t.Fatal("w holds a lease while both nodes are full")
	}

	l.Release("holder-a")
	nqExpectGranted(t, done, "w")
	if ls := l.LeaseOf("w"); ls == nil || ls.Node != "a" {
		t.Fatalf("lease = %+v, want it on node a, the node that freed", ls)
	}
}

// When a waiter re-places and both nodes fit, the less loaded node wins; on a
// load tie the higher NodeChoice priority wins. The shared host pool keeps the
// waiter queued until both cases are set up.
func TestWaiterReplaceOrdersByLoadThenPriority(t *testing.T) {
	cases := []struct {
		name     string
		loadOnB  bool
		wantNode string
	}{
		{"less loaded node beats higher priority", true, "a"},
		{"load tie goes to higher priority", false, "b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clk := newFakeClock()
			l := New(Options{PollInterval: nqPoll, Clock: clk})
			l.SetLimit("host", 1)
			l.SetLimit("node:a", 2)
			l.SetLimit("node:b", 2)
			ncAcquire(t, l, Request{Holder: "host-holder", PRDID: "prd-0", Pools: []string{"host"}})
			if tc.loadOnB {
				ncHold(t, l, "load-b", "b")
			}

			pl := &nrPlacement{}
			pl.set(nrBound("w", "a"))
			done := nrWaiter(t, l, clk, pl, 1)
			pl.set(nrChoices("w"))
			clk.Advance(nqPoll)
			nrWaitReason(t, l, "w", "host")

			l.Release("host-holder")
			nqExpectGranted(t, done, "w")
			if ls := l.LeaseOf("w"); ls == nil || ls.Node != tc.wantNode {
				t.Fatalf("lease = %+v, want node %s", ls, tc.wantNode)
			}
		})
	}
}

// After a waiter moves to b, a second waiter for b queues behind it, and no
// node ever carries more leases than its limit.
func TestWaiterReplaceKeepsOneEnginePerNode(t *testing.T) {
	clk := newFakeClock()
	l := ncLedger(clk)
	ncHold(t, l, "holder-a", "a")

	pl := &nrPlacement{}
	pl.set(nrBound("w1", "a"))
	done1 := nrWaiter(t, l, clk, pl, 1)
	pl.set(nrChoices("w1"))
	clk.Advance(nqPoll)
	nqExpectGranted(t, done1, "w1")

	// w2 is bound to b, which w1 now fills.
	done2 := nqAcquire(l, nrBound("w2", "b"))
	waitPending(t, clk, 1)
	nrWaitReason(t, l, "w2", "node b")
	if l.Held("w2") {
		t.Fatal("w2 holds a lease while node b is full")
	}
	all := []string{"holder-a", "w1", "w2"}
	if n := nrOnNode(l, "b", all...); n != 1 {
		t.Fatalf("leases on b = %d, want 1", n)
	}
	if n := nrOnNode(l, "a", all...); n != 1 {
		t.Fatalf("leases on a = %d, want 1", n)
	}

	l.Release("w1")
	nqExpectGranted(t, done2, "w2")
	if n := nrOnNode(l, "b", all...); n != 1 {
		t.Fatalf("leases on b after hand-over = %d, want 1", n)
	}
	if ls := l.LeaseOf("w2"); ls == nil || ls.Node != "b" {
		t.Fatalf("lease = %+v, want w2 on node b", ls)
	}
}

// A waiter with no Refresh keeps the node it queued on even when another node
// is free; this is the unchanged behaviour.
func TestWaiterReplaceNilRefreshDoesNotMove(t *testing.T) {
	clk := newFakeClock()
	l := ncLedger(clk)
	ncHold(t, l, "holder-a", "a")

	done := nqAcquire(l, nrBound("w", "a"))
	waitPending(t, clk, 1)
	clk.Advance(nqPoll)
	reason := nrWaitReason(t, l, "w", "node a")
	if !strings.Contains(reason, "held by holder-a") {
		t.Fatalf("wait reason = %q, want it to name holder-a", reason)
	}
	if l.Held("w") {
		t.Fatal("w moved to the free node b without a Refresh")
	}

	l.Release("holder-a")
	nqExpectGranted(t, done, "w")
	if ls := l.LeaseOf("w"); ls == nil || ls.Node != "a" {
		t.Fatalf("lease = %+v, want it on node a", ls)
	}
}
