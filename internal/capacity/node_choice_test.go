package capacity

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
)

// ncLedger builds a ledger on the fake clock with nodes a and b limited to one
// engine each.
func ncLedger(clk *fakeClock) *Ledger {
	l := New(Options{PollInterval: nqPoll, Clock: clk})
	l.SetLimit("node:a", 1)
	l.SetLimit("node:b", 1)
	return l
}

// ncRequest is a request that may run on node a (priority 1) or node b
// (priority 5), plus the shared host pool.
func ncRequest(holder string) Request {
	return Request{
		Holder: holder, PRDID: "prd-1", Pools: []string{"host"}, Model: "m1",
		NodeChoices: []NodeChoice{{Name: "a", Priority: 1}, {Name: "b", Priority: 5}},
	}
}

func ncAcquire(t *testing.T, l *Ledger, req Request) *Lease {
	t.Helper()
	if err := l.Acquire(context.Background(), req, 0, nil, nil); err != nil {
		t.Fatalf("%s: Acquire: %v", req.Holder, err)
	}
	ls := l.LeaseOf(req.Holder)
	if ls == nil {
		t.Fatalf("%s: no lease after Acquire", req.Holder)
	}
	return ls
}

func hasPool(ls *Lease, pool string) bool { return sharesPool(ls.Pools, []string{pool}) }

// ncHold takes a lease on one node's pool, as a session already running there
// would, so the node carries load.
func ncHold(t *testing.T, l *Ledger, holder, node string) {
	t.Helper()
	ncAcquire(t, l, Request{Holder: holder, PRDID: "prd-0", Pools: []string{"node:" + node}, Node: node})
}

// With no load on either node the tie goes to the higher priority; the
// priority is kept as the tie-break after load.
func TestAcquirePicksHighestPriorityNodeOnLoadTie(t *testing.T) {
	l := ncLedger(newFakeClock())
	ls := ncAcquire(t, l, ncRequest("t1"))
	if ls.Node != "b" {
		t.Fatalf("Node = %q, want b (equal load, higher priority)", ls.Node)
	}
	if !hasPool(ls, "node:b") || hasPool(ls, "node:a") || !hasPool(ls, "host") {
		t.Fatalf("Pools = %v, want host and node:b only", ls.Pools)
	}
}

func TestAcquireLeastLoadedNodeBeatsHigherPriority(t *testing.T) {
	l := New(Options{PollInterval: nqPoll, Clock: newFakeClock()})
	l.SetLimit("node:a", 2)
	l.SetLimit("node:b", 2)
	ncHold(t, l, "h1", "b")
	ls := ncAcquire(t, l, ncRequest("t1"))
	if ls.Node != "a" {
		t.Fatalf("Node = %q, want a (0/2 beats b at 1/2 though b has the higher priority)", ls.Node)
	}
}

func TestAcquireLoadIsUsedOverLimit(t *testing.T) {
	l := New(Options{PollInterval: nqPoll, Clock: newFakeClock()})
	l.SetLimit("node:a", 4)
	l.SetLimit("node:b", 2)
	ncHold(t, l, "h1", "a")
	ncHold(t, l, "h2", "b")
	// a is 1/4 and b is 1/2: a holds as many sessions but is less loaded.
	if ls := ncAcquire(t, l, ncRequest("t1")); ls.Node != "a" {
		t.Fatalf("Node = %q, want a (1/4 beats 1/2)", ls.Node)
	}
	// a is now 2/4 and b 1/2: the same load, so the higher priority b wins.
	if ls := ncAcquire(t, l, ncRequest("t2")); ls.Node != "b" {
		t.Fatalf("Node = %q, want b (2/4 ties 1/2, higher priority)", ls.Node)
	}
}

func TestAcquireUnlimitedNodeCountsAsNoLoad(t *testing.T) {
	l := New(Options{PollInterval: nqPoll, Clock: newFakeClock()})
	l.SetLimit("node:b", 3)
	ncHold(t, l, "h1", "b")
	for _, h := range []string{"u1", "u2", "u3"} {
		ncHold(t, l, h, "a")
	}
	// a has no limit and three sessions, which counts as load 0, so it beats b at 1/3.
	if ls := ncAcquire(t, l, ncRequest("t1")); ls.Node != "a" {
		t.Fatalf("Node = %q, want a (unlimited counts as 0)", ls.Node)
	}
}

func TestAcquireCountsExternalUseAsLoad(t *testing.T) {
	var ext atomic.Int32
	l := New(Options{
		PollInterval: nqPoll, Clock: newFakeClock(),
		External: func(pool string) int {
			if pool == "node:b" {
				return int(ext.Load())
			}
			return 0
		},
	})
	l.SetLimit("node:a", 2)
	l.SetLimit("node:b", 2)
	ext.Store(1)
	if ls := ncAcquire(t, l, ncRequest("t1")); ls.Node != "a" {
		t.Fatalf("Node = %q, want a (b has 1/2 in use outside the ledger)", ls.Node)
	}
}

func TestAcquireFallsToOtherNodeWhenBestIsFull(t *testing.T) {
	clk := newFakeClock()
	l := ncLedger(clk)
	if first := ncAcquire(t, l, ncRequest("t1")); first.Node != "b" {
		t.Fatalf("first Node = %q, want b", first.Node)
	}
	second := ncAcquire(t, l, ncRequest("t2"))
	if second.Node != "a" {
		t.Fatalf("second Node = %q, want a while b is full", second.Node)
	}

	done := nqAcquire(l, ncRequest("t3"))
	waitPending(t, clk, 1)
	if l.Held("t3") {
		t.Fatal("t3 holds a lease while both nodes are full")
	}
	reason := nqWaitingReason(t, l, "t3")
	for _, want := range []string{"nodes b, a", "held by t1", "held by t2"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("wait reason = %q, want it to contain %q", reason, want)
		}
	}

	l.Release("t1")
	nqExpectGranted(t, done, "t3")
	if got := l.LeaseOf("t3"); got == nil || got.Node != "b" {
		t.Fatalf("t3 lease = %+v, want node b after t1 released", got)
	}
}

func TestAcquireSingleNodeUnchanged(t *testing.T) {
	clk := newFakeClock()
	l := ncLedger(clk)
	one := Request{
		Holder: "t1", PRDID: "prd-1", Pools: []string{"node:a"}, Node: "a",
		NodeChoices: []NodeChoice{{Name: "b", Priority: 9}},
	}
	ls := ncAcquire(t, l, one)
	if ls.Node != "a" || !hasPool(ls, "node:a") || hasPool(ls, "node:b") {
		t.Fatalf("lease = %+v, want the caller's node a untouched", ls)
	}

	done := nqAcquire(l, Request{Holder: "t2", PRDID: "prd-1", Pools: []string{"node:a"}, Node: "a"})
	waitPending(t, clk, 1)
	if reason := nqWaitingReason(t, l, "t2"); !strings.Contains(reason, "waiting for node a:") {
		t.Fatalf("wait reason = %q, want the single-node form", reason)
	}
	l.Release("t1")
	nqExpectGranted(t, done, "t2")
}

func TestAcquireSkipsExcludedNode(t *testing.T) {
	l := ncLedger(newFakeClock())
	req := ncRequest("t1")
	req.ExcludeNodes = []string{"b"}
	ls := ncAcquire(t, l, req)
	if ls.Node != "a" || hasPool(ls, "node:b") {
		t.Fatalf("lease = %+v, want node a because b is excluded though it has room", ls)
	}
	if d := l.Check(Request{Holder: "t2", Pools: []string{"host"}, NodeChoices: req.NodeChoices, ExcludeNodes: []string{"a", "b"}}); d.Kind == DecisionAdmit {
		t.Fatalf("Check admitted a request whose every node is excluded: %+v", d)
	}
}

func TestLeaseRecordsChosenNode(t *testing.T) {
	l := ncLedger(newFakeClock())
	ls := ncAcquire(t, l, ncRequest("t1"))
	if ls.Node != "b" {
		t.Fatalf("Node = %q, want b", ls.Node)
	}
	if d := l.Check(ncRequest("probe")); d.Kind != DecisionAdmit || d.Node != "a" {
		t.Fatalf("Check = %+v, want admit on node a while b is held", d)
	}

	// Model affinity follows the chosen node.
	if got := l.lastModel["b"]; got != "m1" {
		t.Fatalf("lastModel[b] = %q, want m1", got)
	}
	if _, ok := l.lastModel["a"]; ok {
		t.Fatal("lastModel recorded for the node that was not chosen")
	}

	// Snapshot and Handoff carry the chosen node with the lease.
	if st := l.Snapshot(); len(st.Leases) != 1 || st.Leases[0].Node != "b" || !hasPool(&st.Leases[0], "node:b") {
		t.Fatalf("snapshot leases = %+v", st.Leases)
	}
	if !l.Handoff("t1", "t2") {
		t.Fatal("Handoff failed")
	}
	if got := l.LeaseOf("t2"); got == nil || got.Node != "b" || !hasPool(got, "node:b") {
		t.Fatalf("handed-off lease = %+v, want node b kept", got)
	}
	for _, ps := range l.Snapshot().Pools {
		if ps.Name == "node:b" && (len(ps.Holders) != 1 || ps.Holders[0] != "t2") {
			t.Fatalf("node:b holders = %v, want [t2]", ps.Holders)
		}
	}

	// Adopt records the node the running session is on.
	l.Adopt(Request{Holder: "t3", PRDID: "prd-1", Pools: []string{"host"}, Node: "a", NodeChoices: ncRequest("t3").NodeChoices}, "sess-3")
	if got := l.LeaseOf("t3"); got == nil || got.Node != "a" || !hasPool(got, "node:a") || got.SessionID != "sess-3" {
		t.Fatalf("adopted lease = %+v, want node a and session sess-3", got)
	}
}

// With equal load and equal priority the node listed first wins, in either
// list order.
func TestAcquireEqualLoadEqualPriorityTakesListOrder(t *testing.T) {
	for _, order := range [][]string{{"a", "b"}, {"b", "a"}} {
		l := ncLedger(newFakeClock())
		req := ncRequest("t1")
		req.NodeChoices = []NodeChoice{{Name: order[0], Priority: 3}, {Name: order[1], Priority: 3}}
		if ls := ncAcquire(t, l, req); ls.Node != order[0] {
			t.Fatalf("order %v: Node = %q, want %q", order, ls.Node, order[0])
		}
	}
}
