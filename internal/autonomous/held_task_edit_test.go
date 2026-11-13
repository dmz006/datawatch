package autonomous

import (
	"strings"
	"testing"
)

const heldTestReport = ".datawatch/reports/held.md"

// newHeldTaskManager returns a Manager with one blocked Automaton whose tasks
// are: t1 held for review (with a review report), t2 running, t3 pending,
// t4 completed.
func newHeldTaskManager(t *testing.T) (*Manager, string) {
	t.Helper()
	mgr, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := mgr.CreatePRD("spec", t.TempDir(), "", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := mgr.Store().SetStories(prd.ID, []Story{{ID: "s1", Title: "Story", Tasks: []Task{
		{ID: "t1", Title: "Held", Spec: "old spec", Status: TaskReviewNeeded,
			ReviewSummary: &ReviewSummary{FailureCount: 3, Text: "attempt 1: failed"}},
		{ID: "t2", Title: "Running", Spec: "running spec", Status: TaskInProgress},
		{ID: "t3", Title: "Pending", Spec: "pending spec", Status: TaskPending},
		{ID: "t4", Title: "Done", Spec: "done spec", Status: TaskCompleted},
	}}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	cur, _ := mgr.Store().GetPRD(prd.ID)
	cur.Status = PRDBlocked
	if err := mgr.Store().SavePRD(cur); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return mgr, prd.ID
}

func heldTaskOfID(t *testing.T, mgr *Manager, prdID, taskID string) Task {
	t.Helper()
	prd, _ := mgr.Store().GetPRD(prdID)
	for _, s := range prd.Story {
		for _, tk := range s.Tasks {
			if tk.ID == taskID {
				return tk
			}
		}
	}
	t.Fatalf("task %q not found", taskID)
	return Task{}
}

// TestHeldTaskEditAcceptsSpecReadOnlyAndReportPath: a review_needed task of a
// blocked Automaton takes a spec, read_only and report_path edit, stays held,
// and the edit is in the decision log.
func TestHeldTaskEditAcceptsSpecReadOnlyAndReportPath(t *testing.T) {
	mgr, id := newHeldTaskManager(t)
	if _, err := mgr.EditTask(id, "t1", "new spec", boolPtr(true), heldTestReport, "", "", "operator"); err != nil {
		t.Fatalf("EditTask on a held task: %v", err)
	}
	tk := heldTaskOfID(t, mgr, id, "t1")
	if tk.Spec != "new spec" || !tk.ReadOnly || tk.ReportPath != heldTestReport {
		t.Fatalf("task after edit: spec=%q read_only=%v report_path=%q", tk.Spec, tk.ReadOnly, tk.ReportPath)
	}
	if tk.Status != TaskReviewNeeded || tk.ReviewSummary == nil {
		t.Fatalf("edit changed the held status: status=%q summary=%v", tk.Status, tk.ReviewSummary)
	}
	prd, _ := mgr.Store().GetPRD(id)
	last := prd.Decisions[len(prd.Decisions)-1]
	if last.Kind != "edit_task" || !strings.Contains(last.Note, "read_only=true") || !strings.Contains(last.Note, heldTestReport) {
		t.Fatalf("decision log entry = %+v, want an edit_task note with read_only and report_path", last)
	}
}

// TestHeldTaskEditStillRefusesCompletedRunningAndPending: completed tasks, and
// running or pending tasks of a blocked Automaton, are still refused and keep
// their spec.
func TestHeldTaskEditStillRefusesCompletedRunningAndPending(t *testing.T) {
	mgr, id := newHeldTaskManager(t)
	for _, c := range []struct{ task, want string }{
		{"t4", "completed"},
		{"t2", "cannot be edited while prd"},
		{"t3", "cannot be edited while prd"},
	} {
		before := heldTaskOfID(t, mgr, id, c.task)
		_, err := mgr.EditTask(id, c.task, "changed", nil, "", "", "", "operator")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("EditTask(%s) error = %v, want one mentioning %q", c.task, err, c.want)
		}
		if after := heldTaskOfID(t, mgr, id, c.task); after.Spec != before.Spec || after.Status != before.Status {
			t.Fatalf("refused edit changed %s: %+v -> %+v", c.task, before, after)
		}
	}
}

// TestHeldTaskEditRefusedReportPathLeavesTaskUnchanged: an invalid report path,
// or a report path on a task that is not read-only after the edit, is refused
// and nothing else of the same edit is written.
func TestHeldTaskEditRefusedReportPathLeavesTaskUnchanged(t *testing.T) {
	mgr, id := newHeldTaskManager(t)
	before := heldTaskOfID(t, mgr, id, "t1")
	cases := []struct {
		name       string
		readOnly   *bool
		reportPath string
		want       string
	}{
		{"absolute path", boolPtr(true), "/etc/report.md", "repo-relative"},
		{"outside reports dir", boolPtr(true), "docs/report.md", ".datawatch/reports/"},
		{"parent segment", boolPtr(true), ".datawatch/reports/../x.md", ".."},
		{"task not read-only", nil, heldTestReport, "read_only"},
		{"read-only cleared in the same call", boolPtr(false), heldTestReport, "read_only"},
	}
	for _, c := range cases {
		_, err := mgr.EditTask(id, "t1", "half applied spec", c.readOnly, c.reportPath, "", "", "operator")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: error = %v, want one mentioning %q", c.name, err, c.want)
		}
		after := heldTaskOfID(t, mgr, id, "t1")
		if after.Spec != before.Spec || after.ReadOnly != before.ReadOnly || after.ReportPath != before.ReportPath || after.Status != before.Status {
			t.Fatalf("%s: refused edit changed the task: %+v -> %+v", c.name, before, after)
		}
	}
}

// TestReviewRetryReadOnlyRequeuesWithBothApplied: a retry with read_only and a
// report path requeues the held task to pending with both set, and the
// decision log and the next attempt's hint say so.
func TestReviewRetryReadOnlyRequeuesWithBothApplied(t *testing.T) {
	mgr, id := newHeldTaskManager(t)
	req := ReviewResolution{Action: reviewActionRetry, ReadOnly: boolPtr(true), ReportPath: heldTestReport, Note: "report only"}
	if _, err := mgr.ResolveTaskReview(id, "t1", req, "operator-test"); err != nil {
		t.Fatalf("ResolveTaskReview: %v", err)
	}
	tk := heldTaskOfID(t, mgr, id, "t1")
	if normTaskStatus(tk.Status) != TaskPending {
		t.Fatalf("status after retry = %q, want pending", tk.Status)
	}
	if !tk.ReadOnly || tk.ReportPath != heldTestReport {
		t.Fatalf("read_only=%v report_path=%q, want true and %q", tk.ReadOnly, tk.ReportPath, heldTestReport)
	}
	if tk.ReviewSummary != nil {
		t.Fatal("review report survived the retry")
	}
	if !strings.Contains(tk.PriorAttemptHint, "read-only") || !strings.Contains(tk.PriorAttemptHint, heldTestReport) {
		t.Fatalf("hint does not mention the read-only mark and report: %q", tk.PriorAttemptHint)
	}
	prd, _ := mgr.Store().GetPRD(id)
	var note string
	for _, d := range prd.Decisions {
		if d.Kind == reviewDecisionKind {
			note = d.Note
		}
	}
	if !strings.Contains(note, "read_only=true") || !strings.Contains(note, "report_path="+heldTestReport) {
		t.Fatalf("decision note %q does not mention both changes", note)
	}
}

// TestReviewRetryReadOnlyReportPathAloneOnReadOnlyTask: a task already marked
// read-only takes a report path on its own.
func TestReviewRetryReadOnlyReportPathAloneOnReadOnlyTask(t *testing.T) {
	mgr, id := newHeldTaskManager(t)
	if _, err := mgr.EditTask(id, "t1", "", boolPtr(true), "", "", "", "operator"); err != nil {
		t.Fatalf("EditTask: %v", err)
	}
	if _, err := mgr.ResolveTaskReview(id, "t1", ReviewResolution{Action: reviewActionRetry, ReportPath: heldTestReport}, "operator-test"); err != nil {
		t.Fatalf("ResolveTaskReview: %v", err)
	}
	if tk := heldTaskOfID(t, mgr, id, "t1"); !tk.ReadOnly || tk.ReportPath != heldTestReport || normTaskStatus(tk.Status) != TaskPending {
		t.Fatalf("task after retry: %+v", tk)
	}
}

// TestReviewRetryReadOnlyRefusedChangesLeaveTaskHeld: a bad report path, a
// report path on a task that is not read-only, and a read-only mark on a task
// that has planned files are all refused, and the task stays held and unchanged.
func TestReviewRetryReadOnlyRefusedChangesLeaveTaskHeld(t *testing.T) {
	mgr, id := newHeldTaskManager(t)
	cur, _ := mgr.Store().GetPRD(id)
	mgr.Store().WithLock(func() { cur.Story[0].Tasks[0].FilesPlanned = []string{"internal/x.go"} })
	if err := mgr.Store().SavePRD(cur); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	before := heldTaskOfID(t, mgr, id, "t1")
	cases := []struct {
		name string
		req  ReviewResolution
		want string
	}{
		{"bad report path", ReviewResolution{Action: reviewActionRetry, ReadOnly: boolPtr(true), ReportPath: "docs/x.md"}, ".datawatch/reports/"},
		{"report path on a writer", ReviewResolution{Action: reviewActionRetry, ReportPath: heldTestReport}, "read_only"},
		{"read-only with planned files", ReviewResolution{Action: reviewActionRetry, ReadOnly: boolPtr(true)}, "planned files"},
	}
	for _, c := range cases {
		_, err := mgr.ResolveTaskReview(id, "t1", c.req, "operator-test")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: error = %v, want one mentioning %q", c.name, err, c.want)
		}
		after := heldTaskOfID(t, mgr, id, "t1")
		if after.Status != TaskReviewNeeded || after.ReviewSummary == nil || after.ReadOnly != before.ReadOnly || after.ReportPath != before.ReportPath || after.Spec != before.Spec {
			t.Fatalf("%s: refused retry changed the held task: %+v", c.name, after)
		}
	}
}
