package capacity

import (
	"strings"
	"testing"
)

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

func TestAdoptUnknownNodeFlagsLease(t *testing.T) {
	l := New(Options{PollInterval: nqPoll, Clock: newFakeClock()})
	req := ncRequest("w1")
	req.NodeUnknown = true
	l.Adopt(req, "sess-1")
	l.Adopt(req, "sess-1")

	got := l.LeaseOf("w1")
	if got == nil || got.Node != "" || !got.NodeUnknown || hasPool(got, "node:a") || hasPool(got, "node:b") {
		t.Fatalf("an unresolved node should leave a flagged lease holding no node pool, got %+v", got)
	}
	st := l.Snapshot()
	if n := len(st.Leases); n != 1 || !st.Leases[0].NodeUnknown {
		t.Fatalf("snapshot should carry one flagged lease, got %+v", st.Leases)
	}
	for _, p := range st.Pools {
		if p.Name == "host" && p.Held != 1 {
			t.Fatalf("a flagged lease still counts against host, got %+v", p)
		}
		if len(p.Name) > 5 && p.Name[:5] == "node:" && p.Held != 0 {
			t.Fatalf("a flagged lease must hold no node pool, got %+v", p)
		}
	}

	// A nodeless lease the request does not call unknown stays unflagged, and a
	// request that names a node never flags.
	other := ncRequest("w2")
	l.Adopt(other, "sess-2")
	if got := l.LeaseOf("w2"); got == nil || got.NodeUnknown {
		t.Fatalf("a nodeless lease that was not flagged must stay unflagged, got %+v", got)
	}
	named := ncRequest("w3")
	named.Node, named.NodeUnknown = "a", true
	l.Adopt(named, "sess-3")
	if got := l.LeaseOf("w3"); got == nil || got.NodeUnknown || got.Node != "a" {
		t.Fatalf("a lease with a node is never flagged, got %+v", got)
	}

	// A later Adopt that flags an existing nodeless lease marks it.
	late := ncRequest("w2")
	late.NodeUnknown = true
	l.Adopt(late, "sess-2")
	if got := l.LeaseOf("w2"); got == nil || !got.NodeUnknown {
		t.Fatalf("a later flagged adopt should mark the nodeless lease, got %+v", got)
	}
}

func TestAdoptNodeClearsUnknownFlag(t *testing.T) {
	l := New(Options{PollInterval: nqPoll, Clock: newFakeClock()})
	req := ncRequest("w1")
	req.NodeUnknown = true
	l.Adopt(req, "sess-1")

	req.Node = "b"
	l.Adopt(req, "sess-1")
	l.Adopt(req, "sess-1")

	got := l.LeaseOf("w1")
	if got == nil || got.Node != "b" || got.NodeUnknown || !hasPool(got, "node:b") {
		t.Fatalf("supplying the node should clear the flag and add its pool, got %+v", got)
	}
	if n := len(l.Snapshot().Leases); n != 1 {
		t.Fatalf("adopt must stay idempotent, got %d leases", n)
	}
	// Once the node is known a later unknown request cannot re-flag it.
	req.Node = ""
	l.Adopt(req, "sess-1")
	if got := l.LeaseOf("w1"); got == nil || got.NodeUnknown || got.Node != "b" {
		t.Fatalf("a lease holding a node must not be re-flagged, got %+v", got)
	}
}

func TestFormatTextCountsUnknownNodeLeases(t *testing.T) {
	l := New(Options{PollInterval: nqPoll, Clock: newFakeClock()})
	if strings.Contains(FormatText(l.Snapshot()), "unknown node") {
		t.Fatal("no unknown lease, no line")
	}
	req := ncRequest("w1")
	req.NodeUnknown = true
	l.Adopt(req, "sess-1")
	if txt := FormatText(l.Snapshot()); !strings.Contains(txt, "Leases with unknown node: 1") {
		t.Fatalf("status text should count the flagged lease:\n%s", txt)
	}
}
