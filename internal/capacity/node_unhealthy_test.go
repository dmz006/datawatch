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
