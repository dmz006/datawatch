// Tests for the planner spawn hook on DecomposeRequest: decomposeAdmitted
// wires OnPlannerSpawned to RecordPlannerSession, so the PRD names its planner
// session while the planner is still running, and a failed spawn records
// nothing.

package autonomous

import (
	"errors"
	"path/filepath"
	"testing"
)

// TestPlannerRecord_DecomposeAdmittedRecordsSpawnBeforeReturn uses a fake
// planner that fires the spawn hook the way the one-shot reader does, then
// checks the record while the planner is still "running" (before it returns).
func TestPlannerRecord_DecomposeAdmittedRecordsSpawnBeforeReturn(t *testing.T) {
	dir := t.TempDir()
	projectDir := t.TempDir()
	outputFile := filepath.Join(projectDir, ".decompose-output-x.json")

	var m *Manager
	var prdID string
	fake := func(req DecomposeRequest) (string, error) {
		if req.OnPlannerSpawned == nil {
			t.Fatal("decomposeAdmitted did not wire OnPlannerSpawned")
		}
		req.OnPlannerSpawned("planner-sess-9", outputFile)
		got, ok := m.store.GetPRD(prdID)
		if !ok {
			t.Fatal("PRD missing while planner runs")
		}
		if got.PlannerSessionID != "planner-sess-9" {
			t.Errorf("PlannerSessionID during run = %q, want planner-sess-9", got.PlannerSessionID)
		}
		if got.PlannerOutputFile != ".decompose-output-x.json" {
			t.Errorf("PlannerOutputFile during run = %q, want .decompose-output-x.json", got.PlannerOutputFile)
		}
		return `{"stories":[]}`, nil
	}
	var err error
	m, err = NewManager(dir, DefaultConfig(), fake)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("plan me", projectDir, "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	prdID = prd.ID
	if _, err := m.decomposeAdmitted(prd, "claude-code", "m", DecomposeRequest{Spec: "plan me", ProjectDir: projectDir, PRDID: prd.ID}); err != nil {
		t.Fatalf("decomposeAdmitted: %v", err)
	}
}

// TestPlannerRecord_FailedDecomposeSpawnRecordsNothing covers a planner that
// fails to spawn: it never fires the hook, so the PRD keeps no planner fields.
func TestPlannerRecord_FailedDecomposeSpawnRecordsNothing(t *testing.T) {
	dir := t.TempDir()
	projectDir := t.TempDir()
	fake := func(req DecomposeRequest) (string, error) {
		return "", errors.New("session start refused")
	}
	m, err := NewManager(dir, DefaultConfig(), fake)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("plan me", projectDir, "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if _, err := m.decomposeAdmitted(prd, "claude-code", "m", DecomposeRequest{Spec: "plan me", ProjectDir: projectDir, PRDID: prd.ID}); err == nil {
		t.Fatal("want the spawn error back")
	}
	got, ok := m.store.GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD missing")
	}
	if got.PlannerSessionID != "" || got.PlannerOutputFile != "" {
		t.Errorf("failed spawn recorded planner fields: id=%q file=%q", got.PlannerSessionID, got.PlannerOutputFile)
	}
}
