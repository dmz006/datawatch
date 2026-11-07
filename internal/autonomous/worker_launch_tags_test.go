package autonomous

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// TestWorkerLaunchPlacementTags: a task worker launch tags the session with
// the launch placement facts — the Automaton it runs under, the role it
// plays, the kind of work — on top of the axes the placement already carried
// (compute, backend, lane, project), and the stage when the Automaton is
// stage-typed (a member of an Automation whose slot carries a stage label).
func TestWorkerLaunchPlacementTags(t *testing.T) {
	store := &fakePlacementStore{}
	pc := config.DefaultPlacementConfig()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.SetPlacementTagSink(store)
	m.SetPlacementTagSettings(func() config.PlacementConfig { return pc })

	prd, err := m.CreatePRD("tag the worker launch", "", "pi", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	auto, err := m.CreateAutomation(AutomationInput{
		Name:    "tag arc",
		Members: []AutomationMember{{PRDID: prd.ID, Stage: "build"}},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	if len(auto.Members) != 1 {
		t.Fatalf("expected one member, got %d", len(auto.Members))
	}
	// Membership stamped the store's copy with AutomationID; the local copy
	// is stale. Re-read it the way the executor's loop holds a fresh PRD.
	fresh, ok := m.store.GetPRD(prd.ID)
	if !ok {
		t.Fatal("re-read PRD not found")
	}
	prd = fresh
	if prd.AutomationID != auto.ID {
		t.Fatalf("membership must stamp the PRD's AutomationID, got %q", prd.AutomationID)
	}

	// The launch the executor performs: one spawn, then one tag write with
	// the worker placement facts.
	placedNode := "node-a"
	m.TagTaskPlacement("task1", "sess1", m.workerLaunchPlacement(prd, placedNode, "pi", "qwen3:8b", "datawatch"))
	tags := store.tags("sess1")

	// Every fact the launch carries takes its axis tag: the Automaton, the
	// worker role, the task kind, the automation context (including the
	// Automaton's stage in its Automation), plus the compute/backend/lane
	// and project axes.
	for _, want := range []string{
		"automaton:" + prd.ID,
		"role:worker",
		"kind:task",
		"automation:" + auto.ID,
		"stage:build",
		"backend:pi",
		"lane:gpu",
		"compute:" + placedNode,
		"project:datawatch",
	} {
		if !store.has("sess1", want) {
			t.Fatalf("worker launch must write %q; got %v", want, tags)
		}
	}

	// A retry on another node re-tags the same session with the same two
	// facts (Automaton, stage) — and the node the retry landed on — so the
	// old node's compute tag is gone (SetSystemTags replaces the list).
	m.TagTaskPlacement("task1", "sess1", m.workerLaunchPlacement(prd, "node-b", "pi", "qwen3:8b", "datawatch"))
	tags = store.tags("sess1")
	if !store.has("sess1", "compute:node-b") {
		t.Fatalf("retry placement must carry the new node's compute tag; got %v", tags)
	}
	if store.has("sess1", "compute:"+placedNode) {
		t.Fatalf("retry placement must drop the old node's compute tag; got %v", tags)
	}
	for _, want := range []string{"automaton:" + prd.ID, "role:worker", "kind:task", "stage:build"} {
		if !store.has("sess1", want) {
			t.Fatalf("retry must re-tag the same Automaton/stage/role/kind facts; %q missing from %v", want, tags)
		}
	}
}

// TestWorkerLaunchPlacement_NoStageWhenNotStageTyped: a standalone Automaton
// (no Automation, no member slot) is not stage-typed, so the launch takes no
// stage tag; the Automaton, role, automation-context and kind tags are all
// still written. A member whose slot carries no stage label is the same: no
// stage tag.
func TestWorkerLaunchPlacement_NoStageWhenNotStageTyped(t *testing.T) {
	// Standalone Automaton.
	store := &fakePlacementStore{}
	pc := config.DefaultPlacementConfig()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.SetPlacementTagSink(store)
	m.SetPlacementTagSettings(func() config.PlacementConfig { return pc })

	prd, err := m.CreatePRD("standalone worker", "", "pi", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	auto, err := m.CreateAutomation(AutomationInput{
		Name:    "tag arc",
		Members: []AutomationMember{{PRDID: prd.ID}}, // no Stage label
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	if got := m.StageOfPRD(prd); got != "" {
		t.Fatalf("a member slot with no Stage label is not stage-typed, got %q", got)
	}
	fresh, ok := m.store.GetPRD(prd.ID)
	if !ok {
		t.Fatal("re-read PRD not found")
	}
	prd = fresh
	m.TagTaskPlacement("task1", "sess1", m.workerLaunchPlacement(prd, "node-a", "pi", "qwen3:8b", "datawatch"))
	tags := store.tags("sess1")
	for _, other := range []string{"automaton:" + prd.ID, "role:worker", "kind:task", "automation:" + auto.ID} {
		if !store.has("sess1", other) {
			t.Fatalf("missing %q; got %v", other, tags)
		}
	}
	if hasStageTag(tags) {
		t.Fatalf("a non stage-typed Automaton must take no stage tag; got %v", tags)
	}

	// Standalone: no Automation at all.
	m2, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	store2 := &fakePlacementStore{}
	m2.SetPlacementTagSink(store2)
	m2.SetPlacementTagSettings(func() config.PlacementConfig { return pc })
	prd2, err := m2.CreatePRD("standalone worker", "", "pi", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if got := m2.StageOfPRD(prd2); got != "" {
		t.Fatalf("a standalone Automaton is not stage-typed, got %q", got)
	}
	m2.TagTaskPlacement("task2", "sess2", m2.workerLaunchPlacement(prd2, "node-a", "pi", "qwen3:8b", "datawatch"))
	tags2 := store2.tags("sess2")
	for _, other := range []string{"automaton:" + prd2.ID, "role:worker", "kind:task"} {
		if !store2.has("sess2", other) {
			t.Fatalf("missing %q; got %v", other, tags2)
		}
	}
	if hasStageTag(tags2) {
		t.Fatalf("a standalone Automaton must take no stage tag; got %v", tags2)
	}
}

func hasStageTag(tags []string) bool {
	for _, t := range tags {
		if strings.HasPrefix(t, "stage:") && t != "stage:" {
			return true
		}
	}
	return false
}
