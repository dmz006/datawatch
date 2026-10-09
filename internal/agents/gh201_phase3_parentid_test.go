// GH#201 Phase 3 — chained-children (ParentAgentID) in the agent
// audit trail. Agent.ParentAgentID was already tracked on the agent
// instance (spawn-chain enforcement, container labels); it just never
// made it into AuditEvent itself. This threads it through as a
// first-class field on every emitter, not just "spawn"'s old
// Extra-only copy.

package agents

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/profile"
)

func gh201Phase3Setup(t *testing.T) (*Manager, *MemoryAuditor) {
	t.Helper()
	dir := t.TempDir()
	ps, _ := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	cs, _ := profile.NewClusterStore(filepath.Join(dir, "c.json"))
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair:            profile.ImagePair{Agent: "agent-claude"},
		Memory:               profile.MemorySpec{Mode: profile.MemorySyncBack},
		AllowSpawnChildren:   true,
		SpawnBudgetTotal:     10,
		SpawnBudgetPerMinute: 10,
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})

	m := NewManager(ps, cs)
	m.RegisterDriver(&fakeDriver{kind: "docker"})
	aud := NewMemoryAuditor()
	m.Auditor = aud
	return m, aud
}

// TestGH201Phase3_SpawnTerminateResult_CarryParentAgentID spawns a
// parent, then a real recursive child (exercising the actual
// recursion-budget gate, not a hand-built struct), and confirms every
// lifecycle event for the CHILD carries ParentAgentID as a first-class
// field — not buried in Extra, and not just on the "spawn" event.
func TestGH201Phase3_SpawnTerminateResult_CarryParentAgentID(t *testing.T) {
	m, aud := gh201Phase3Setup(t)

	parent, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})
	if err != nil {
		t.Fatalf("spawn parent: %v", err)
	}

	child, err := m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", ParentAgentID: parent.ID, Branch: "child-branch",
	})
	if err != nil {
		t.Fatalf("spawn child: %v", err)
	}
	if child.ParentAgentID != parent.ID {
		t.Fatalf("child.ParentAgentID = %q, want %q", child.ParentAgentID, parent.ID)
	}

	if err := m.RecordResult(child.ID, &AgentResult{Status: "ok", Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Terminate(context.Background(), child.ID); err != nil {
		t.Fatal(err)
	}

	var sawSpawn, sawResult, sawTerminate bool
	for _, ev := range aud.All() {
		if ev.AgentID != child.ID {
			continue
		}
		if ev.ParentAgentID != parent.ID {
			t.Errorf("event %q: ParentAgentID = %q, want %q", ev.Event, ev.ParentAgentID, parent.ID)
		}
		switch ev.Event {
		case "spawn":
			sawSpawn = true
			if _, ok := ev.Extra["parent_agent_id"]; ok {
				t.Error("spawn event should no longer duplicate parent_agent_id into Extra now it's a first-class field")
			}
		case "result":
			sawResult = true
		case "terminate":
			sawTerminate = true
		}
	}
	if !sawSpawn || !sawResult || !sawTerminate {
		t.Fatalf("missing expected events: spawn=%v result=%v terminate=%v; all=%+v", sawSpawn, sawResult, sawTerminate, aud.All())
	}
}

// TestGH201Phase3_TopLevelSpawn_EmptyParentAgentID confirms an
// operator-initiated top-level spawn (no ParentAgentID) still emits
// cleanly with an empty ParentAgentID — this phase must not force
// every spawn to look recursive.
func TestGH201Phase3_TopLevelSpawn_EmptyParentAgentID(t *testing.T) {
	m, aud := gh201Phase3Setup(t)
	a, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range aud.All() {
		if ev.AgentID == a.ID && ev.ParentAgentID != "" {
			t.Errorf("top-level spawn event has non-empty ParentAgentID: %+v", ev)
		}
	}
}

// TestGH201Phase3_IdleReap_CarriesParentAgentID exercises the one
// emitter whose source data doesn't come straight from an *Agent at
// the emit call site (ReapIdle builds a local "victim" struct first).
func TestGH201Phase3_IdleReap_CarriesParentAgentID(t *testing.T) {
	m, aud := gh201Phase3Setup(t)
	parent, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})
	if err != nil {
		t.Fatalf("spawn parent: %v", err)
	}
	child, err := m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", ParentAgentID: parent.ID, Branch: "child-branch",
	})
	if err != nil {
		t.Fatalf("spawn child: %v", err)
	}

	m.mu.Lock()
	m.agents[child.ID].project.IdleTimeout = time.Millisecond
	m.agents[child.ID].LastActivityAt = time.Now().UTC().Add(-time.Hour)
	m.mu.Unlock()

	reaped := m.ReapIdle(context.Background(), time.Now().UTC())
	if len(reaped) != 1 || reaped[0] != child.ID {
		t.Fatalf("ReapIdle reaped = %+v, want [%s]", reaped, child.ID)
	}

	var sawIdleReap bool
	for _, ev := range aud.All() {
		if ev.Event == "idle_reap" && ev.AgentID == child.ID {
			sawIdleReap = true
			if ev.ParentAgentID != parent.ID {
				t.Errorf("idle_reap ParentAgentID = %q, want %q", ev.ParentAgentID, parent.ID)
			}
		}
	}
	if !sawIdleReap {
		t.Fatalf("no idle_reap event recorded; all=%+v", aud.All())
	}
}

// TestGH201Phase3_ReadEventsFilter_ByParentAgentID confirms the full
// spawn chain for one parent can be queried directly, without parsing
// Extra's loosely-typed map.
func TestGH201Phase3_ReadEventsFilter_ByParentAgentID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents.jsonl")
	fa, err := NewFileAuditor(path)
	if err != nil {
		t.Fatal(err)
	}
	fa.Append(AuditEvent{Event: "spawn", AgentID: "child-1", ParentAgentID: "parent-x"})
	fa.Append(AuditEvent{Event: "spawn", AgentID: "child-2", ParentAgentID: "parent-y"})
	fa.Append(AuditEvent{Event: "terminate", AgentID: "child-1", ParentAgentID: "parent-x"})
	_ = fa.Close()

	got, err := ReadEvents(path, ReadEventsFilter{ParentAgentID: "parent-x"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("filtered events = %d, want 2: %+v", len(got), got)
	}
	for _, ev := range got {
		if ev.ParentAgentID != "parent-x" {
			t.Errorf("leaked event for a different parent: %+v", ev)
		}
	}
}

// TestGH201Phase3_CEF_IncludesParentAgentIDWhenSet confirms the CEF
// mirror surfaces ParentAgentID as a labeled extension field, and
// omits it cleanly when empty (no "deviceCustomString5=" with a blank
// value cluttering top-level spawns).
func TestGH201Phase3_CEF_IncludesParentAgentIDWhenSet(t *testing.T) {
	withParent := FormatCEFLine(AuditEvent{Event: "spawn", AgentID: "c1", ParentAgentID: "p1"})
	if !strings.Contains(withParent, "deviceCustomString5Label=parent_agent_id") || !strings.Contains(withParent, "deviceCustomString5=p1") {
		t.Errorf("CEF line missing parent_agent_id extension: %s", withParent)
	}

	withoutParent := FormatCEFLine(AuditEvent{Event: "spawn", AgentID: "c1"})
	if strings.Contains(withoutParent, "parent_agent_id") {
		t.Errorf("CEF line for a top-level spawn should not mention parent_agent_id at all: %s", withoutParent)
	}
}

// TestGH201Phase3_AuditEvent_JSONRoundTrip confirms the new field
// marshals/unmarshals correctly and omits cleanly when empty (not
// `"parent_agent_id":""` cluttering every top-level-spawn log line).
func TestGH201Phase3_AuditEvent_JSONRoundTrip(t *testing.T) {
	ev := AuditEvent{Event: "spawn", AgentID: "c1", ParentAgentID: "p1"}
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"parent_agent_id":"p1"`) {
		t.Errorf("marshaled JSON missing parent_agent_id: %s", raw)
	}

	topLevel := AuditEvent{Event: "spawn", AgentID: "c1"}
	raw2, err := json.Marshal(topLevel)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw2), "parent_agent_id") {
		t.Errorf("empty ParentAgentID should omitempty, got: %s", raw2)
	}

	var back AuditEvent
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.ParentAgentID != "p1" {
		t.Errorf("round trip lost ParentAgentID: %+v", back)
	}
}
