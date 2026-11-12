package capacity

import (
	"context"
	"testing"
	"time"
)

// csLedger is the production-shaped setup: two llm pools and one node pool.
func csLedger() *Ledger {
	l := New(Options{PollInterval: time.Minute, Clock: newFakeClock()})
	l.SetLimit("llm:pi-prompt", 2)
	l.SetLimit("llm:claude-code", 2)
	l.SetLimit("node:datawatch", 2)
	return l
}

func csHold(t *testing.T, l *Ledger, holder string, pools ...string) {
	t.Helper()
	if err := l.Acquire(context.Background(), Request{Holder: holder, PRDID: "prd-1", Pools: pools}, 0, nil, nil); err != nil {
		t.Fatalf("%s: Acquire: %v", holder, err)
	}
}

func csFired(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func TestLedgerChangedFiresOnRelease(t *testing.T) {
	l := csLedger()
	csHold(t, l, "t1", "llm:pi-prompt", "node:datawatch")
	ch := l.Changed()
	l.Release("t1")
	if !csFired(ch) {
		t.Fatal("Release of a held lease must close the channel")
	}
}

func TestLedgerChangedIgnoresReleaseOfUnknownHolder(t *testing.T) {
	l := csLedger()
	ch := l.Changed()
	l.Release("nobody")
	if csFired(ch) {
		t.Fatal("Release of an unknown holder is not a change")
	}
}

func TestLedgerChangedFiresOnSetLimit(t *testing.T) {
	l := csLedger()
	ch := l.Changed()
	l.SetLimit("llm:pi-prompt", 3)
	if !csFired(ch) {
		t.Fatal("SetLimit must close the channel")
	}
}

func TestLedgerChangedFiresOnReplan(t *testing.T) {
	l := csLedger()
	ch := l.Changed()
	l.Replan()
	if !csFired(ch) {
		t.Fatal("Replan must close the channel")
	}
}

func TestLedgerChangedIgnoresWaiterRemoval(t *testing.T) {
	l := csLedger()
	csHold(t, l, "a", "llm:pi-prompt")
	csHold(t, l, "b", "llm:pi-prompt")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- l.Acquire(ctx, Request{Holder: "w", PRDID: "prd-1", Pools: []string{"llm:pi-prompt"}}, 0, nil, nil)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for len(l.Snapshot().Waiting) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("waiter never queued")
		}
		time.Sleep(time.Millisecond)
	}
	ch := l.Changed()
	cancel()
	<-done
	if csFired(ch) {
		t.Fatal("removing a waiter that frees nothing is not a change")
	}
}

func TestLedgerChangedReturnsFreshChannelAfterChange(t *testing.T) {
	l := csLedger()
	first := l.Changed()
	l.Replan()
	second := l.Changed()
	if !csFired(first) {
		t.Fatal("first channel must be closed by the change")
	}
	if csFired(second) {
		t.Fatal("a channel taken after a change must be open")
	}
	if first == second {
		t.Fatal("want a new channel after a change")
	}
	if again := l.Changed(); again != second {
		t.Fatal("without a change, Changed must keep returning the same open channel")
	}
	l.SetLimit("node:datawatch", 4)
	if !csFired(second) {
		t.Fatal("second channel must close on the next change")
	}
}

func TestLedgerWouldGrantFollowsPoolRoom(t *testing.T) {
	l := csLedger()
	csHold(t, l, "t1", "llm:pi-prompt")
	csHold(t, l, "t2", "llm:pi-prompt")
	req := Request{Holder: "t3", PRDID: "prd-1", Pools: []string{"llm:pi-prompt"}}
	if l.WouldGrant(req) {
		t.Fatal("pool is full: want false")
	}
	l.Release("t1")
	if !l.WouldGrant(req) {
		t.Fatal("after Release there is room: want true")
	}
}

func TestLedgerWouldGrantIsReadOnly(t *testing.T) {
	l := csLedger()
	l.SetRate("llm:pi-prompt", RateCeiling{Requests: 5, Window: time.Hour})
	csHold(t, l, "t1", "llm:pi-prompt")
	before := l.Snapshot()
	req := Request{Holder: "t2", PRDID: "prd-2", Pools: []string{"llm:pi-prompt", "node:datawatch"}, Node: "datawatch", Model: "m"}
	if !l.WouldGrant(req) {
		t.Fatal("want true")
	}
	if l.Held("t2") {
		t.Fatal("WouldGrant must not take a lease")
	}
	after := l.Snapshot()
	if len(after.Waiting) != 0 || len(after.Leases) != len(before.Leases) {
		t.Fatalf("snapshot changed: before %+v after %+v", before, after)
	}
	for i, p := range after.Pools {
		if p.Held != before.Pools[i].Held || p.RequestsInWindow != before.Pools[i].RequestsInWindow {
			t.Fatalf("pool %s changed: before %+v after %+v", p.Name, before.Pools[i], p)
		}
	}
	// Repeated calls must not consume the rate ceiling either.
	for i := 0; i < 10; i++ {
		if !l.WouldGrant(req) {
			t.Fatalf("call %d: dry run must not record rate use", i)
		}
	}
	l.mu.Lock()
	lastGrant, lastModel := len(l.lastGrant), len(l.lastModel)
	l.mu.Unlock()
	if lastGrant != 1 || lastModel != 0 {
		t.Fatalf("lastGrant=%d lastModel=%d, want 1 and 0", lastGrant, lastModel)
	}
}

func TestLedgerWouldGrantFalseWhenOnlyNodePoolFull(t *testing.T) {
	l := csLedger()
	csHold(t, l, "n1", "node:datawatch")
	csHold(t, l, "n2", "node:datawatch")
	req := Request{Holder: "t1", PRDID: "prd-1", Pools: []string{"llm:pi-prompt", "node:datawatch"}, Node: "datawatch"}
	if l.WouldGrant(req) {
		t.Fatal("node pool is full: want false even though llm:pi-prompt has room")
	}
	if !l.WouldGrant(Request{Holder: "t1", PRDID: "prd-1", Pools: []string{"llm:pi-prompt"}}) {
		t.Fatal("llm pool alone has room: want true")
	}
}

func TestLedgerWouldGrantMultiNodeNeedsOneCandidateWithRoom(t *testing.T) {
	l := csLedger()
	l.SetLimit("node:other", 1)
	csHold(t, l, "o1", "node:other")
	req := Request{
		Holder: "t1", PRDID: "prd-1", Pools: []string{"llm:claude-code"},
		NodeChoices: []NodeChoice{{Name: "other", Priority: 5}, {Name: "datawatch", Priority: 1}},
	}
	if !l.WouldGrant(req) {
		t.Fatal("datawatch has room: want true")
	}
	csHold(t, l, "d1", "node:datawatch")
	csHold(t, l, "d2", "node:datawatch")
	if l.WouldGrant(req) {
		t.Fatal("every candidate node is full: want false")
	}
	if l.Held("t1") {
		t.Fatal("WouldGrant must not take a lease")
	}
}
