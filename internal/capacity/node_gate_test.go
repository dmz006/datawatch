package capacity

import (
	"strings"
	"sync"
	"testing"
)

// fakeNodeGate is a mutable NodeGate table. A drain closes a node for every
// consumer, including an empty one; a consumer denial closes a node for one
// consumer kind only. Both return the set reason verbatim.
type fakeNodeGate struct {
	mu    sync.Mutex
	drain map[string]string // node → verbatim reason
	deny  map[string]string // node + "/" + consumer → verbatim reason
}

func (g *fakeNodeGate) gate(node, consumer string) (bool, string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if why, ok := g.drain[node]; ok {
		return false, why
	}
	if consumer == "" {
		return true, "" // an empty consumer skips the consumer check
	}
	if why, ok := g.deny[node+"/"+consumer]; ok {
		return false, why
	}
	return true, ""
}

func (g *fakeNodeGate) drainNode(node, reason string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.drain == nil {
		g.drain = map[string]string{}
	}
	g.drain[node] = reason
}

func (g *fakeNodeGate) denyConsumer(node, consumer, reason string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.deny == nil {
		g.deny = map[string]string{}
	}
	g.deny[node+"/"+consumer] = reason
}

// ngLedger is ncLedger with the gate hook wired.
func ngLedger(clk *fakeClock, g *fakeNodeGate) *Ledger {
	l := New(Options{PollInterval: nqPoll, Clock: clk, NodeGate: g.gate})
	l.SetLimit("node:a", 1)
	l.SetLimit("node:b", 1)
	return l
}

// ngRequest is ncRequest with a Consumer kind.
func ngRequest(holder, consumer string) Request {
	r := ncRequest(holder)
	r.Consumer = consumer
	return r
}

func TestNodeGateSkipsDeniedConsumerForMultiNode(t *testing.T) {
	g := &fakeNodeGate{}
	g.denyConsumer("b", "session_spawn", "consumer session_spawn denied")
	l := ngLedger(newFakeClock(), g)
	// b is denied for this consumer kind, so a is granted although b is idle
	// and carries the higher priority: the skip is the gate's, not load's.
	ls := ncAcquire(t, l, ngRequest("t1", "session_spawn"))
	if ls.Node != "a" || !hasPool(ls, "node:a") {
		t.Fatalf("lease = %+v, want node a because b rejects the consumer kind", ls)
	}
}

func TestNodeGateSkipsDrainedCandidateAndGrantsOther(t *testing.T) {
	clk := newFakeClock()
	g := &fakeNodeGate{}
	g.drainNode("b", "drained")
	l := ngLedger(clk, g)
	// b is drained: a is granted although b is idle and carries the higher
	// priority (5 to 1), so the skip is the gate's doing, not load's.
	ls := ncAcquire(t, l, ngRequest("t1", "session_spawn"))
	if ls.Node != "a" || !hasPool(ls, "node:a") {
		t.Fatalf("lease = %+v, want node a because b is drained", ls)
	}

	// A second request must queue and name the drained candidate with its
	// gate reason.
	done := nqAcquire(l, ngRequest("t2", "session_spawn"))
	waitPending(t, clk, 1)
	reason := nqWaitingReason(t, l, "t2")
	for _, want := range []string{"a: ", "held by t1", "b: drained"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("wait reason = %q, want it to contain %q", reason, want)
		}
	}
	_ = done
}

func TestNodeGateEmptyConsumerIsNotConsumerChecked(t *testing.T) {
	clk := newFakeClock()
	g := &fakeNodeGate{}
	// Every node rejects the session_spawn consumer kind.
	g.denyConsumer("a", "session_spawn", "consumer session_spawn denied")
	g.denyConsumer("b", "session_spawn", "consumer session_spawn denied")
	l := ngLedger(clk, g)

	// An empty Consumer skips the consumer check, so the request is granted.
	ls := ncAcquire(t, l, ngRequest("t1", ""))
	if ls.Node != "a" && ls.Node != "b" {
		t.Fatalf("lease = %+v, want a grant: an empty consumer is not consumer checked", ls)
	}

	// The same request with a consumer kind is refused by both candidates and
	// names every denial in the wait reason.
	l.Release(ls.Holder)
	done := nqAcquire(l, ngRequest("t2", "session_spawn"))
	waitPending(t, clk, 1)
	if l.Held("t2") {
		t.Fatal("t2 holds a lease although both nodes reject its consumer kind")
	}
	reason := nqWaitingReason(t, l, "t2")
	for _, want := range []string{
		"a: consumer session_spawn denied",
		"b: consumer session_spawn denied",
	} {
		if !strings.Contains(reason, want) {
			t.Fatalf("wait reason = %q, want it to contain %q", reason, want)
		}
	}
	_ = done
}

func TestNodeGateBlocksSingleNodeByDrain(t *testing.T) {
	clk := newFakeClock()
	g := &fakeNodeGate{}
	g.drainNode("a", "drained")
	l := ngLedger(clk, g)

	req := Request{Holder: "t1", PRDID: "prd-1", Pools: []string{"node:a"}, Node: "a",
		Consumer: "session_spawn"}
	done := nqAcquire(l, req)
	waitPending(t, clk, 1)
	if l.Held("t1") {
		t.Fatal("t1 holds a lease although its node is drained")
	}
	reason := nqWaitingReason(t, l, "t1")
	if reason != "waiting for node a: drained" {
		t.Fatalf("wait reason = %q, want the gate reason verbatim after the node name", reason)
	}
	_ = done
}

func TestNodeGateReturnsConsumerReasonVerbatim(t *testing.T) {
	clk := newFakeClock()
	g := &fakeNodeGate{}
	g.denyConsumer("a", "council", "consumer council denied: policy 42")
	l := ngLedger(clk, g)

	req := Request{Holder: "t1", PRDID: "prd-1", Pools: []string{"node:a"}, Node: "a",
		Consumer: "council"}
	done := nqAcquire(l, req)
	waitPending(t, clk, 1)
	reason := nqWaitingReason(t, l, "t1")
	want := "waiting for node a: consumer council denied: policy 42"
	if reason != want {
		t.Fatalf("wait reason = %q, want %q", reason, want)
	}
	_ = done
}

func TestNodeGateDrainKeepsRunningLeases(t *testing.T) {
	clk := newFakeClock()
	g := &fakeNodeGate{}
	l := ngLedger(clk, g)

	// A holder running on node b when it is drained.
	ls := ncAcquire(t, l, Request{Holder: "t1", PRDID: "prd-0", Pools: []string{"node:b"}, Node: "b"})
	if !hasPool(ls, "node:b") {
		t.Fatalf("lease = %+v, want it held in the node b pool", ls)
	}

	// The drain must not touch the lease: it stays held and counts in Held.
	g.drainNode("b", "drained")
	if !l.Held("t1") {
		t.Fatal("drain released the running holder")
	}
	cur := l.LeaseOf("t1")
	if cur == nil || cur.Node != "b" || !hasPool(cur, "node:b") {
		t.Fatalf("lease after drain = %+v, want unchanged in the node b pool on b", cur)
	}
	st := l.Snapshot()
	for _, p := range st.Pools {
		if p.Name == "node:b" {
			if p.Held != 1 {
				t.Fatalf("node:b Held = %d after the drain, want the running holder still counted", p.Held)
			}
		}
	}

	// New work for the drained node is still refused.
	req := Request{Holder: "t2", PRDID: "prd-0", Pools: []string{"node:b"}, Node: "b"}
	done := nqAcquire(l, req)
	waitPending(t, clk, 1)
	reason := nqWaitingReason(t, l, "t2")
	want := "waiting for node b: drained"
	if !strings.Contains(reason, want) {
		t.Fatalf("wait reason = %q, want it to contain %q", reason, want)
	}
	_ = done
}
