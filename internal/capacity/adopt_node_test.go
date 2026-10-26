package capacity

import "testing"

func TestAdoptRecordsExplicitNodeAmongChoices(t *testing.T) {
	l := New(Options{PollInterval: nqPoll, Clock: newFakeClock()})
	req := ncRequest("w1")
	req.Node = "a"
	l.Adopt(req, "sess-1")

	got := l.LeaseOf("w1")
	if got == nil || got.Node != "a" || !hasPool(got, "node:a") || hasPool(got, "node:b") {
		t.Fatalf("adopted lease should sit on node a only, got %+v", got)
	}
	for _, p := range l.Snapshot().Pools {
		if p.Name == "node:a" && p.Held != 1 {
			t.Fatalf("node a should show one engine in use, got %d", p.Held)
		}
	}
}

func TestAdoptRecordsNodeOutsideCurrentChoices(t *testing.T) {
	l := New(Options{PollInterval: nqPoll, Clock: newFakeClock()})
	req := ncRequest("w1")
	req.Node = "gone"
	l.Adopt(req, "sess-1")

	got := l.LeaseOf("w1")
	if got == nil || got.Node != "gone" || !hasPool(got, "node:gone") {
		t.Fatalf("a session on a node outside the choices still occupies it, got %+v", got)
	}
}

func TestAdoptWithoutNodeLeavesLeaseNodeless(t *testing.T) {
	l := New(Options{PollInterval: nqPoll, Clock: newFakeClock()})
	l.Adopt(ncRequest("w1"), "sess-1")
	if got := l.LeaseOf("w1"); got == nil || got.Node != "" {
		t.Fatalf("no node known, lease should carry none, got %+v", got)
	}
}

func TestAdoptUpgradesNodelessLeaseWithNode(t *testing.T) {
	l := New(Options{PollInterval: nqPoll, Clock: newFakeClock()})
	l.Adopt(ncRequest("w1"), "sess-1")
	req := ncRequest("w1")
	req.Node = "b"
	l.Adopt(req, "sess-1")
	l.Adopt(req, "sess-1")

	got := l.LeaseOf("w1")
	if got == nil || got.Node != "b" || !hasPool(got, "node:b") {
		t.Fatalf("existing nodeless lease should gain node b, got %+v", got)
	}
	if n := len(l.Snapshot().Leases); n != 1 {
		t.Fatalf("adopt must stay idempotent, got %d leases", n)
	}
}

func TestAdoptedNodeLeaseBlocksThirdPlacementOnFullNode(t *testing.T) {
	l := New(Options{PollInterval: nqPoll, Clock: newFakeClock()})
	l.SetLimit("node:a", 1)
	l.SetLimit("node:b", 1)
	req := ncRequest("w1")
	req.Node = "b"
	l.Adopt(req, "sess-1")

	got := ncAcquire(t, l, ncRequest("w2"))
	if got.Node != "a" {
		t.Fatalf("node b is full from the adopted session, new work should go to a, got %q", got.Node)
	}
}
