package capacity

import (
	"fmt"
	"strings"
	"testing"
)

// Node over-admission: a node with a limit of one engine must never show more
// than one engine in use, whatever mix of workers, verifiers and resumed
// (adopted) sessions lands on it. Every test here names the failure it checks
// in its own name, so a red run says which rule broke.

const (
	ooNodeA = "datawatch"
	ooNodeB = "local-node"
	ooLLM   = "llm:pi-prompt"
)

// ooLedger builds the two-node ledger on the fake clock: one engine per node
// and two pi-prompt sessions. The host pool is left unlimited, as the other
// capacity tests leave it.
func ooLedger(clk *fakeClock) *Ledger {
	l := New(Options{PollInterval: nqPoll, Clock: clk})
	l.SetLimit("node:"+ooNodeA, 1)
	l.SetLimit("node:"+ooNodeB, 1)
	l.SetLimit(ooLLM, 2)
	return l
}

// ooBoth lists both nodes. datawatch has the higher priority, so an idle tie
// goes to it.
func ooBoth() []NodeChoice {
	return []NodeChoice{{Name: ooNodeA, Priority: 5}, {Name: ooNodeB, Priority: 1}}
}

// ooWorker is a worker already bound to one node, as a running session holds it.
func ooWorker(holder, node string) Request {
	return Request{
		Holder: holder, PRDID: "prd-1", Role: RoleWorker, Node: node,
		Pools: []string{"host", ooLLM, "node:" + node},
	}
}

// ooVerifier is a verifier that may run on either node.
func ooVerifier(holder string) Request {
	return Request{
		Holder: holder, PRDID: "prd-1", Role: RoleVerifier, Pools: []string{"host", ooLLM},
		NodeChoices: ooBoth(),
	}
}

// ooResumed is a worker whose session survived a daemon restart on node, with
// both nodes still listed as choices.
func ooResumed(holder, node string) Request {
	r := ooWorker(holder, node)
	r.Pools = []string{"host", ooLLM}
	r.NodeChoices = ooBoth()
	return r
}

// ooAssertNoNodeOver fails when any node pool reports more engines in use than
// its limit.
func ooAssertNoNodeOver(t *testing.T, l *Ledger, when string) {
	t.Helper()
	for _, p := range l.Snapshot().Pools {
		if !strings.HasPrefix(p.Name, "node:") || p.Limit <= 0 {
			continue
		}
		if used := p.Held + p.External; used > p.Limit {
			t.Errorf("%s: %s is over its limit: %d/%d in use, held by %v", when, p.Name, used, p.Limit, p.Holders)
		}
	}
}

// ooPoolHeld returns the held count of one pool from the snapshot.
func ooPoolHeld(t *testing.T, l *Ledger, pool string) int {
	t.Helper()
	for _, p := range l.Snapshot().Pools {
		if p.Name == pool {
			return p.Held
		}
	}
	return 0
}

// (1) A worker fills datawatch and local-node is idle. A verifier that may
// run on either must be bound to local-node, and once both are full the next
// verifier must wait. No node pool is ever over its limit.
func TestNodeOverAdmit_VerifierWithChoicesTakesIdleNodeOrWaits(t *testing.T) {
	clk := newFakeClock()
	l := ooLedger(clk)
	ncAcquire(t, l, ooWorker("worker-1", ooNodeA))
	ooAssertNoNodeOver(t, l, "worker holds datawatch")

	v1 := ncAcquire(t, l, ooVerifier("verifier-1"))
	if v1.Node != ooNodeB || !hasPool(v1, "node:"+ooNodeB) || hasPool(v1, "node:"+ooNodeA) {
		t.Fatalf("verifier-1 was bound to node %q with pools %v, want %s: %s is at its limit", v1.Node, v1.Pools, ooNodeB, ooNodeA)
	}
	ooAssertNoNodeOver(t, l, "verifier-1 granted")

	done := nqAcquire(l, ooVerifier("verifier-2"))
	waitPending(t, clk, 1)
	if l.Held("verifier-2") {
		t.Fatalf("verifier-2 was granted with both nodes full: %+v", l.LeaseOf("verifier-2"))
	}
	reason := nqWaitingReason(t, l, "verifier-2")
	for _, want := range []string{ooNodeA, ooNodeB} {
		if !strings.Contains(reason, want) {
			t.Errorf("wait reason = %q, want it to name %s", reason, want)
		}
	}
	ooAssertNoNodeOver(t, l, "verifier-2 waiting")

	l.Release("worker-1")
	nqExpectGranted(t, done, "verifier-2")
	if got := l.LeaseOf("verifier-2"); got == nil || got.Node != ooNodeA {
		t.Fatalf("verifier-2 lease = %+v, want %s once the worker released it", got, ooNodeA)
	}
	ooAssertNoNodeOver(t, l, "verifier-2 granted")
}

// (2) A verifier pinned to one node (its Pools carry node:datawatch, no
// NodeChoices) must wait while that node is full, even though the other node
// is idle.
func TestNodeOverAdmit_PinnedVerifierWaitsOnFullNode(t *testing.T) {
	clk := newFakeClock()
	l := ooLedger(clk)
	ncAcquire(t, l, ooWorker("worker-1", ooNodeA))

	pinned := Request{
		Holder: "verifier-pinned", PRDID: "prd-1", Role: RoleVerifier,
		Pools: []string{"host", ooLLM, "node:" + ooNodeA}, Node: ooNodeA,
	}
	if d := l.Check(pinned); d.Kind == DecisionAdmit {
		t.Fatalf("Check admitted a pinned verifier onto the full node: %+v", d)
	}
	if l.WouldGrant(pinned) {
		t.Fatal("WouldGrant is true for a pinned verifier on the full node")
	}

	done := nqAcquire(l, pinned)
	waitPending(t, clk, 1)
	if l.Held("verifier-pinned") {
		t.Fatalf("pinned verifier was granted on the full node: %+v", l.LeaseOf("verifier-pinned"))
	}
	if reason := nqWaitingReason(t, l, "verifier-pinned"); !strings.Contains(reason, "node "+ooNodeA) {
		t.Errorf("wait reason = %q, want it to name node %s", reason, ooNodeA)
	}
	ooAssertNoNodeOver(t, l, "pinned verifier waiting")

	l.Release("worker-1")
	nqExpectGranted(t, done, "verifier-pinned")
	ooAssertNoNodeOver(t, l, "pinned verifier granted")
}

// (3a) A resumed worker whose session sits on a node that is already at its
// limit, and which had another node among its choices, must not push the node
// over its limit. Adopt ignores pool limits today, so datawatch reads 2/1.
func TestNodeOverAdmit_AdoptWithAlternativeNodeNeverOverLimit(t *testing.T) {
	l := ooLedger(newFakeClock())
	ncAcquire(t, l, ooWorker("worker-1", ooNodeA))

	l.Adopt(ooResumed("worker-2", ooNodeA), "sess-2")

	ooAssertNoNodeOver(t, l, "resumed worker adopted while datawatch was full")
	if !l.Held("worker-2") {
		t.Fatal("the resumed worker holds no lease at all; a running session must stay counted")
	}
}

// (3b) The llm pool counts each session once per node. A session adopted twice
// under two holder keys (the task key and its relaunch key) is one session, so
// it uses one slot in the llm pool and one on its node.
func TestNodeOverAdmit_AdoptCountsSessionOncePerNode(t *testing.T) {
	l := ooLedger(newFakeClock())
	l.Adopt(ooResumed("worker-1", ooNodeB), "sess-1")
	l.Adopt(ooResumed("worker-1", ooNodeB), "sess-1") // same holder again: idempotent
	if got := ooPoolHeld(t, l, ooLLM); got != 1 {
		t.Fatalf("%s held = %d after adopting one session twice under one holder, want 1", ooLLM, got)
	}

	l.Adopt(ooResumed("worker-1-relaunch", ooNodeB), "sess-1") // same session, second key
	if got := ooPoolHeld(t, l, ooLLM); got != 1 {
		t.Errorf("%s held = %d after the same session was adopted under a second holder, want 1 (one session, counted once)", ooLLM, got)
	}
	if got := ooPoolHeld(t, l, "node:"+ooNodeB); got != 1 {
		t.Errorf("node:%s held = %d for one session, want 1", ooNodeB, got)
	}
	ooAssertNoNodeOver(t, l, "same session adopted under two keys")
}

// (4a) Restart with the workers adopted first: a new multi-node request is
// placed on the idle node, and once both are taken the waiter reason names the
// nodes.
func TestNodeOverAdmit_RestartAdoptedFirstThenNewRequestUsesIdleNode(t *testing.T) {
	clk := newFakeClock()
	l := ooLedger(clk)
	l.Adopt(ooResumed("worker-1", ooNodeA), "sess-1")

	got := ncAcquire(t, l, ooVerifier("verifier-1"))
	if got.Node != ooNodeB {
		t.Fatalf("verifier-1 node = %q, want %s (the idle node; %s holds the resumed worker)", got.Node, ooNodeB, ooNodeA)
	}
	ooAssertNoNodeOver(t, l, "adopted first, then a new request")

	done := nqAcquire(l, ooVerifier("verifier-2"))
	waitPending(t, clk, 1)
	reason := nqWaitingReason(t, l, "verifier-2")
	if !strings.Contains(reason, "nodes "+ooNodeA+", "+ooNodeB) && !strings.Contains(reason, "nodes "+ooNodeB+", "+ooNodeA) {
		t.Errorf("wait reason = %q, want it to name both nodes", reason)
	}
	for _, w := range l.Snapshot().Waiting {
		if w.Holder == "verifier-2" && len(w.Nodes) != 2 {
			t.Errorf("waiter Nodes = %v, want both candidate nodes", w.Nodes)
		}
	}
	l.Release("worker-1")
	nqExpectGranted(t, done, "verifier-2")
	ooAssertNoNodeOver(t, l, "slot freed and handed on")
}

// adoptionGate is the admission gate the restart path needs: it answers
// not-ready until the boot-resume adoption has finished, so a new request
// cannot take a slot a resumed worker is about to occupy. The ledger has no
// such gate, so the test below probes for it; the method names are the
// proposal, not existing API.
type adoptionGate interface {
	BeginAdoption()
	MarkAdoptionComplete()
}

// (4b) A new multi-node request made BEFORE adoption must not take the slot a
// resumed worker is about to occupy. With no gate, the request lands on
// datawatch (the higher priority on an idle tie) and the adoption that follows
// puts a second engine on it.
func TestNodeOverAdmit_RestartRequestBeforeAdoptionDoesNotTakeResumedSlot(t *testing.T) {
	clk := newFakeClock()
	l := ooLedger(clk)

	gate, ok := any(l).(adoptionGate)
	if !ok {
		// Show the concrete damage first, so the failure is more than a missing method.
		early := ncAcquire(t, l, ooVerifier("verifier-early"))
		l.Adopt(ooResumed("worker-resumed", ooNodeA), "sess-resumed")
		ooAssertNoNodeOver(t, l, fmt.Sprintf("request granted on %s before adoption, then the resumed worker adopted", early.Node))
		t.Fatalf("the ledger has no admission gate (BeginAdoption/MarkAdoptionComplete): a request made before adoption is granted at once")
	}

	gate.BeginAdoption()
	done := nqAcquire(l, ooVerifier("verifier-early"))
	waitPending(t, clk, 1)
	if l.Held("verifier-early") {
		t.Fatal("a request was granted while adoption was incomplete")
	}
	if d := l.Check(ooVerifier("probe")); d.Kind == DecisionAdmit {
		t.Fatalf("Check admitted during adoption: %+v", d)
	}
	l.Adopt(ooResumed("worker-resumed", ooNodeA), "sess-resumed")
	gate.MarkAdoptionComplete()
	l.Replan()
	nqExpectGranted(t, done, "verifier-early")
	if got := l.LeaseOf("verifier-early"); got == nil || got.Node != ooNodeB {
		t.Fatalf("verifier-early lease = %+v, want %s: %s holds the resumed worker", got, ooNodeB, ooNodeA)
	}
	ooAssertNoNodeOver(t, l, "after the gate opened")
}

// (5a) The pool cap counts by node: with llm:pi-prompt at 2/2 because both
// sessions sit on datawatch (restart left them there), a request that fits
// local-node is admitted there instead of being refused for a pool whose
// slots are on the other node.
func TestNodeOverAdmit_PoolCapCountsByNodeNotAcrossNodes(t *testing.T) {
	l := ooLedger(newFakeClock())
	l.Adopt(ooResumed("worker-1", ooNodeA), "sess-1")
	l.Adopt(ooResumed("worker-2", ooNodeA), "sess-2")
	if got := ooPoolHeld(t, l, ooLLM); got != 2 {
		t.Fatalf("setup: %s held = %d, want 2", ooLLM, got)
	}

	d := l.Check(ooVerifier("verifier-1"))
	if d.Kind != DecisionAdmit || d.Node != ooNodeB {
		t.Fatalf("Check = %+v, want admit on %s: it is idle and the pool's slots are all on %s", d, ooNodeB, ooNodeA)
	}
	got := ncAcquire(t, l, ooVerifier("verifier-1"))
	if got.Node != ooNodeB {
		t.Fatalf("verifier-1 node = %q, want %s", got.Node, ooNodeB)
	}
	// local-node now has its one engine, so a third request is refused.
	if d := l.Check(ooVerifier("verifier-2")); d.Kind == DecisionAdmit {
		t.Fatalf("a third request was admitted: %+v", d)
	}
}

// (5b) Two sessions on two nodes fill the pool and are allowed; a third is
// refused.
func TestNodeOverAdmit_PoolCapTwoSessionsOnTwoNodesThirdRefused(t *testing.T) {
	l := ooLedger(newFakeClock())
	first := ncAcquire(t, l, ooVerifier("session-1"))
	second := ncAcquire(t, l, ooVerifier("session-2"))
	if first.Node == second.Node || first.Node == "" || second.Node == "" {
		t.Fatalf("two sessions landed on nodes %q and %q, want one on each", first.Node, second.Node)
	}
	ooAssertNoNodeOver(t, l, "two sessions on two nodes")
	if got := ooPoolHeld(t, l, ooLLM); got != 2 {
		t.Fatalf("%s held = %d, want 2", ooLLM, got)
	}
	if d := l.Check(ooVerifier("session-3")); d.Kind == DecisionAdmit {
		t.Fatalf("a third session was admitted: %+v", d)
	}
	if l.WouldGrant(ooVerifier("session-3")) {
		t.Fatal("WouldGrant is true for a third session")
	}
}
