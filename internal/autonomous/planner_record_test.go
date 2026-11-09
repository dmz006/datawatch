// Tests for the planner session record: the PRD fields that name the live
// planner session and its output file, so a restart can re-attach to a plan.

package autonomous

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlannerRecord_FieldsRoundTripThroughStore checks that the planner fields
// survive a store save and a reload from disk, and that they use the
// documented JSON keys.
func TestPlannerRecord_FieldsRoundTripThroughStore(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("plan me", "/proj", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	m.RecordPlannerSession(prd.ID, "sess-123", "/proj/.datawatch/plan.md")

	// Reopen the store from disk: the fields must come back, not just live in memory.
	st, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore reopen: %v", err)
	}
	got, ok := st.GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD missing after reload")
	}
	if got.PlannerSessionID != "sess-123" {
		t.Errorf("PlannerSessionID after reload = %q, want sess-123", got.PlannerSessionID)
	}
	if got.PlannerOutputFile != ".datawatch/plan.md" {
		t.Errorf("PlannerOutputFile after reload = %q, want .datawatch/plan.md", got.PlannerOutputFile)
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, key := range []string{`"planner_session_id":"sess-123"`, `"planner_output_file":".datawatch/plan.md"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("JSON missing %s", key)
		}
	}
}

// TestPlannerRecord_RecordThenClear checks that Clear empties both fields
// after Record set them, and that a second Clear is a harmless no-op.
func TestPlannerRecord_RecordThenClear(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("plan me", "/proj", "", "", "")

	m.RecordPlannerSession(prd.ID, "sess-abc", "/elsewhere/plan.md")
	got, _ := m.Store().GetPRD(prd.ID)
	if got.PlannerSessionID != "sess-abc" || got.PlannerOutputFile != "/elsewhere/plan.md" {
		t.Fatalf("after Record: session=%q file=%q", got.PlannerSessionID, got.PlannerOutputFile)
	}

	m.ClearPlannerSession(prd.ID)
	got, _ = m.Store().GetPRD(prd.ID)
	if got.PlannerSessionID != "" || got.PlannerOutputFile != "" {
		t.Errorf("after Clear: session=%q file=%q, want both empty", got.PlannerSessionID, got.PlannerOutputFile)
	}

	m.ClearPlannerSession(prd.ID) // second clear must not panic or re-set anything
	got, _ = m.Store().GetPRD(prd.ID)
	if got.PlannerSessionID != "" || got.PlannerOutputFile != "" {
		t.Errorf("after second Clear: session=%q file=%q, want both empty", got.PlannerSessionID, got.PlannerOutputFile)
	}
}

// TestPlannerRecord_UnknownIDIgnored checks that Record and Clear on an id the
// store does not hold neither panic nor create or alter any PRD.
func TestPlannerRecord_UnknownIDIgnored(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	known, _ := m.CreatePRD("known", "/proj", "", "", "")

	m.RecordPlannerSession("no-such-prd", "sess-x", "/proj/plan.md")
	m.ClearPlannerSession("no-such-prd")

	if _, ok := m.Store().GetPRD("no-such-prd"); ok {
		t.Error("unknown id was created by RecordPlannerSession")
	}
	got, _ := m.Store().GetPRD(known.ID)
	if got.PlannerSessionID != "" || got.PlannerOutputFile != "" {
		t.Errorf("unrelated PRD was altered: session=%q file=%q", got.PlannerSessionID, got.PlannerOutputFile)
	}
}

// TestPlannerRecord_OutputPathRelativeUnderProject checks the path rule: a file
// under the project directory is stored relative to it, while a file outside
// it, or a relative path already given, is stored as recorded.
func TestPlannerRecord_OutputPathRelativeUnderProject(t *testing.T) {
	proj := filepath.Join(t.TempDir(), "proj")
	cases := []struct {
		name string
		file string
		want string
	}{
		{"under project", filepath.Join(proj, "docs", "plan.md"), filepath.Join("docs", "plan.md")},
		{"outside project", filepath.Join(t.TempDir(), "plan.md"), ""}, // filled in below
		{"already relative", "docs/plan.md", "docs/plan.md"},
		{"project dir itself", proj, proj},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.want
			if tc.name == "outside project" {
				want = tc.file
			}
			m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
			if err != nil {
				t.Fatalf("NewManager: %v", err)
			}
			prd, _ := m.CreatePRD("plan me", proj, "", "", "")
			m.RecordPlannerSession(prd.ID, "sess", tc.file)
			got, _ := m.Store().GetPRD(prd.ID)
			if got.PlannerOutputFile != want {
				t.Errorf("PlannerOutputFile = %q, want %q", got.PlannerOutputFile, want)
			}
		})
	}
}

// TestPlannerRecord_DecomposeFailureClears checks that a planner session
// recorded on a PRD is cleared when its decompose fails, so a failed plan
// never leaves a stale session for a restart to re-attach to.
func TestPlannerRecord_DecomposeFailureClears(t *testing.T) {
	fn := func(req DecomposeRequest) (string, error) {
		return "", errors.New("planner exploded")
	}
	m, err := NewManager(t.TempDir(), DefaultConfig(), fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("decompose me", "/proj", "", "", "")
	m.RecordPlannerSession(prd.ID, "sess-fail", "/proj/plan.md")

	if _, err := m.Decompose(prd.ID); err == nil {
		t.Fatal("expected Decompose to fail")
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.PlannerSessionID != "" || got.PlannerOutputFile != "" {
		t.Errorf("after failed decompose: session=%q file=%q, want both empty", got.PlannerSessionID, got.PlannerOutputFile)
	}
}
