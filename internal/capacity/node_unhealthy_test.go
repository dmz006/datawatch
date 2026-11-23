package capacity

import (
	"sync"
	"testing"
)

func TestMarkNodeUnhealthyRoundTrip(t *testing.T) {
	l := New(Options{PollInterval: nqPoll, Clock: newFakeClock()})
	if bad, _ := l.NodeUnhealthy("a"); bad {
		t.Fatal("fresh node reported unhealthy")
	}
	l.MarkNodeUnhealthy("a", "probe failed")
	if bad, why := l.NodeUnhealthy("a"); !bad || why != "probe failed" {
		t.Fatalf("NodeUnhealthy = %v %q", bad, why)
	}
	l.ClearNodeUnhealthy("a")
	if bad, _ := l.NodeUnhealthy("a"); bad {
		t.Fatal("still unhealthy after clear")
	}
}

func TestMarkedNodeSkippedByPlacement(t *testing.T) {
	l := nhLedger(newFakeClock(), &nodeHealth{})
	l.MarkNodeUnhealthy("b", "probe failed")
	ls := ncAcquire(t, l, ncRequest("t1"))
	if ls.Node != "a" {
		t.Fatalf("lease node = %q, want a because b is marked", ls.Node)
	}
	l.ClearNodeUnhealthy("b")
	ls2 := ncAcquire(t, l, ncRequest("t2"))
	if ls2.Node != "b" {
		t.Fatalf("lease node = %q, want b after clear", ls2.Node)
	}
}

func TestMarkShownInSnapshot(t *testing.T) {
	l := nhLedger(newFakeClock(), &nodeHealth{})
	l.MarkNodeUnhealthy("a", "no model")
	for _, p := range l.Snapshot().Pools {
		if p.Name == "node:a" && (!p.Unhealthy || p.UnhealthyReason != "no model") {
			t.Fatalf("pool = %+v, want unhealthy mark", p)
		}
		if p.Name == "node:b" && p.Unhealthy {
			t.Fatalf("pool b unexpectedly unhealthy")
		}
	}
}

func TestMarkConcurrent(t *testing.T) {
	l := New(Options{PollInterval: nqPoll, Clock: newFakeClock()})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.MarkNodeUnhealthy("a", "x")
			l.NodeUnhealthy("a")
			l.ClearNodeUnhealthy("a")
		}()
	}
	wg.Wait()
}

func TestUnhealthyMarkReplacesReason(t *testing.T) {
	l := New(Options{PollInterval: nqPoll, Clock: newFakeClock()})
	l.MarkNodeUnhealthy("a", "first")
	l.MarkNodeUnhealthy("a", "second")
	if bad, why := l.NodeUnhealthy("a"); !bad || why != "second" {
		t.Fatalf("NodeUnhealthy = %v %q, want second", bad, why)
	}
}

func TestUnhealthyEmptyNodeIgnored(t *testing.T) {
	l := New(Options{PollInterval: nqPoll, Clock: newFakeClock()})
	l.MarkNodeUnhealthy("", "no name")
	if bad, _ := l.NodeUnhealthy(""); bad {
		t.Fatal("empty node name was marked")
	}
}

func TestUnhealthyClearUnmarkedNodeIsNoop(t *testing.T) {
	l := New(Options{PollInterval: nqPoll, Clock: newFakeClock()})
	l.ClearNodeUnhealthy("never-marked")
	if bad, _ := l.NodeUnhealthy("never-marked"); bad {
		t.Fatal("node unhealthy after clearing an unmarked node")
	}
}

// TestUnhealthyMarkIsPerNode checks marking one node leaves another alone and
// clearing it does not clear the other.
func TestUnhealthyMarkIsPerNode(t *testing.T) {
	l := New(Options{PollInterval: nqPoll, Clock: newFakeClock()})
	l.MarkNodeUnhealthy("a", "x")
	l.MarkNodeUnhealthy("b", "y")
	l.ClearNodeUnhealthy("a")
	if bad, _ := l.NodeUnhealthy("a"); bad {
		t.Fatal("a still marked")
	}
	if bad, why := l.NodeUnhealthy("b"); !bad || why != "y" {
		t.Fatalf("b = %v %q, want marked y", bad, why)
	}
}

// TestUnhealthyMarkSkipsNodeEvenWhenHealthCheckPasses checks the mark is
// independent of the Options.NodeHealthy callback: a node the callback calls
// healthy is still skipped while marked, and used again once cleared.
func TestUnhealthyMarkSkipsNodeEvenWhenHealthCheckPasses(t *testing.T) {
	h := &nodeHealth{}
	h.set("b", false)
	l := nhLedger(newFakeClock(), h)
	l.MarkNodeUnhealthy("b", "preflight failed")
	if got := ncAcquire(t, l, ncRequest("t1")); got.Node != "a" {
		t.Fatalf("lease node = %q, want a while b is marked", got.Node)
	}
	l.ClearNodeUnhealthy("b")
	if got := ncAcquire(t, l, ncRequest("t2")); got.Node != "b" {
		t.Fatalf("lease node = %q, want b after clear", got.Node)
	}
}

// TestUnhealthyMarkAndCallbackBothSkip checks a node is skipped when either
// the mark or the callback says unhealthy, and clearing the mark alone does
// not bring back a node the callback still rejects.
func TestUnhealthyMarkAndCallbackBothSkip(t *testing.T) {
	h := &nodeHealth{}
	h.set("b", true)
	l := nhLedger(newFakeClock(), h)
	l.MarkNodeUnhealthy("b", "preflight failed")
	l.ClearNodeUnhealthy("b")
	if got := ncAcquire(t, l, ncRequest("t1")); got.Node != "a" {
		t.Fatalf("lease node = %q, want a: callback still reports b unhealthy", got.Node)
	}
}

func TestUnhealthyMarkClearedInSnapshot(t *testing.T) {
	l := nhLedger(newFakeClock(), &nodeHealth{})
	l.MarkNodeUnhealthy("a", "no model")
	l.ClearNodeUnhealthy("a")
	for _, p := range l.Snapshot().Pools {
		if p.Unhealthy || p.UnhealthyReason != "" {
			t.Fatalf("pool %s = %+v, want no unhealthy mark", p.Name, p)
		}
	}
}
