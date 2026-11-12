package capacity

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// Snapshot must report the CURRENT wait reason, not the one the last dispatch
// pass left behind. These tests use a poll interval far longer than the clock
// moves, so no tick fires between the change and the read.

const (
	wrAdoptText = "waiting for work resumed after the restart to be adopted"
	wrPoll      = 24 * time.Hour
	wrTimeout   = 3 * time.Minute
)

// wrLedger holds node:alpha (limit 1) with holder "owner", then begins the
// adoption hold and queues "t1" behind it.
func wrLedger(t *testing.T, clk *fakeClock, onWait func(string)) (*Ledger, <-chan error) {
	t.Helper()
	l := New(Options{PollInterval: wrPoll, Clock: clk, AdoptionTimeout: wrTimeout})
	l.SetLimit("node:alpha", 1)
	owner := Request{Holder: "owner", PRDID: "prd-0", Pools: []string{"node:alpha"}, Node: "alpha"}
	if err := l.Acquire(context.Background(), owner, 0, nil, nil); err != nil {
		t.Fatalf("owner: %v", err)
	}
	l.BeginAdoption()
	done := make(chan error, 1)
	go func() {
		done <- l.Acquire(context.Background(),
			Request{Holder: "t1", PRDID: "prd-1", Pools: []string{"node:alpha"}, Node: "alpha"},
			0, nil, onWait)
	}()
	waitPending(t, clk, 1)
	return l, done
}

func wrWantNodeReason(t *testing.T, got string) {
	t.Helper()
	if !strings.HasPrefix(got, "waiting for node alpha: 1/1 engines in use, held by ") {
		t.Fatalf("reason = %q, want the node-engine reason", got)
	}
}

func TestSnapshotReasonRecomputedAfterAdoptionTimeout(t *testing.T) {
	clk := newFakeClock()
	l, _ := wrLedger(t, clk, nil)
	if got := nqWaitingReason(t, l, "t1"); got != wrAdoptText {
		t.Fatalf("reason = %q, want the adoption hold", got)
	}
	clk.Advance(wrTimeout + time.Second) // no tick: poll is 24h
	wrWantNodeReason(t, nqWaitingReason(t, l, "t1"))
	l.mu.Lock()
	l.dispatchLocked()
	var got string
	for _, w := range l.waiters {
		got = w.reason
	}
	l.mu.Unlock()
	wrWantNodeReason(t, got)
}

func TestSnapshotReasonRecomputedAfterMarkAdoptionComplete(t *testing.T) {
	clk := newFakeClock()
	l, _ := wrLedger(t, clk, nil)
	if got := nqWaitingReason(t, l, "t1"); got != wrAdoptText {
		t.Fatalf("reason = %q, want the adoption hold", got)
	}
	l.MarkAdoptionComplete()
	wrWantNodeReason(t, nqWaitingReason(t, l, "t1"))
}

// The snapshot read neither grants nor reorders anyone.
func TestSnapshotRecomputeDoesNotGrant(t *testing.T) {
	clk := newFakeClock()
	l, _ := wrLedger(t, clk, nil)
	clk.Advance(wrTimeout + time.Second)
	l.Snapshot()
	if l.Held("t1") {
		t.Fatal("Snapshot granted a waiter")
	}
}

// onWait fires for a change that Snapshot already noticed between ticks.
func TestOnWaitFiresWhenReasonChangedBetweenTicks(t *testing.T) {
	clk := newFakeClock()
	var mu sync.Mutex
	var seen []string
	l, _ := wrLedger(t, clk, func(r string) {
		mu.Lock()
		seen = append(seen, r)
		mu.Unlock()
	})
	// Wait for the first report.
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(seen)
		mu.Unlock()
		if n >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no initial onWait")
		}
		time.Sleep(time.Millisecond)
	}
	clk.Advance(wrTimeout + time.Second)
	nqWaitingReason(t, l, "t1") // Snapshot updates w.reason between ticks
	clk.Advance(wrPoll)         // now the tick fires
	deadline = time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(seen)
		last := ""
		if n > 0 {
			last = seen[n-1]
		}
		mu.Unlock()
		if n >= 2 {
			wrWantNodeReason(t, last)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("onWait not called with the changed reason; seen = %v", seen)
		}
		time.Sleep(time.Millisecond)
	}
}
