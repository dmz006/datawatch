package capacity

import "testing"

// A multi-node request rejected by the node gate on every candidate — each
// node drained, in maintenance, or its consumer denied — gets a wait reason
// that names the gate refusal up front (AllCandidatesGatePrefix) and keeps
// the per-candidate "node: reason" parts. A request whose candidates are
// merely full keeps the previous wording with no prefix.

func TestNodeGateAllCandidatesDrainedNamesTheGate(t *testing.T) {
	clk := newFakeClock()
	g := &fakeNodeGate{}
	g.drainNode("a", "drained")
	g.drainNode("b", "drained")
	l := ngLedger(clk, g)

	done := nqAcquire(l, ngRequest("t1", "session_spawn"))
	waitPending(t, clk, 1)
	if l.Held("t1") {
		t.Fatal("t1 holds a lease although every candidate is drained")
	}
	reason := nqWaitingReason(t, l, "t1")
	want := "waiting for nodes b, a: " +
		AllCandidatesGatePrefix + "b: drained; a: drained"
	if reason != want {
		t.Fatalf("wait reason = %q, want %q", reason, want)
	}
	_ = done
}

func TestNodeGateAllCandidatesDeniedNamesTheGate(t *testing.T) {
	clk := newFakeClock()
	g := &fakeNodeGate{}
	g.denyConsumer("a", "session_spawn", "consumer session_spawn denied")
	g.denyConsumer("b", "session_spawn", "consumer session_spawn denied")
	l := ngLedger(clk, g)

	done := nqAcquire(l, ngRequest("t1", "session_spawn"))
	waitPending(t, clk, 1)
	if l.Held("t1") {
		t.Fatal("t1 holds a lease although every candidate denies its consumer kind")
	}
	reason := nqWaitingReason(t, l, "t1")
	want := "waiting for nodes b, a: " +
		AllCandidatesGatePrefix +
		"b: consumer session_spawn denied; a: consumer session_spawn denied"
	if reason != want {
		t.Fatalf("wait reason = %q, want %q", reason, want)
	}
	// The denial is per consumer kind: another kind is still granted.
	if ls := ncAcquire(t, l, ngRequest("t2", "agent_session")); !hasPool(ls, "node:a") && !hasPool(ls, "node:b") {
		t.Fatalf("lease = %+v, want a grant on some node", ls)
	}
	_ = done
}

func TestNodeGateMixedDrainAndFullHasNoGatePrefix(t *testing.T) {
	clk := newFakeClock()
	g := &fakeNodeGate{}
	g.drainNode("a", "drained")
	l := ngLedger(clk, g)
	ncHold(t, l, "t1", "b")

	// One candidate is gated and the other merely full: the old wording
	// stands, no gate prefix.
	done := nqAcquire(l, ngRequest("t2", "session_spawn"))
	waitPending(t, clk, 1)
	reason := nqWaitingReason(t, l, "t2")
	want := "waiting for nodes a, b: a: drained; b: 1/1 engines in use, held by t1"
	if reason != want {
		t.Fatalf("wait reason = %q, want %q", reason, want)
	}
	_ = done
}

// Candidates that are full, with no gate set, keep the previous wording
// exactly, in both the multi-node and the single-node forms.

func TestAllFullCandidatesKeepPreviousWording(t *testing.T) {
	clk := newFakeClock()
	l := ncLedger(clk)
	ncHold(t, l, "t1", "b")
	ncHold(t, l, "t2", "a")

	done := nqAcquire(l, ncRequest("t3"))
	waitPending(t, clk, 1)
	reason := nqWaitingReason(t, l, "t3")
	want := "waiting for nodes b, a: " +
		"b: 1/1 engines in use, held by t1; a: 1/1 engines in use, held by t2"
	if reason != want {
		t.Fatalf("wait reason = %q, want %q", reason, want)
	}
	_ = done
}

func TestSingleNodeFullKeepsPreviousWording(t *testing.T) {
	clk := newFakeClock()
	l := ncLedger(clk)
	ncHold(t, l, "t1", "a")

	req := Request{Holder: "t2", PRDID: "prd-1", Pools: []string{"node:a"}, Node: "a"}
	done := nqAcquire(l, req)
	waitPending(t, clk, 1)
	reason := nqWaitingReason(t, l, "t2")
	want := "waiting for node a: 1/1 engines in use, held by t1"
	if reason != want {
		t.Fatalf("wait reason = %q, want %q", reason, want)
	}
	_ = done
}
