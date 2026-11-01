package capacity

import (
	"strings"
	"testing"
	"time"
)

// The switches and edges of the node over-admission rules: the overflow lease
// joins its node pool once the pool has room, each rule can be turned off, and
// the restart gate ends on its timeout.

func ooTrue() bool  { return true }
func ooFalse() bool { return false }

// An adopted lease held outside a full node keeps the node full for new work,
// and joins the node pool the moment the pool has room, so it then reads as held.
func TestNodeOverAdmit_AdoptedOverflowJoinsNodePoolWhenRoomAppears(t *testing.T) {
	l := ooLedger(newFakeClock())
	ncAcquire(t, l, ooWorker("worker-1", ooNodeA))
	l.Adopt(ooResumed("worker-2", ooNodeA), "sess-2")

	ls := l.LeaseOf("worker-2")
	if ls == nil || ls.Node != ooNodeA || !ls.NodeOverflow || hasPool(ls, "node:"+ooNodeA) {
		t.Fatalf("worker-2 lease = %+v, want it on %s, flagged overflow and outside the node pool", ls, ooNodeA)
	}
	for _, p := range l.Snapshot().Pools {
		if p.Name == "node:"+ooNodeA && p.Overflow != 1 {
			t.Errorf("node:%s overflow = %d, want 1", ooNodeA, p.Overflow)
		}
	}
	// Both sessions occupy datawatch, so a new multi-node request goes to the other node.
	if d := l.Check(ooVerifier("verifier-1")); d.Kind != DecisionAdmit || d.Node != ooNodeB {
		t.Fatalf("Check = %+v, want admit on %s", d, ooNodeB)
	}

	l.Release("worker-1")
	ls = l.LeaseOf("worker-2")
	if ls == nil || ls.NodeOverflow || !hasPool(ls, "node:"+ooNodeA) {
		t.Fatalf("worker-2 lease = %+v, want it moved into node:%s once the pool had room", ls, ooNodeA)
	}
	if got := ooPoolHeld(t, l, "node:"+ooNodeA); got != 1 {
		t.Errorf("node:%s held = %d, want 1", ooNodeA, got)
	}
	// The node is still full: a request pinned to it waits.
	if l.WouldGrant(ooWorker("worker-3", ooNodeA)) {
		t.Error("a worker pinned to the node was admittable while the adopted session ran on it")
	}
	ooAssertNoNodeOver(t, l, "after the first worker released")
}

// An adopted lease that cannot join a node whose engine is used by outside work
// is held as overflow, and a request pinned to the node still waits.
func TestNodeOverAdmit_OverflowAloneKeepsNodeFull(t *testing.T) {
	// A node with no room for the adopted session: the external count fills it.
	l2 := New(Options{PollInterval: nqPoll, Clock: newFakeClock(), External: func(pool string) int {
		if pool == "node:"+ooNodeA {
			return 1
		}
		return 0
	}})
	l2.SetLimit("node:"+ooNodeA, 1)
	l2.Adopt(ooResumed("worker-1", ooNodeA), "sess-1")
	if ls := l2.LeaseOf("worker-1"); ls == nil || !ls.NodeOverflow {
		t.Fatalf("lease = %+v, want overflow: external work fills the node", ls)
	}
	if l2.WouldGrant(ooWorker("worker-2", ooNodeA)) {
		t.Error("a pinned worker was admittable on a node whose engine is in use")
	}
}

// StrictNodeLimit off restores the old behaviour: the adopted lease joins the
// node pool whatever its use.
func TestNodeOverAdmit_StrictNodeLimitOffAdoptJoinsFullNode(t *testing.T) {
	l := New(Options{PollInterval: nqPoll, Clock: newFakeClock(), StrictNodeLimit: ooFalse})
	l.SetLimit("node:"+ooNodeA, 1)
	ncAcquire(t, l, ooWorker("worker-1", ooNodeA))
	l.Adopt(ooResumed("worker-2", ooNodeA), "sess-2")
	if ls := l.LeaseOf("worker-2"); ls == nil || ls.NodeOverflow || !hasPool(ls, "node:"+ooNodeA) {
		t.Fatalf("lease = %+v, want it in the node pool with the rule off", ls)
	}
	if got := ooPoolHeld(t, l, "node:"+ooNodeA); got != 2 {
		t.Errorf("node held = %d, want 2 with the rule off", got)
	}
}

// CountPoolByNode off counts every lease once against the llm pool again.
func TestNodeOverAdmit_CountPoolByNodeOffCountsEveryLease(t *testing.T) {
	l := New(Options{PollInterval: nqPoll, Clock: newFakeClock(), CountPoolByNode: ooFalse})
	l.SetLimit("node:"+ooNodeA, 1)
	l.SetLimit("node:"+ooNodeB, 1)
	l.SetLimit(ooLLM, 2)
	l.Adopt(ooResumed("worker-1", ooNodeA), "sess-1")
	l.Adopt(ooResumed("worker-2", ooNodeA), "sess-2")
	if d := l.Check(ooVerifier("verifier-1")); d.Kind == DecisionAdmit {
		t.Fatalf("Check = %+v, want the llm pool full at 2/2 with the rule off", d)
	}
}

// A pool counted by node never counts a node past its own limit, and leases on
// no node or on an unlimited node count once each.
func TestNodeOverAdmit_CountPoolByNodeCapsEachNodeAtItsLimit(t *testing.T) {
	l := New(Options{PollInterval: nqPoll, Clock: newFakeClock(), CountPoolByNode: ooTrue})
	l.SetLimit("node:"+ooNodeA, 1)
	l.SetLimit(ooLLM, 5)
	l.Adopt(ooResumed("a-1", ooNodeA), "s1")
	l.Adopt(ooResumed("a-2", ooNodeA), "s2")
	l.Adopt(Request{Holder: "free-1", Pools: []string{"host", ooLLM}, Node: ooNodeB}, "s3") // node B has no limit
	l.Adopt(Request{Holder: "free-2", Pools: []string{"host", ooLLM}}, "s4")                // no node
	l.mu.Lock()
	got := l.usedLocked(ooLLM)
	l.mu.Unlock()
	if got != 3 {
		t.Fatalf("%s used = %d, want 3: one for the full node, one for the unlimited node, one for no node", ooLLM, got)
	}
}

// The adoption hold ends on its timeout, so a gate nobody closes cannot stop
// admission for good. A waiter queued during the hold is granted on the poll
// tick after the timeout.
func TestNodeOverAdmit_AdoptionGateEndsOnTimeout(t *testing.T) {
	clk := newFakeClock()
	l := New(Options{PollInterval: nqPoll, Clock: clk, AdoptionTimeout: time.Minute})
	l.SetLimit("node:"+ooNodeA, 1)
	l.SetLimit("node:"+ooNodeB, 1)
	l.BeginAdoption()
	if d := l.Check(ooVerifier("v1")); d.Kind == DecisionAdmit || !strings.Contains(d.Reason, "adopted") {
		t.Fatalf("Check = %+v, want it held back for adoption", d)
	}
	clk.Advance(59 * time.Second)
	if l.WouldGrant(ooVerifier("v1")) {
		t.Fatal("granted before the adoption timeout")
	}
	clk.Advance(2 * time.Second)
	if !l.WouldGrant(ooVerifier("v1")) {
		t.Fatal("still held back after the adoption timeout")
	}
}

// Adoption itself is never held back by the gate, and MarkAdoptionComplete with
// no hold set is harmless.
func TestNodeOverAdmit_AdoptIgnoresGateAndCompleteWithoutBeginIsSafe(t *testing.T) {
	l := ooLedger(newFakeClock())
	l.MarkAdoptionComplete() // no hold: nothing happens
	l.BeginAdoption()
	l.Adopt(ooResumed("worker-1", ooNodeA), "sess-1")
	if !l.Held("worker-1") {
		t.Fatal("Adopt was held back by the adoption gate")
	}
	l.MarkAdoptionComplete()
	l.MarkAdoptionComplete()
}

// AdoptBeforeAdmit off: BeginAdoption sets no hold.
func TestNodeOverAdmit_AdoptBeforeAdmitOffSetsNoHold(t *testing.T) {
	l := New(Options{PollInterval: nqPoll, Clock: newFakeClock(), AdoptBeforeAdmit: ooFalse})
	l.SetLimit("node:"+ooNodeA, 1)
	l.SetLimit("node:"+ooNodeB, 1)
	l.BeginAdoption()
	if !l.WouldGrant(ooVerifier("v1")) {
		t.Fatal("a request was held back with AdoptBeforeAdmit off")
	}
}
