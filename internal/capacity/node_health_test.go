package capacity

import (
	"strings"
	"sync"
	"testing"
)

// nodeHealth is a mutable health table for the NodeHealthy hook.
type nodeHealth struct {
	mu  sync.Mutex
	bad map[string]bool
}

func (h *nodeHealth) set(name string, unhealthy bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.bad == nil {
		h.bad = map[string]bool{}
	}
	h.bad[name] = unhealthy
}

func (h *nodeHealth) healthy(name string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return !h.bad[name]
}

// nhLedger is ncLedger with the health hook wired.
func nhLedger(clk *fakeClock, h *nodeHealth) *Ledger {
	l := New(Options{PollInterval: nqPoll, Clock: clk, NodeHealthy: h.healthy})
	l.SetLimit("node:a", 1)
	l.SetLimit("node:b", 1)
	return l
}

func TestAcquireSkipsUnhealthyNode(t *testing.T) {
	h := &nodeHealth{}
	h.set("b", true)
	l := nhLedger(newFakeClock(), h)
	ls := ncAcquire(t, l, ncRequest("t1"))
	if ls.Node != "a" || hasPool(ls, "node:b") {
		t.Fatalf("lease = %+v, want node a because b is unhealthy though it is idle and higher priority", ls)
	}
	if d := l.Check(ncRequest("probe")); d.Kind == DecisionAdmit {
		t.Fatalf("Check admitted with a full and b unhealthy: %+v", d)
	}
}

func TestUnhealthyNodeIsNamedInWaitReason(t *testing.T) {
	clk := newFakeClock()
	h := &nodeHealth{}
	l := nhLedger(clk, h)
	first := ncAcquire(t, l, ncRequest("t1"))
	if first.Node != "b" {
		t.Fatalf("first Node = %q, want b", first.Node)
	}
	h.set("a", true)

	done := nqAcquire(l, ncRequest("t2"))
	waitPending(t, clk, 1)
	reason := nqWaitingReason(t, l, "t2")
	for _, want := range []string{"a: unhealthy", "b: ", "held by t1"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("wait reason = %q, want it to contain %q", reason, want)
		}
	}

	// The node recovers: the next poll places the waiter on it.
	h.set("a", false)
	clk.Advance(nqPoll)
	nqExpectGranted(t, done, "t2")
	if got := l.LeaseOf("t2"); got == nil || got.Node != "a" {
		t.Fatalf("t2 lease = %+v, want node a once it is healthy", got)
	}
}

func TestEveryCandidateUnhealthyOrFullIsNamed(t *testing.T) {
	clk := newFakeClock()
	h := &nodeHealth{}
	l := New(Options{PollInterval: nqPoll, Clock: clk, NodeHealthy: h.healthy})
	l.SetLimit("node:a", 1)
	l.SetLimit("node:b", 1)
	l.SetLimit("node:c", 1)
	ncHold(t, l, "h1", "c")
	h.set("a", true)
	h.set("b", true)

	req := ncRequest("t1")
	req.NodeChoices = append(req.NodeChoices, NodeChoice{Name: "c", Priority: 3})
	done := nqAcquire(l, req)
	waitPending(t, clk, 1)
	if l.Held("t1") {
		t.Fatal("t1 holds a lease while every node is unhealthy or full")
	}
	reason := nqWaitingReason(t, l, "t1")
	for _, want := range []string{"a: unhealthy", "b: unhealthy", "c: ", "held by h1"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("wait reason = %q, want it to contain %q", reason, want)
		}
	}

	l.Release("h1")
	nqExpectGranted(t, done, "t1")
	if got := l.LeaseOf("t1"); got == nil || got.Node != "c" {
		t.Fatalf("t1 lease = %+v, want node c, the only healthy one", got)
	}
}

func TestAllCandidatesUnhealthyNamesEach(t *testing.T) {
	clk := newFakeClock()
	h := &nodeHealth{}
	h.set("a", true)
	h.set("b", true)
	l := nhLedger(clk, h)
	_ = nqAcquire(l, ncRequest("t1"))
	waitPending(t, clk, 1)
	reason := nqWaitingReason(t, l, "t1")
	for _, want := range []string{"a: unhealthy", "b: unhealthy"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("wait reason = %q, want it to contain %q", reason, want)
		}
	}
}

func TestSingleNodeRequestIgnoresHealthHook(t *testing.T) {
	h := &nodeHealth{}
	h.set("a", true)
	l := nhLedger(newFakeClock(), h)
	ls := ncAcquire(t, l, Request{Holder: "t1", PRDID: "prd-1", Pools: []string{"node:a"}, Node: "a"})
	if ls.Node != "a" {
		t.Fatalf("Node = %q, want the caller's node a admitted as before", ls.Node)
	}
}
