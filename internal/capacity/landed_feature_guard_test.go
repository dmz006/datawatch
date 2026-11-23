package capacity

import "testing"

// TestGuardUnhealthyNodeRefusedAndRecovers guards the landed unhealthy-node
// feature: a marked node is refused by placement and is used again once the
// mark is cleared.
func TestGuardUnhealthyNodeRefusedAndRecovers(t *testing.T) {
	l := nhLedger(newFakeClock(), &nodeHealth{})
	l.MarkNodeUnhealthy("b", "preflight failed")
	if bad, why := l.NodeUnhealthy("b"); !bad || why != "preflight failed" {
		t.Fatalf("NodeUnhealthy = %v %q", bad, why)
	}
	if got := ncAcquire(t, l, ncRequest("t1")); got.Node != "a" {
		t.Fatalf("lease node = %q, want a while b is marked", got.Node)
	}
	l.ClearNodeUnhealthy("b")
	if bad, _ := l.NodeUnhealthy("b"); bad {
		t.Fatal("b still unhealthy after clear")
	}
	if got := ncAcquire(t, l, ncRequest("t2")); got.Node != "b" {
		t.Fatalf("lease node = %q, want b after recovery", got.Node)
	}
}
