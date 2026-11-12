package capacity

import (
	"strings"
	"testing"
)

// preferRequest is ncRequest (a at priority 1, b at priority 5) with a
// preferred node.
func preferRequest(holder, prefer string) Request {
	r := ncRequest(holder)
	r.PreferNode = prefer
	return r
}

// With both nodes idle b outranks a on Priority, yet the preferred node a wins
// the tie.
func TestPreferNode_TieGoesToPreferred(t *testing.T) {
	l := ncLedger(newFakeClock())
	ls := ncAcquire(t, l, preferRequest("t1", "a"))
	if ls.Node != "a" {
		t.Fatalf("Node = %q, want a (equal load, preferred beats higher priority)", ls.Node)
	}
	if !hasPool(ls, "node:a") || hasPool(ls, "node:b") {
		t.Fatalf("Pools = %v, want node:a only", ls.Pools)
	}
}

// Load comes first: a preferred node that is full loses to a free one.
func TestPreferNode_BusyPreferredLosesToFreeNode(t *testing.T) {
	l := ncLedger(newFakeClock())
	ncHold(t, l, "h1", "a")
	ls := ncAcquire(t, l, preferRequest("t1", "a"))
	if ls.Node != "b" {
		t.Fatalf("Node = %q, want b (a is at 1/1)", ls.Node)
	}
}

// A preference for a node outside NodeChoices changes nothing and adds no pool.
func TestPreferNode_NotACandidateIsIgnored(t *testing.T) {
	l := ncLedger(newFakeClock())
	l.SetLimit("node:z", 1)
	ls := ncAcquire(t, l, preferRequest("t1", "z"))
	if ls.Node != "b" {
		t.Fatalf("Node = %q, want b (normal ranking)", ls.Node)
	}
	if hasPool(ls, "node:z") {
		t.Fatalf("Pools = %v, must not carry node:z", ls.Pools)
	}
}

// With both nodes full the request waits naming both; the node that frees is
// taken even though the other was preferred.
func TestPreferNode_AllFullWaitsAndTakesFreedNode(t *testing.T) {
	clk := newFakeClock()
	l := ncLedger(clk)
	ncHold(t, l, "ha", "a")
	ncHold(t, l, "hb", "b")

	done := nqAcquire(l, preferRequest("t1", "a"))
	waitPending(t, clk, 1)
	reason := nqWaitingReason(t, l, "t1")
	if !strings.Contains(reason, "a") || !strings.Contains(reason, "b") || !strings.Contains(reason, "nodes") {
		t.Fatalf("wait reason = %q, want it to name both nodes", reason)
	}
	if l.Held("t1") {
		t.Fatal("t1 holds a lease while both nodes are full")
	}

	l.Release("hb")
	nqExpectGranted(t, done, "t1")
	if ls := l.LeaseOf("t1"); ls == nil || ls.Node != "b" {
		t.Fatalf("lease = %+v, want it on b", ls)
	}
}

// PreferNode set on the queued request survives Refresh, which replaces only
// the placement fields. The waiter is held back by the shared host pool while
// both nodes sit idle, so the grant is a pure tie.
func TestPreferNode_SurvivesRefresh(t *testing.T) {
	clk := newFakeClock()
	l := ncLedger(clk)
	l.SetLimit("host", 1)
	ncAcquire(t, l, Request{Holder: "hh", PRDID: "prd-0", Pools: []string{"host"}})

	pl := &nrPlacement{}
	first := preferRequest("t1", "a")
	pl.set(first)
	req := first
	req.Refresh = pl.refresh
	done := nqAcquire(l, req)
	waitPending(t, clk, 1)

	// Refresh now returns fresh choices (and no PreferNode of its own).
	pl.set(nrChoices("t1"))
	l.Replan()
	if l.Held("t1") {
		t.Fatal("t1 was granted while the host pool is full")
	}

	l.Release("hh")
	nqExpectGranted(t, done, "t1")
	if ls := l.LeaseOf("t1"); ls == nil || ls.Node != "a" {
		t.Fatalf("lease = %+v, want it on a (preference kept across Refresh)", ls)
	}
}
