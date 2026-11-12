package autonomous

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/gitguard"
	"github.com/dmz006/datawatch/internal/session"
)

// TestTaskSnapshotSkippedFrom_Conversion — population from the session
// commit helper's skip list: order preserved, nil/empty stay nil (so the
// task field keeps omitempty), entries copied by value.
func TestTaskSnapshotSkippedFrom_Conversion(t *testing.T) {
	if got := TaskSnapshotSkippedFrom(nil); got != nil {
		t.Fatalf("nil input must stay nil, got %v", got)
	}
	if got := TaskSnapshotSkippedFrom([]session.StagingSkip{}); got != nil {
		t.Fatalf("empty input must stay nil, got %v", got)
	}
	in := []session.StagingSkip{
		{Path: "secrets/env", Reason: "never-stage"},
		{Path: "node_modules/x", Reason: "ignore-dir"},
		{Path: "scratch/tmp", Reason: "outside-plan"},
	}
	got := TaskSnapshotSkippedFrom(in)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3: %v", len(got), got)
	}
	want := []SnapshotSkippedPath{
		{Path: "secrets/env", Reason: "never-stage"},
		{Path: "node_modules/x", Reason: "ignore-dir"},
		{Path: "scratch/tmp", Reason: "outside-plan"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %+v, want %+v (order must be preserved)", i, got[i], want[i])
		}
	}
}

// TestTaskSnapshotSkippedFromGuard_Conversion — same contract for the
// attempt-snapshot path's skip list (gitguard.Skip, File field).
func TestTaskSnapshotSkippedFromGuard_Conversion(t *testing.T) {
	if got := TaskSnapshotSkippedFromGuard(nil); got != nil {
		t.Fatalf("nil input must stay nil, got %v", got)
	}
	in := []gitguard.Skip{
		{File: "dist/bundle.js", Reason: "ignore-dir"},
		{File: "model.bin", Reason: "size"},
	}
	got := TaskSnapshotSkippedFromGuard(in)
	if len(got) != 2 || got[0].Path != "dist/bundle.js" || got[0].Reason != "ignore-dir" ||
		got[1].Path != "model.bin" || got[1].Reason != "size" {
		t.Fatalf("guard conversion = %v", got)
	}
}

// TestStagingGuardReports_CountsOnly — the gate report is counts per reason
// only, alphabetically sorted, stable. It must never contain a path name:
// feed it paths and assert the output matches the counts exactly.
func TestStagingGuardReports_CountsOnly(t *testing.T) {
	if got := TaskSnapshotSkippedCounts(nil); got != "" {
		t.Fatalf("empty counts report must be \"\", got %q", got)
	}
	in := []session.StagingSkip{
		{Path: "a/path/one", Reason: "size"},
		{Path: "b/path/two", Reason: "ignored"},
		{Path: "c/path/three", Reason: "size"},
		{Path: "d/path/four", Reason: "never-stage"},
	}
	want := "ignored=1, never-stage=1, size=2"
	got := TaskSnapshotSkippedCounts(in)
	if got != want {
		t.Fatalf("counts report = %q, want %q", got, want)
	}
	for _, substring := range []string{"a/path", "b/path", "c/path", "d/path"} {
		if strings.Contains(got, substring) {
			t.Fatalf("counts report must not contain a path name, found %q in %q", substring, got)
		}
	}

	// Guard variant renders identically for the same reasons.
	gin := []gitguard.Skip{
		{File: "x/1", Reason: "size"},
		{File: "x/2", Reason: "ignored"},
		{File: "x/3", Reason: "size"},
		{File: "x/4", Reason: "never-stage"},
	}
	if got := TaskSnapshotSkippedGuardCounts(gin); got != want {
		t.Fatalf("guard counts report = %q, want %q", got, want)
	}
}

// TestRecordTaskSkippedSnapshotPaths_PostSpawnNoLock — daemon-internal hook
// (sibling of RecordTaskFilesTouched): fires after the worker session ends,
// records the last staging pass on the task result, persists it, caps at 50
// entries, and errors for an unknown task.
func TestRecordTaskSkippedSnapshotPaths_PostSpawnNoLock(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T1"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDRunning
	_ = m.Store().SavePRD(prd)
	taskID := prd.Story[0].Tasks[0].ID

	skipped := []SnapshotSkippedPath{
		{Path: "secrets/env", Reason: "never-stage"},
		{Path: "dist/out.js", Reason: "ignore-dir"},
	}
	if err := m.RecordTaskSkippedSnapshotPaths(prd.ID, taskID, skipped); err != nil {
		t.Fatalf("RecordTaskSkippedSnapshotPaths: %v", err)
	}
	out, _ := m.Store().GetPRD(prd.ID)
	got := out.Story[0].Tasks[0].SkippedSnapshotPaths
	if len(got) != 2 || got[0].Path != "secrets/env" || got[0].Reason != "never-stage" ||
		got[1].Path != "dist/out.js" || got[1].Reason != "ignore-dir" {
		t.Fatalf("SkippedSnapshotPaths not recorded: %+v", got)
	}

	// Last staging pass wins: replace the whole record, not append.
	if err := m.RecordTaskSkippedSnapshotPaths(prd.ID, taskID,
		[]SnapshotSkippedPath{{Path: "scratch/txt", Reason: "outside-plan"}}); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	out, _ = m.Store().GetPRD(prd.ID)
	got = out.Story[0].Tasks[0].SkippedSnapshotPaths
	if len(got) != 1 || got[0].Path != "scratch/txt" {
		t.Fatalf("last pass must replace, got %v", got)
	}

	// Empty input is a no-op (nothing to record).
	if err := m.RecordTaskSkippedSnapshotPaths(prd.ID, taskID, nil); err != nil {
		t.Fatalf("empty record: %v", err)
	}
	out, _ = m.Store().GetPRD(prd.ID)
	if got = out.Story[0].Tasks[0].SkippedSnapshotPaths; len(got) != 1 {
		t.Fatalf("empty record must be a no-op, got %v", got)
	}

	// 50-entry cap.
	big := make([]SnapshotSkippedPath, 80)
	for i := range big {
		big[i] = SnapshotSkippedPath{Path: "f" + string(rune('a'+i%26)) + string(rune('0'+i/26)), Reason: "size"}
	}
	if err := m.RecordTaskSkippedSnapshotPaths(prd.ID, taskID, big); err != nil {
		t.Fatalf("cap record: %v", err)
	}
	out, _ = m.Store().GetPRD(prd.ID)
	if got = out.Story[0].Tasks[0].SkippedSnapshotPaths; len(got) != 50 {
		t.Fatalf("expected 50-cap, got %d", len(got))
	}

	// Unknown task errors.
	if err := m.RecordTaskSkippedSnapshotPaths(prd.ID, "missing",
		[]SnapshotSkippedPath{{Path: "x", Reason: "size"}}); err == nil {
		t.Fatal("expected error for unknown task")
	}
}

// TestTaskSkippedSnapshotPaths_JSONShape — the raw JSON surface (REST read,
// PWA, MCP) carries the field: a Task with skips marshals to the
// skipped_snapshot_paths array of {path, reason}, and omits it entirely
// when nothing was skipped (omitempty).
func TestTaskSkippedSnapshotPaths_JSONShape(t *testing.T) {
	tk := Task{
		ID: "t1",
		SkippedSnapshotPaths: []SnapshotSkippedPath{
			{Path: "a.log", Reason: "ignored"},
			{Path: "b.tmp", Reason: "size"},
		},
	}
	b, err := json.Marshal(tk)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	skipped, ok := m["skipped_snapshot_paths"].([]any)
	if !ok || len(skipped) != 2 {
		t.Fatalf("skipped_snapshot_paths = %v, want 2 entries: %s", m["skipped_snapshot_paths"], b)
	}
	first, _ := skipped[0].(map[string]any)
	if first["path"] != "a.log" || first["reason"] != "ignored" {
		t.Fatalf("first entry = %v, want {path:a.log reason:ignored}", first)
	}

	empty := Task{ID: "t2"}
	eb, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(eb), "skipped_snapshot_paths") {
		t.Fatalf("empty task must omit the field: %s", eb)
	}
}
