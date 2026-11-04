package autonomous

import (
	"strings"
	"testing"
)

func taskReportPath(t *testing.T, mgr *Manager, prdID, taskID string) (bool, string) {
	t.Helper()
	prd, _ := mgr.Store().GetPRD(prdID)
	for _, s := range prd.Story {
		for _, tk := range s.Tasks {
			if tk.ID == taskID {
				return tk.ReadOnly, tk.ReportPath
			}
		}
	}
	t.Fatalf("task %q not found", taskID)
	return false, ""
}

func TestEditTaskSetsReportPathOnReadOnlyTask(t *testing.T) {
	mgr, id := newReadOnlyFlagManager(t, PRDNeedsReview)
	const rp = ".datawatch/reports/gate.md"
	// Flag the task read_only, then add the path.
	if _, err := mgr.EditTask(id, "t1", "", boolPtr(true), "", "operator"); err != nil {
		t.Fatalf("flag t1 read_only: %v", err)
	}
	if _, err := mgr.EditTask(id, "t1", "", nil, rp, "operator"); err != nil {
		t.Fatalf("EditTask report_path on read_only task: %v", err)
	}
	ro, got := taskReportPath(t, mgr, id, "t1")
	if !ro || got != rp {
		t.Fatalf("read_only=%v report_path=%q, want true and %q", ro, got, rp)
	}
	// Clearing the read_only flag keeps the stored path.
	if _, err := mgr.EditTask(id, "t1", "", boolPtr(false), "", "operator"); err != nil {
		t.Fatalf("clearing read_only: %v", err)
	}
	ro, got = taskReportPath(t, mgr, id, "t1")
	if ro || got != rp {
		t.Fatalf("after clear: read_only=%v report_path=%q, want false and the path kept", ro, got)
	}
	if _, err := mgr.EditTask(id, "t1", "", boolPtr(true), "", "operator"); err != nil {
		t.Fatalf("re-flagging read_only: %v", err)
	}
}

func TestEditTaskReportPathSameCallAsReadOnlyFlag(t *testing.T) {
	mgr, id := newReadOnlyFlagManager(t, PRDNeedsReview)
	const rp = ".datawatch/reports/audit.md"
	// The task starts as a writer; flagging it read_only in the same call
	// makes the path acceptable.
	if _, err := mgr.EditTask(id, "t1", "", boolPtr(true), rp, "operator"); err != nil {
		t.Fatalf("EditTask read_only+report_path together: %v", err)
	}
	ro, got := taskReportPath(t, mgr, id, "t1")
	if !ro || got != rp {
		t.Fatalf("read_only=%v report_path=%q, want true and %q", ro, got, rp)
	}
}

func TestEditTaskRejectsReportPathOnWriterTask(t *testing.T) {
	mgr, id := newReadOnlyFlagManager(t, PRDNeedsReview)
	const rp = ".datawatch/reports/gate.md"
	if _, err := mgr.EditTask(id, "t1", "", nil, rp, "operator"); err == nil {
		t.Fatal("writer task accepted a report_path — expected rejection")
	} else if !strings.Contains(err.Error(), "read_only") {
		t.Fatalf("rejection should say the task must be read_only: %v", err)
	}
	ro, got := taskReportPath(t, mgr, id, "t1")
	if ro || got != "" {
		t.Fatalf("a rejected edit mutated the task: read_only=%v report_path=%q", ro, got)
	}
	// And clearing read_only in the same call as the path is a contradiction.
	if _, err := mgr.EditTask(id, "t1", "", boolPtr(true), rp, "operator"); err != nil {
		t.Fatalf("set up: %v", err)
	}
	if _, err := mgr.EditTask(id, "t1", "", boolPtr(false), rp, "operator"); err == nil {
		t.Fatal("expected rejection: path passed while the task is being made writable")
	}
}

func TestEditTaskRejectsReportPathOutsideReportsDir(t *testing.T) {
	mgr, id := newReadOnlyFlagManager(t, PRDNeedsReview)
	const rp = ".datawatch/reports/gate.md"
	if _, err := mgr.EditTask(id, "t1", "", boolPtr(true), "", "operator"); err != nil {
		t.Fatalf("flag t1 read_only: %v", err)
	}
	if _, err := mgr.EditTask(id, "t1", "", nil, rp, "operator"); err != nil {
		t.Fatalf("set a valid path: %v", err)
	}
	for _, bad := range []string{"reports/gate.md", "../.datawatch/reports/gate.md", "src/audit.txt"} {
		if _, err := mgr.EditTask(id, "t1", "", nil, bad, "operator"); err == nil {
			t.Errorf("report path %q was accepted, want rejection", bad)
		}
	}
	_, got := taskReportPath(t, mgr, id, "t1")
	if got != ".datawatch/reports/gate.md" {
		t.Fatalf("a rejected path overwrote the stored one: %q", got)
	}
}

func TestAddTaskReadOnlyPersistsReportPath(t *testing.T) {
	mgr, id := newReadOnlyFlagManager(t, PRDNeedsReview)
	const rp = ".datawatch/reports/gate.md"
	prd, err := mgr.AddTaskReadOnly(id, "s1", "Run the gate", "READ-ONLY: gate", "operator", nil, true, rp)
	if err != nil {
		t.Fatalf("AddTaskReadOnly with report_path: %v", err)
	}
	var found bool
	var newID string
	for _, tk := range prd.Story[0].Tasks {
		if tk.Title == "Run the gate" {
			found = tk.ReadOnly && tk.ReportPath == rp
			newID = tk.ID
		}
	}
	if !found || newID == "" {
		t.Fatalf("new task: found=%v id=%q, want read_only with the report path", found, newID)
	}
	if _, got := taskReportPath(t, mgr, id, newID); got != rp {
		t.Fatalf("persisted report_path = %q, want %q", got, rp)
	}
}

func TestAddTaskReadOnlyRejectsReportPathContradictions(t *testing.T) {
	mgr, id := newReadOnlyFlagManager(t, PRDNeedsReview)
	const rp = ".datawatch/reports/gate.md"
	// Path with readOnly=false.
	if _, err := mgr.AddTaskReadOnly(id, "s1", "Audit", "READ-ONLY: audit", "operator", nil, false, rp); err == nil {
		t.Fatal("a writable task accepted a report_path — expected rejection")
	} else if !strings.Contains(err.Error(), "readOnly=true") {
		t.Fatalf("rejection should point at readOnly: %v", err)
	}
	// Invalid path, even with readOnly=true.
	if _, err := mgr.AddTaskReadOnly(id, "s1", "Audit", "READ-ONLY: audit", "operator", nil, true, "../outside.md"); err == nil {
		t.Fatal("a path outside .datawatch/reports/ was accepted — expected rejection")
	}
	// Nothing was stored.
	cur, _ := mgr.Store().GetPRD(id)
	for _, tk := range cur.Story[0].Tasks {
		if strings.HasPrefix(tk.Title, "Audit") {
			t.Fatalf("a rejected add created task %q read_only=%v report_path=%q", tk.Title, tk.ReadOnly, tk.ReportPath)
		}
	}
}
