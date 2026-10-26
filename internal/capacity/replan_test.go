package capacity

import (
	"strings"
	"sync"
	"testing"
)

// rpList stands in for an LLM entry's compute_nodes list. Its refresh builds a
// holder's placement the way the resolver does: one listed node is a bound
// request, two or more are choices for the ledger to pick from.
type rpList struct {
	mu    sync.Mutex
	nodes []string
}

func (r *rpList) set(nodes ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodes = nodes
}

func (r *rpList) refresh(holder string) func() Request {
	return func() Request {
		r.mu.Lock()
		defer r.mu.Unlock()
		req := Request{Holder: holder, PRDID: "prd-1", Model: "m1", Pools: []string{"host"}}
		if len(r.nodes) == 1 {
			req.Pools = append(req.Pools, "node:"+r.nodes[0])
			req.Node = r.nodes[0]
			return req
		}
		for _, n := range r.nodes {
			req.NodeChoices = append(req.NodeChoices, NodeChoice{Name: n, Priority: 1})
		}
		return req
	}
}

// rpQueue queues holder on a ledger whose Refresh follows list and waits for
// its first poll timer.
func rpQueue(t *testing.T, l *Ledger, clk *fakeClock, list *rpList, holder string, pending int) <-chan error {
	t.Helper()
	req := list.refresh(holder)()
	req.Refresh = list.refresh(holder)
	done := nqAcquire(l, req)
	waitPending(t, clk, pending)
	return done
}

func rpLedger(clk *fakeClock, nodes ...string) *Ledger {
	l := New(Options{PollInterval: nqPoll, Clock: clk})
	for _, n := range nodes {
		l.SetLimit("node:"+n, 1)
	}
	return l
}

// Waiters queued on the only listed node spread over the nodes a longer list
// adds, as soon as Replan runs and without a poll tick.
func TestReplanSpreadsWaitersOverNodesTheListGains(t *testing.T) {
	clk := newFakeClock()
	l := rpLedger(clk, "alpha", "beta", "gamma")
	ncHold(t, l, "running", "alpha")

	list := &rpList{}
	list.set("alpha")
	done1 := rpQueue(t, l, clk, list, "w1", 1)
	done2 := rpQueue(t, l, clk, list, "w2", 2)
	nrWaitReason(t, l, "w1", "node alpha")
	nrWaitReason(t, l, "w2", "node alpha")

	list.set("alpha", "beta", "gamma")
	l.Replan()
	nqExpectGranted(t, done1, "w1")
	nqExpectGranted(t, done2, "w2")

	on := map[string]int{}
	for _, h := range []string{"w1", "w2"} {
		on[l.LeaseOf(h).Node]++
	}
	if on["beta"] != 1 || on["gamma"] != 1 || on["alpha"] != 0 {
		t.Fatalf("waiters landed on %v, want one on beta and one on gamma", on)
	}
}

// Without Replan the waiters stay queued until a poll tick, so Replan is what
// makes the change immediate.
func TestWaitersStayQueuedUntilReplanOrPoll(t *testing.T) {
	clk := newFakeClock()
	l := rpLedger(clk, "alpha", "beta")
	ncHold(t, l, "running", "alpha")

	list := &rpList{}
	list.set("alpha")
	done := rpQueue(t, l, clk, list, "w1", 1)

	list.set("alpha", "beta")
	if l.Held("w1") {
		t.Fatal("w1 was granted before any re-plan or poll tick")
	}
	l.Replan()
	nqExpectGranted(t, done, "w1")
	if ls := l.LeaseOf("w1"); ls == nil || ls.Node != "beta" {
		t.Fatalf("lease = %+v, want it on beta", ls)
	}
}

// Waiters whose node leaves the list move to a listed node, and their wait
// reason stops naming the removed node.
func TestReplanMovesWaitersOffNodeTheListLoses(t *testing.T) {
	clk := newFakeClock()
	l := rpLedger(clk, "alpha", "beta", "gamma")
	ncHold(t, l, "run-alpha", "alpha")
	ncHold(t, l, "run-beta", "beta")

	list := &rpList{}
	list.set("alpha", "beta")
	done := rpQueue(t, l, clk, list, "w1", 1)
	reason := nrWaitReason(t, l, "w1", "alpha")
	if !strings.Contains(reason, "beta") {
		t.Fatalf("wait reason = %q, want it to name both listed nodes", reason)
	}

	list.set("beta", "gamma")
	l.Replan()
	nqExpectGranted(t, done, "w1")
	if ls := l.LeaseOf("w1"); ls == nil || ls.Node != "gamma" || hasPool(ls, "node:alpha") {
		t.Fatalf("lease = %+v, want it on gamma and off alpha", ls)
	}
}

// A waiter whose node leaves the list while every listed node is busy keeps
// waiting, and its reason names only the listed nodes.
func TestReplanReasonNamesOnlyListedNodes(t *testing.T) {
	clk := newFakeClock()
	l := rpLedger(clk, "alpha", "beta", "gamma")
	ncHold(t, l, "run-alpha", "alpha")
	ncHold(t, l, "run-beta", "beta")
	ncHold(t, l, "run-gamma", "gamma")

	list := &rpList{}
	list.set("alpha", "beta")
	done := rpQueue(t, l, clk, list, "w1", 1)
	nrWaitReason(t, l, "w1", "alpha")

	list.set("beta", "gamma")
	l.Replan()
	reason := nrWaitReason(t, l, "w1", "gamma")
	if strings.Contains(reason, "alpha") {
		t.Fatalf("wait reason = %q, want no mention of the removed node", reason)
	}

	l.Release("run-gamma")
	nqExpectGranted(t, done, "w1")
	if ls := l.LeaseOf("w1"); ls == nil || ls.Node != "gamma" {
		t.Fatalf("lease = %+v, want it on gamma", ls)
	}
}

// A waiter on a node that stays listed is not moved by Replan while that node
// is the freest one.
func TestReplanKeepsWaiterOnStillListedNode(t *testing.T) {
	clk := newFakeClock()
	l := rpLedger(clk, "alpha", "beta", "gamma")
	ncHold(t, l, "run-alpha", "alpha")
	ncHold(t, l, "run-beta", "beta")

	list := &rpList{}
	list.set("alpha", "beta")
	done := rpQueue(t, l, clk, list, "w1", 1)
	nrWaitReason(t, l, "w1", "alpha")

	list.set("alpha", "beta") // an unrelated edit: same candidates
	l.Replan()
	if l.Held("w1") {
		t.Fatal("w1 was granted while both listed nodes are full")
	}
	l.Release("run-alpha")
	nqExpectGranted(t, done, "w1")
	if ls := l.LeaseOf("w1"); ls == nil || ls.Node != "alpha" {
		t.Fatalf("lease = %+v, want it on alpha, the node that freed", ls)
	}
}

// Replan on an idle ledger is a no-op.
func TestReplanWithNoWaitersChangesNothing(t *testing.T) {
	l := rpLedger(newFakeClock(), "alpha")
	ncHold(t, l, "running", "alpha")
	l.Replan()
	if st := l.Snapshot(); len(st.Leases) != 1 || len(st.Waiting) != 0 {
		t.Fatalf("snapshot = %+v, want one lease and no waiters", st)
	}
}
