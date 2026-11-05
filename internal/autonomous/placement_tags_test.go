package autonomous

import (
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// fakePlacementStore is the fake session store for placement-tag tests: it
// carries both lists a session owns — the operator tags (Tags, the
// machine-managed path must never touch them) and the system tags the sink
// replaces on every placement.
type fakePlacementStore struct {
	mu         sync.Mutex
	operator   map[string][]string
	systemTags map[string][]string
}

func (f *fakePlacementStore) SetSystemTags(sessionID string, tags []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.systemTags == nil {
		f.systemTags = map[string][]string{}
	}
	f.systemTags[sessionID] = append([]string(nil), tags...)
	return nil
}

// setOperator records the operator-selected tags on a session, the way the
// session store does in production. Placement code must never write here.
func (f *fakePlacementStore) setOperator(sessionID string, tags []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.operator == nil {
		f.operator = map[string][]string{}
	}
	f.operator[sessionID] = append([]string(nil), tags...)
}

func (f *fakePlacementStore) tags(sessionID string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.systemTags[sessionID]))
	copy(out, f.systemTags[sessionID])
	return out
}

func (f *fakePlacementStore) has(sessionID, tag string) bool {
	for _, t := range f.tags(sessionID) {
		if t == tag {
			return true
		}
	}
	return false
}

func (f *fakePlacementStore) operatorOf(sessionID string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.operator[sessionID]))
	copy(out, f.operator[sessionID])
	return out
}

// newFakeNodeChooser returns a fake node chooser: the capacity ledger's
// heldNode stand-in. Each test moves the pointer to change where the task
// is "placed", simulating a retry on another node.
func newFakeNodeChooser() (func(taskID string) string, *string) {
	chosen := "node-a"
	return func(taskID string) string { return chosen }, &chosen
}

func hasPeerTag(tags []string) bool {
	for _, t := range tags {
		if strings.HasPrefix(t, "peer:") {
			return true
		}
	}
	return false
}

// TestPlacementTags_LocalNode: a task that lands on a local node takes the
// compute tag for that node — resolved through the fake node chooser — and
// no peer tag, because the work runs on the daemon's own machine. The
// backend, lane, role and project axes ride along by default.
func TestPlacementTags_LocalNode(t *testing.T) {
	store := &fakePlacementStore{}
	node, _ := newFakeNodeChooser()
	pc := config.DefaultPlacementConfig()
	p := Placement{
		Backend:    "pi",
		Model:      "qwen3:8b",
		Lane:       "gpu",
		Role:       "worker",
		Project:    "datawatch",
		Automation: "prd1",
	}
	if err := ApplyPlacementTags(store, node, "task1", "sess1", p, pc); err != nil {
		t.Fatalf("ApplyPlacementTags: %v", err)
	}
	tags := store.tags("sess1")
	for _, want := range []string{"compute:node-a", "backend:pi", "lane:gpu", "role:worker"} {
		if !store.has("sess1", want) {
			t.Fatalf("placement tag %q missing; got %q", want, tags)
		}
	}
	if hasPeerTag(tags) {
		t.Fatalf("local work must not yield a peer tag, got %q", tags)
	}
	// The project axis is off by default (config.DefaultPlacementConfig); when
	// the operator turns it on, the tag follows the placement.
	projPC := pc
	projPC.Project.Enabled = true
	if got := DerivePlacementTags(p, projPC); !listContains(got, "project:datawatch") {
		t.Fatalf("project tag missing once the axis is enabled; got %q", got)
	}
}

func listContains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// TestPlacementTags_RetryOnAnotherNode: a retry that lands the task on
// another node re-tags the same session. SetSystemTags replaces the whole
// list, so the old node's compute tag (and the old peer tag, when the
// first placement carried one) must be gone; only the new placement's tags
// remain.
func TestPlacementTags_RetryOnAnotherNode(t *testing.T) {
	store := &fakePlacementStore{}
	node, chosen := newFakeNodeChooser()
	pc := config.DefaultPlacementConfig()

	// First placement: node-a, on a remote peer.
	first := Placement{
		Node:  "",
		Peer:  "peerA",
		Backend: "claude",
		Lane:  "claude",
		Role:  "worker",
		Project: "datawatch",
	}
	if err := ApplyPlacementTags(store, node, "task1", "sess1", first, pc); err != nil {
		t.Fatalf("first placement: %v", err)
	}
	if !store.has("sess1", "compute:node-a") || !store.has("sess1", "peer:peerA") {
		t.Fatalf("first placement missing tags; got %q", store.tags("sess1"))
	}

	// Retry: the ledger now holds node-b; the peer fact is cleared.
	*chosen = "node-b"
	second := first
	second.Peer = ""
	if err := ApplyPlacementTags(store, node, "task1", "sess1", second, pc); err != nil {
		t.Fatalf("retry placement: %v", err)
	}
	tags := store.tags("sess1")
	if !store.has("sess1", "compute:node-b") {
		t.Fatalf("new node's compute tag missing; got %q", tags)
	}
	if store.has("sess1", "compute:node-a") {
		t.Fatalf("old node's compute tag must be gone after a retry; got %q", tags)
	}
	if hasPeerTag(tags) {
		t.Fatalf("old peer tag must be gone after a retry; got %q", tags)
	}
	// Exactly one compute tag total.
	n := 0
	for _, tag := range tags {
		if strings.HasPrefix(tag, "compute:") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expected exactly one compute tag after a retry, got %q", tags)
	}
}

// TestPlacementTags_OperatorTagsSurviveRetag: operator tags live in a
// different list (Session.Tags, here the store's operator map) from system
// tags (Session.SystemTags). Re-tagging a session for a new placement must
// leave the operator's own tags untouched.
func TestPlacementTags_OperatorTagsSurviveRetag(t *testing.T) {
	store := &fakePlacementStore{}
	store.setOperator("sess1", []string{"bug", "p1"})
	node, chosen := newFakeNodeChooser()
	pc := config.DefaultPlacementConfig()
	p := Placement{Backend: "pi", Lane: "gpu", Role: "worker", Project: "datawatch"}

	for i := 0; i < 2; i++ {
		if err := ApplyPlacementTags(store, node, "task1", "sess1", p, pc); err != nil {
			t.Fatalf("placement %d: %v", i, err)
		}
		if i == 1 {
			*chosen = "node-b"
		}
	}
	got := store.operatorOf("sess1")
	if len(got) != 2 || got[0] != "bug" || got[1] != "p1" {
		t.Fatalf("operator tags must survive a re-tag, got %q", got)
	}
}

// TestPlacementTags_MasterOffClears: with the master switch off the
// placement tags are not written — and any system tags a placement wrote
// earlier are removed, so flipping the switch off cleans the session up.
func TestPlacementTags_MasterOffClears(t *testing.T) {
	store := &fakePlacementStore{}
	node, _ := newFakeNodeChooser()
	pc := config.DefaultPlacementConfig()
	p := Placement{Backend: "pi", Lane: "gpu", Role: "worker", Project: "datawatch"}

	if err := ApplyPlacementTags(store, node, "task1", "sess1", p, pc); err != nil {
		t.Fatalf("placement: %v", err)
	}
	if len(store.tags("sess1")) == 0 {
		t.Fatalf("expected placement tags with the master switch on")
	}

	off := pc
	off.Enabled = false
	if got := DerivePlacementTags(p, off); len(got) != 0 {
		t.Fatalf("master switch off must derive no tags, got %q", got)
	}
	if err := ApplyPlacementTags(store, node, "task1", "sess1", p, off); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := store.tags("sess1"); len(got) != 0 {
		t.Fatalf("master switch off must clear the session's system tags, got %q", got)
	}
}
