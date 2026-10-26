package capacity

import "testing"

// A queued request recorded on node a whose list now offers only b is placed
// on b.
func TestWaiterRecordedOnRemovedNodeIsPlacedOnListedNode(t *testing.T) {
	clk := newFakeClock()
	l := ncLedger(clk)
	ncHold(t, l, "holder-a", "a")

	pl := &nrPlacement{}
	pl.set(nrBound("w", "a"))
	done := nrWaiter(t, l, clk, pl, 1)
	nrWaitReason(t, l, "w", "node a")

	cur := nrBound("w", "a")
	cur.Pools = []string{"host", "node:a"}
	cur.NodeChoices = []NodeChoice{{Name: "b", Priority: 5}, {Name: "c", Priority: 1}}
	pl.set(cur)
	clk.Advance(nqPoll)
	nqExpectGranted(t, done, "w")

	ls := l.LeaseOf("w")
	if ls == nil || ls.Node != "b" || hasPool(ls, "node:a") || !hasPool(ls, "node:b") {
		t.Fatalf("lease = %+v, want it on node b alone", ls)
	}
}

// A recorded node that is still among the choices is kept.
func TestWaiterRecordedOnListedNodeKeepsIt(t *testing.T) {
	clk := newFakeClock()
	l := ncLedger(clk)
	ncHold(t, l, "holder-b", "b")
	req := ncRequest("w")
	req.Node = "a"
	req.Pools = []string{"host", "node:a"}
	ls := ncAcquire(t, l, req)
	if ls.Node != "a" || !hasPool(ls, "node:a") {
		t.Fatalf("lease = %+v, want it kept on node a", ls)
	}
}
