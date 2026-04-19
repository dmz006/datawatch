// BL107 — ReadEvents tests.

package agents

import (
	"path/filepath"
	"testing"
	"time"
)

func writeAuditFixture(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "agents.jsonl")
	a, err := NewFileAuditor(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Append(AuditEvent{At: time.Now(), Event: "spawn", AgentID: "a1", Project: "alpha"})
	a.Append(AuditEvent{At: time.Now(), Event: "spawn_fail", AgentID: "a1", Project: "alpha", Note: "image pull"})
	a.Append(AuditEvent{At: time.Now(), Event: "spawn", AgentID: "a2", Project: "beta"})
	a.Append(AuditEvent{At: time.Now(), Event: "terminate", AgentID: "a1", Project: "alpha"})
	return path
}

func TestReadEvents_NoFilter_ReturnsAll(t *testing.T) {
	dir := t.TempDir()
	path := writeAuditFixture(t, dir)
	events, err := ReadEvents(path, ReadEventsFilter{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 {
		t.Errorf("got %d events, want 4", len(events))
	}
}

func TestReadEvents_FilterByEvent(t *testing.T) {
	dir := t.TempDir()
	path := writeAuditFixture(t, dir)
	events, err := ReadEvents(path, ReadEventsFilter{Event: "spawn"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Errorf("event=spawn returned %d, want 2", len(events))
	}
	for _, e := range events {
		if e.Event != "spawn" {
			t.Errorf("non-matching event in result: %q", e.Event)
		}
	}
}

func TestReadEvents_FilterByAgentID(t *testing.T) {
	dir := t.TempDir()
	path := writeAuditFixture(t, dir)
	events, _ := ReadEvents(path, ReadEventsFilter{AgentID: "a1"}, 0)
	if len(events) != 3 {
		t.Errorf("agent_id=a1 returned %d, want 3", len(events))
	}
}

func TestReadEvents_FilterByProject(t *testing.T) {
	dir := t.TempDir()
	path := writeAuditFixture(t, dir)
	events, _ := ReadEvents(path, ReadEventsFilter{Project: "beta"}, 0)
	if len(events) != 1 {
		t.Errorf("project=beta returned %d, want 1", len(events))
	}
}

func TestReadEvents_LimitTrimsToMostRecent(t *testing.T) {
	dir := t.TempDir()
	path := writeAuditFixture(t, dir)
	events, _ := ReadEvents(path, ReadEventsFilter{}, 2)
	if len(events) != 2 {
		t.Fatalf("limit=2 returned %d, want 2", len(events))
	}
	// Last two writes were spawn(a2) + terminate(a1).
	if events[0].AgentID != "a2" || events[1].Event != "terminate" {
		t.Errorf("limit returned wrong window: %+v", events)
	}
}

func TestReadEvents_MissingFile(t *testing.T) {
	_, err := ReadEvents(filepath.Join(t.TempDir(), "nope.jsonl"), ReadEventsFilter{}, 0)
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestReadEvents_EmptyPath(t *testing.T) {
	_, err := ReadEvents("", ReadEventsFilter{}, 0)
	if err == nil {
		t.Error("expected error for empty path")
	}
}
