package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// fakePeerSource is a fake federated peer source for the peer axis tests: it
// names the work it serves the way the peer placement build will — an
// explicit registered peer name per task, or "" when the work runs locally.
// It never infers a name from a node, DNS, an IP address or the hostname.
func fakePeerSource(named map[string]string) PeerSource {
	return func(taskID string) string { return named[taskID] }
}

// newPlacementTagManager wires a manager for the placement-tag hook the way
// the daemon does it: a system-tags sink plus the live tags.placement block.
func newPlacementTagManager(t *testing.T, sink SystemTagsSink, pc config.PlacementConfig) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.SetPlacementTagSink(sink)
	m.SetPlacementTagSettings(func() config.PlacementConfig { return pc })
	return m
}

// TestPlacementTags_FederatedPeer: work that is placed on a federated peer
// carries peer:<name>, and the name is the peer's explicit registered name —
// from the placement record (the field the addendum names) or, when the
// record leaves it unset, from the wired peer source. Either way the tag is
// peer:fake-federated-peer and rides alongside the other placement axes the
// same session already carries (compute, backend, lane, role, project).
func TestPlacementTags_FederatedPeer(t *testing.T) {
	peerName := "fake-federated-peer"
	node, _ := newFakeNodeChooser()
	pc := config.DefaultPlacementConfig()
	base := Placement{
		Backend:    "pi",
		Model:      "qwen3:8b",
		Lane:       "gpu",
		Role:       "worker",
		Project:    "datawatch",
		Automation: "prd1",
	}

	// Record path: the placing code names the peer on the placement record.
	store := &fakePlacementStore{}
	withPeer := base
	withPeer.Peer = peerName
	if err := ApplyPlacementTags(store, node, "task1", "sess1", withPeer, pc); err != nil {
		t.Fatalf("ApplyPlacementTags: %v", err)
	}
	tags := store.tags("sess1")
	if !store.has("sess1", "peer:"+peerName) {
		t.Fatalf("peer work should take peer:%s, got %v", peerName, tags)
	}
	// compute, backend, lane, role (the daemon's own project takes no
	// project tag by the no-tag rule, which the record above relies on).
	for _, axis := range []string{"compute:node-a", "backend:pi", "lane:gpu", "role:worker"} {
		if !store.has("sess1", axis) {
			t.Fatalf("peer tag must ride alongside the other placement axes; missing %s in %v", axis, tags)
		}
	}

	// Source path: the peer placement build names the peer per task, and the
	// hook writes that into the record before derivation.
	store2 := &fakePlacementStore{}
	src := fakePeerSource(map[string]string{"task2": peerName, "task-other": "somewhere-else"})
	m := newPlacementTagManager(t, store2, pc)
	m.SetPlacementPeerSource(src)
	m.TagTaskPlacement("task2", "sess2", base)
	tags2 := store2.tags("sess2")
	if !store2.has("sess2", "peer:"+peerName) {
		t.Fatalf("peer source naming %q should take peer tag; got %v", peerName, tags2)
	}
	if store2.has("sess2", "peer:somewhere-else") {
		t.Fatalf("the peer of another task must not leak into this placement: %v", tags2)
	}

	// Explicit record wins over the source — the placement record is the
	// source the addendum names, so an explicit field never loses to a
	// fallback that says something else.
	if got := PlacementPeer(Placement{Peer: "explicit-peer"}, func(string) string { return peerName }, "task3"); got != "explicit-peer" {
		t.Fatalf("explicit placement record must win over the peer source, got %q", got)
	}
	// A blank peer field is no peer, whatever the record's other fields say.
	if got := PlacementPeer(Placement{Node: "datawatch", Peer: "  "}, nil, "task4"); got != "" {
		t.Fatalf("a blank peer field is no peer, got %q", got)
	}
}

// TestPlacementTags_LocalWorkHasNoPeerTag: work placed on the daemon's own
// machine takes no peer tag, and the peer axis never borrows a name from a
// DNS, IP-address or hostname inference. The node here is the machine's own
// name ("datawatch") on purpose: a local placement's node must not be read
// as a peer, and the peer source is unwired (the placeholder) — so the only
// name anywhere in reach is a hostname-ish one to misuse, and there is no
// peer:<name> in the result.
func TestPlacementTags_LocalWorkHasNoPeerTag(t *testing.T) {
	node, _ := newFakeNodeChooser()
	pc := config.DefaultPlacementConfig()

	// Plain derivation: local record (no Peer field, node is the machine's
	// own name) yields a compute tag and no peer tag.
	store := &fakePlacementStore{}
	p := Placement{Node: "datawatch", Backend: "pi", Lane: "gpu", Role: "worker", Project: "datawatch"}
	if err := ApplyPlacementTags(store, node, "task5", "sess5", p, pc); err != nil {
		t.Fatalf("ApplyPlacementTags: %v", err)
	}
	if hasPeerTag(store.tags("sess5")) {
		t.Fatalf("local work must take no peer tag, got %v", store.tags("sess5"))
	}

	// Hook path, same guarantee: unwired peer source, unset record field.
	store2 := &fakePlacementStore{}
	m := newPlacementTagManager(t, store2, pc)
	m.TagTaskPlacement("task6", "sess6", Placement{Backend: "pi", Lane: "gpu", Role: "worker", Project: "datawatch"})
	if hasPeerTag(store2.tags("sess6")) {
		t.Fatalf("local work through an unwired peer source must take no peer tag, got %v", store2.tags("sess6"))
	}

	// A wired source that names no peer (local work per its contract) is
	// still local work.
	store3 := &fakePlacementStore{}
	m2 := newPlacementTagManager(t, store3, pc)
	m2.SetPlacementPeerSource(fakePeerSource(nil)) // names nothing for any task
	m2.TagTaskPlacement("task7", "sess7", basePlacement())
	if hasPeerTag(store3.tags("sess7")) {
		t.Fatalf("a peer source that names no peer is local work, got %v", store3.tags("sess7"))
	}
}

// basePlacement is a local worker placement for the peer axis tests.
func basePlacement() Placement {
	return Placement{
		Backend:    "pi",
		Lane:       "gpu",
		Role:       "worker",
		Project:    "datawatch",
		Automation: "prd1",
	}
}
