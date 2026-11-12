package capacity

import "testing"

var (
	piPools     = []string{"host", "llm:pi-prompt"}
	claudePools = []string{"host", "llm:claude-code"}
)

func poolHeld(t *testing.T, l *Ledger, name string) int {
	t.Helper()
	for _, p := range l.Snapshot().Pools {
		if p.Name == name {
			return p.Held
		}
	}
	return 0
}

// adoptOnDatawatch records holder as a pi-prompt lease on node datawatch.
func adoptOnDatawatch(l *Ledger, holder string) {
	l.Adopt(Request{Holder: holder, PRDID: "prd-1", Pools: piPools, Node: "datawatch"}, "sess-1")
}

func TestRehomeToNodelessPoolsFreesNode(t *testing.T) {
	l := nqLedger(newFakeClock(), 1)
	l.SetLimit("node:datawatch", 1)
	adoptOnDatawatch(l, "t1")
	if got := poolHeld(t, l, "node:datawatch"); got != 1 {
		t.Fatalf("setup: node:datawatch held = %d, want 1", got)
	}
	before := l.LeaseOf("t1")
	if !l.Rehome("t1", claudePools, "") {
		t.Fatal("Rehome returned false for a held lease")
	}
	if got := poolHeld(t, l, "node:datawatch"); got != 0 {
		t.Fatalf("node:datawatch held = %d after move, want 0", got)
	}
	ls := l.LeaseOf("t1")
	if ls == nil || ls.Node != "" || hasPool(ls, "node:datawatch") || !hasPool(ls, "llm:claude-code") || hasPool(ls, "llm:pi-prompt") {
		t.Fatalf("lease after move = %+v", ls)
	}
	if ls.SessionID != "sess-1" || ls.PRDID != "prd-1" || !ls.Acquired.Equal(before.Acquired) {
		t.Fatalf("Rehome must keep session, PRD and Acquired: %+v", ls)
	}
	if n := len(l.Snapshot().Leases); n != 1 {
		t.Fatalf("lease count = %d, want 1", n)
	}
}

func TestRehomeGrantsWaiterOnFreedNode(t *testing.T) {
	clk := newFakeClock()
	l := nqLedger(clk, 1)
	l.SetLimit("node:datawatch", 1)
	adoptOnDatawatch(l, "t1")

	done := nqAcquire(l, Request{Holder: "w1", PRDID: "prd-2", Pools: []string{"host", "llm:pi-prompt", "node:datawatch"}})
	waitPending(t, clk, 1)
	if l.Held("w1") {
		t.Fatal("w1 granted while node:datawatch is full")
	}
	if !l.Rehome("t1", claudePools, "") {
		t.Fatal("Rehome returned false")
	}
	nqExpectGranted(t, done, "w1")
	if got := poolHeld(t, l, "node:datawatch"); got != 1 {
		t.Fatalf("node:datawatch held = %d, want 1 (the waiter)", got)
	}
}

func TestRehomeUnknownHolder(t *testing.T) {
	l := nqLedger(newFakeClock(), 1)
	if l.Rehome("nobody", claudePools, "") {
		t.Fatal("Rehome of an unknown holder returned true")
	}
	if n := len(l.Snapshot().Leases); n != 0 {
		t.Fatalf("lease count = %d, want 0", n)
	}
}

func TestRehomeWithNodeAddsNodePool(t *testing.T) {
	l := nqLedger(newFakeClock(), 1)
	l.Adopt(Request{Holder: "t1", PRDID: "prd-1", Pools: claudePools}, "sess-1")
	if !l.Rehome("t1", piPools, "datawatch") {
		t.Fatal("Rehome returned false")
	}
	ls := l.LeaseOf("t1")
	if ls == nil || ls.Node != "datawatch" || !hasPool(ls, "node:datawatch") {
		t.Fatalf("lease = %+v", ls)
	}
}

func TestReadoptWithDifferentPoolsDropsNode(t *testing.T) {
	l := nqLedger(newFakeClock(), 1)
	l.SetLimit("node:datawatch", 1)
	adoptOnDatawatch(l, "t1")
	l.Adopt(Request{Holder: "t1", PRDID: "prd-1", Pools: claudePools}, "sess-1")

	ls := l.LeaseOf("t1")
	if ls == nil || ls.Node != "" || hasPool(ls, "node:datawatch") || !hasPool(ls, "llm:claude-code") {
		t.Fatalf("lease after re-adopt = %+v", ls)
	}
	if got := poolHeld(t, l, "node:datawatch"); got != 0 {
		t.Fatalf("node:datawatch held = %d, want 0", got)
	}
}

func TestReadoptWithSamePoolsKeepsNode(t *testing.T) {
	l := nqLedger(newFakeClock(), 1)
	l.SetLimit("node:datawatch", 1)
	adoptOnDatawatch(l, "t1")
	l.Adopt(Request{Holder: "t1", PRDID: "prd-1", Pools: piPools}, "sess-1")

	ls := l.LeaseOf("t1")
	if ls == nil || ls.Node != "datawatch" || !hasPool(ls, "node:datawatch") {
		t.Fatalf("same-pool re-adopt must keep the node, got %+v", ls)
	}
	if got := poolHeld(t, l, "node:datawatch"); got != 1 {
		t.Fatalf("node:datawatch held = %d, want 1", got)
	}
}
