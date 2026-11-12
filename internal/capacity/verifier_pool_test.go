package capacity

import (
	"strings"
	"sync"
	"testing"
)

// vpLedger builds a ledger on the fake clock with node a limited to one
// executor slot; when verifierLimit is above 0 the node also gets its
// per-node verifier pool. A node left at zero has NO verifier pool, which is
// the fallback case under test.
func vpLedger(clk *fakeClock, verifierLimit int) *Ledger {
	l := New(Options{PollInterval: nqPoll, Clock: clk})
	l.SetLimit("node:a", 1)
	if verifierLimit > 0 {
		l.SetLimit(VerifierPool("a"), verifierLimit)
	}
	return l
}

// vpVerifier is a verifier request the caller has bound to node a the usual
// way: node:a in Pools and Node set.
func vpVerifier(holder string) Request {
	return Request{Holder: holder, PRDID: "p-" + holder, Pools: []string{"node:a"}, Node: "a", Role: RoleVerifier}
}

// headroomGate stands in for an Options.VerifierHeadroom source the test can
// flip between calls.
type headroomGate struct {
	mu  sync.Mutex
	bad map[string]string
}

func (g *headroomGate) block(node, reason string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.bad == nil {
		g.bad = map[string]string{}
	}
	g.bad[node] = reason
}

func (g *headroomGate) clear(node string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.bad, node)
}

func (g *headroomGate) headroom(node string) (bool, string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if why, ok := g.bad[node]; ok {
		return true, why
	}
	return false, ""
}

func TestVerifierPoolFunctionAndSlots(t *testing.T) {
	if got := VerifierPool("a"); got != "node-verify:a" {
		t.Fatalf("VerifierPool(a) = %q, want node-verify:a", got)
	}
	l := New(Options{})
	if got := l.VerifierSlotsOn("a"); got != 0 {
		t.Fatalf("VerifierSlotsOn(a) = %d, want 0 (no pool configured)", got)
	}
	l.SetLimit(VerifierPool("a"), 3)
	if got := l.VerifierSlotsOn("a"); got != 3 {
		t.Fatalf("VerifierSlotsOn(a) = %d, want 3", got)
	}
	if got := l.VerifierSlotsOn("b"); got != 0 {
		t.Fatalf("VerifierSlotsOn(b) = %d, want 0", got)
	}
	l.SetLimit(VerifierPool("a"), 0) // 0 = no verifier pool, not unlimited
	if got := l.VerifierSlotsOn("a"); got != 0 {
		t.Fatalf("after SetLimit(0) VerifierSlotsOn(a) = %d, want 0", got)
	}
}

// A verifier lease takes node-verify:a; the executor slot node:a stays free
// and takes a worker directly behind it. The llm pool rides along unchanged
// in both leases.
func TestVerifierAdmittedWhileExecutorSlotFree(t *testing.T) {
	l := vpLedger(newFakeClock(), 1)
	l.SetLimit("llm:m1", 2)
	v := vpVerifier("v1")
	v.Pools = append(v.Pools, "llm:m1")
	ls := ncAcquire(t, l, v)
	if !hasPool(ls, "node-verify:a") {
		t.Fatalf("verifier Pools = %v, want node-verify:a", ls.Pools)
	}
	if hasPool(ls, "node:a") {
		t.Fatalf("verifier Pools = %v, the executor pool node:a must stay untouched", ls.Pools)
	}
	if !hasPool(ls, "llm:m1") {
		t.Fatalf("verifier Pools = %v, the llm pool must ride along", ls.Pools)
	}

	w := vpVerifier("w1")
	w.Role = RoleWorker
	w.Pools = append(w.Pools, "llm:m1")
	ws := ncAcquire(t, l, w) // node:a is still free although the verifier pool is full
	if !hasPool(ws, "node:a") || hasPool(ws, "node-verify:a") {
		t.Fatalf("worker Pools = %v, want node:a (and not node-verify:a)", ws.Pools)
	}
	if !hasPool(ws, "llm:m1") {
		t.Fatalf("worker Pools = %v, the llm pool must ride along", ws.Pools)
	}

	st := l.Snapshot()
	for _, p := range st.Pools {
		switch p.Name {
		case "node-verify:a":
			if p.Limit != 1 || p.Held != 1 || len(p.Holders) != 1 || p.Holders[0] != "v1" {
				t.Fatalf("node-verify:a pool = %+v, want limit 1 held 1 by v1", p)
			}
		case "node:a":
			if p.Limit != 1 || p.Held != 1 || len(p.Holders) != 1 || p.Holders[0] != "w1" {
				t.Fatalf("node:a pool = %+v, want limit 1 held 1 by w1", p)
			}
		case "llm:m1":
			if p.Held != 2 {
				t.Fatalf("llm:m1 pool = %+v, want both leases held", p)
			}
		}
	}
}

// The cap is per node-verify pool: the second verifier waits, and the reason
// it shows names the verifier slots, then it is admitted when the first
// releases without the executor pool ever having been involved.
func TestSecondVerifierWaitsForVerifierSlotUntilRelease(t *testing.T) {
	clk := newFakeClock()
	l := vpLedger(clk, 1)
	ncAcquire(t, l, vpVerifier("v1"))
	done := nqAcquire(l, vpVerifier("v2"))
	waitPending(t, clk, 1)

	reason := nqWaitingReason(t, l, "v2")
	if !strings.Contains(reason, "verifier slots") || !strings.Contains(reason, "node a") {
		t.Fatalf("wait reason = %q, want it to name node a's verifier pool", reason)
	}

	// node:a was never taken by v1, so the worker on it is admitted while
	// v2 still waits for its verifier slot.
	ws := ncAcquire(t, l, Request{Holder: "w1", PRDID: "p0", Pools: []string{"node:a"}, Node: "a"})
	if !hasPool(ws, "node:a") {
		t.Fatalf("worker Pools = %v, want node:a", ws.Pools)
	}

	l.Release("v1")
	nqExpectGranted(t, done, "v2")
	ls := l.LeaseOf("v2")
	if !hasPool(ls, "node-verify:a") || hasPool(ls, "node:a") {
		t.Fatalf("v2 Pools = %v, want node-verify:a only", ls.Pools)
	}
}

// A limit of 0 is not "unlimited" for a node-verify pool: it means the node
// has no verifier pool, so verifiers behave exactly as before on node:a.
func TestVerifierPoolLimitZeroFallsBackToNodePool(t *testing.T) {
	clk := newFakeClock()
	l := vpLedger(clk, 0)
	if l.VerifierSlotsOn("a") != 0 {
		t.Fatalf("VerifierSlotsOn(a) = %d, want 0", l.VerifierSlotsOn("a"))
	}
	ls := ncAcquire(t, l, vpVerifier("v1"))
	if !hasPool(ls, "node:a") || hasPool(ls, "node-verify:a") {
		t.Fatalf("Pools = %v, want the fallback to node:a only", ls.Pools)
	}
	done := nqAcquire(l, vpVerifier("v2"))
	waitPending(t, clk, 1)
	reason := nqWaitingReason(t, l, "v2")
	if !strings.Contains(reason, "node a") {
		t.Fatalf("wait reason = %q, want it to name node a", reason)
	}
	l.Release("v1")
	nqExpectGranted(t, done, "v2")
}

// A headroom-blocked candidate is skipped like an unhealthy node, and the
// waiting reason carries the headroom failure in plain English.
func TestVerifierHeadroomBlocksNodeAndNamesIt(t *testing.T) {
	clk := newFakeClock()
	g := &headroomGate{}
	g.block("a", "not enough free memory for verifier output")
	g.block("b", "verifier queue saturated")
	l := New(Options{PollInterval: nqPoll, Clock: clk, VerifierHeadroom: g.headroom})
	l.SetLimit("node:a", 1)
	l.SetLimit("node:b", 1)
	l.SetLimit(VerifierPool("a"), 1)
	l.SetLimit(VerifierPool("b"), 1)

	req := Request{Holder: "v1", PRDID: "p1", Role: RoleVerifier,
		NodeChoices: []NodeChoice{{Name: "a"}, {Name: "b"}}}
	done := nqAcquire(l, req)
	waitPending(t, clk, 1)

	if l.Held("v1") {
		t.Fatal("verifier admitted on headroom-blocked nodes")
	}
	reason := nqWaitingReason(t, l, "v1")
	if !strings.Contains(reason, "a: verifier headroom: not enough free memory for verifier output") ||
		!strings.Contains(reason, "b: verifier headroom: verifier queue saturated") {
		t.Fatalf("wait reason = %q, want it to name each headroom failure", reason)
	}

	g.clear("a")
	clk.Advance(nqPoll)
	nqExpectGranted(t, done, "v1")
	ls := l.LeaseOf("v1")
	if ls.Node != "a" || !hasPool(ls, VerifierPool("a")) || hasPool(ls, "node:a") {
		t.Fatalf("lease = %+v, want node a on its verifier pool once headroom recovers", ls)
	}
}

// Headroom only constrains verifiers: with the node headroom-blocked, a
// worker on the same node still takes its executor slot.
func TestVerifierHeadroomDoesNotBlockWorkers(t *testing.T) {
	g := &headroomGate{}
	g.block("a", "not enough free memory for verifier output")
	l := New(Options{PollInterval: nqPoll, Clock: newFakeClock(), VerifierHeadroom: g.headroom})
	l.SetLimit("node:a", 1)
	l.SetLimit(VerifierPool("a"), 1)

	w := vpVerifier("w1")
	w.Role = RoleWorker
	ls := ncAcquire(t, l, w)
	if !hasPool(ls, "node:a") {
		t.Fatalf("worker Pools = %v, want node:a: headroom is verifier-only", ls.Pools)
	}
}

// A full verifier pool blocks further verifiers, not workers: node:a is free
// and takes a worker while the verifier pool stays at its cap.
func TestVerifierPoolFullDoesNotBlockNonVerifier(t *testing.T) {
	l := vpLedger(newFakeClock(), 1)
	v := vpVerifier("v1")
	ls := ncAcquire(t, l, v)
	if !hasPool(ls, "node-verify:a") {
		t.Fatalf("verifier Pools = %v, want node-verify:a", ls.Pools)
	}
	w := vpVerifier("w1")
	w.Role = RoleWorker
	ws := ncAcquire(t, l, w)
	if !hasPool(ws, "node:a") || hasPool(ws, "node-verify:a") {
		t.Fatalf("worker Pools = %v, want node:a only", ws.Pools)
	}
}

// Without headroom there is nothing to block: with a nil VerifierHeadroom a
// verifier is admitted straight away, multi-node and single-node alike.
func TestNilVerifierHeadroomNeverBlocks(t *testing.T) {
	clk := newFakeClock()
	l := vpLedger(clk, 1) // Options.VerifierHeadroom is nil
	single := vpVerifier("v1")
	ls := ncAcquire(t, l, single)
	if !hasPool(ls, "node-verify:a") {
		t.Fatalf("single-node Pools = %v, want node-verify:a", ls.Pools)
	}

	l2 := New(Options{PollInterval: nqPoll, Clock: clk}) // VerifierHeadroom is nil
	l2.SetLimit("node:a", 1)
	l2.SetLimit("node:b", 1)
	l2.SetLimit(VerifierPool("a"), 1)
	ncHold(t, l2, "h1", "b")
	multi := Request{Holder: "v2", PRDID: "p2", Role: RoleVerifier,
		NodeChoices: []NodeChoice{{Name: "a"}, {Name: "b"}}}
	ms := ncAcquire(t, l2, multi)
	if ms.Node != "a" || !hasPool(ms, "node-verify:a") || hasPool(ms, "node:a") {
		t.Fatalf("multi-node lease = %+v, want node a on the verifier pool", ms)
	}
}
