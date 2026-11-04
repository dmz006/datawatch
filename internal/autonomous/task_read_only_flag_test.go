package autonomous

import (
	"strings"
	"testing"
)

func newReadOnlyFlagManager(t *testing.T, status PRDStatus) (*Manager, string) {
	t.Helper()
	mgr, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := mgr.CreatePRD("spec", t.TempDir(), "", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := mgr.Store().SetStories(prd.ID, []Story{
		{ID: "s1", Title: "Story", Tasks: []Task{{ID: "t1", Title: "Task one", Spec: "old spec"}}},
	}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	cur, _ := mgr.Store().GetPRD(prd.ID)
	cur.Status = status
	if err := mgr.Store().SavePRD(cur); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return mgr, prd.ID
}

func readOnlyOf(t *testing.T, mgr *Manager, prdID, taskID string) (bool, string) {
	t.Helper()
	prd, _ := mgr.Store().GetPRD(prdID)
	for _, s := range prd.Story {
		for _, tk := range s.Tasks {
			if tk.ID == taskID {
				return tk.ReadOnly, tk.Spec
			}
		}
	}
	t.Fatalf("task %q not found", taskID)
	return false, ""
}

func TestEditTaskSetsAndClearsReadOnlyKeepingSpec(t *testing.T) {
	mgr, id := newReadOnlyFlagManager(t, PRDNeedsReview)
	if _, err := mgr.EditTask(id, "t1", "", boolPtr(true), "", "operator"); err != nil {
		t.Fatalf("EditTask set: %v", err)
	}
	ro, spec := readOnlyOf(t, mgr, id, "t1")
	if !ro || spec != "old spec" {
		t.Fatalf("after set: read_only=%v spec=%q, want true and the old spec", ro, spec)
	}
	// A spec-only edit leaves the flag alone.
	if _, err := mgr.EditTask(id, "t1", "new spec", nil, "", "operator"); err != nil {
		t.Fatalf("EditTask spec: %v", err)
	}
	ro, spec = readOnlyOf(t, mgr, id, "t1")
	if !ro || spec != "new spec" {
		t.Fatalf("after spec edit: read_only=%v spec=%q, want true and the new spec", ro, spec)
	}
	if _, err := mgr.EditTask(id, "t1", "", boolPtr(false), "", "operator"); err != nil {
		t.Fatalf("EditTask clear: %v", err)
	}
	if ro, _ := readOnlyOf(t, mgr, id, "t1"); ro {
		t.Fatal("read_only still true after clearing")
	}
}

func TestEditTaskNeedsSomethingToEdit(t *testing.T) {
	mgr, id := newReadOnlyFlagManager(t, PRDNeedsReview)
	if _, err := mgr.EditTask(id, "t1", "", nil, "", "operator"); err == nil {
		t.Fatal("expected an error when neither spec nor read_only is given")
	}
}

func TestEditTaskReadOnlyHonoursStatusGates(t *testing.T) {
	mgr, id := newReadOnlyFlagManager(t, PRDRunning)
	if _, err := mgr.EditTask(id, "t1", "", boolPtr(true), "", "operator"); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("running PRD edit err = %v, want a locked error", err)
	}
	if ro, _ := readOnlyOf(t, mgr, id, "t1"); ro {
		t.Fatal("a rejected edit changed read_only")
	}
}

func TestEditTaskSpecStillWorksWithoutReadOnly(t *testing.T) {
	mgr, id := newReadOnlyFlagManager(t, PRDNeedsReview)
	if _, err := mgr.EditTaskSpec(id, "t1", "reworded", "operator"); err != nil {
		t.Fatalf("EditTaskSpec: %v", err)
	}
	if ro, spec := readOnlyOf(t, mgr, id, "t1"); ro || spec != "reworded" {
		t.Fatalf("read_only=%v spec=%q", ro, spec)
	}
}

func TestAddTaskReadOnlyPersistsFlag(t *testing.T) {
	mgr, id := newReadOnlyFlagManager(t, PRDNeedsReview)
	prd, err := mgr.AddTaskReadOnly(id, "s1", "Run the suite", "READ-ONLY: gate", "operator", nil, true, "")
	if err != nil {
		t.Fatalf("AddTaskReadOnly: %v", err)
	}
	var newID string
	for _, tk := range prd.Story[0].Tasks {
		if tk.Title == "Run the suite" {
			newID = tk.ID
		}
	}
	if newID == "" {
		t.Fatal("new task missing from the returned PRD")
	}
	if ro, _ := readOnlyOf(t, mgr, id, newID); !ro {
		t.Fatal("new task not read_only after AddTaskReadOnly(true)")
	}
	// Plain AddTask stays writable.
	if _, err := mgr.AddTask(id, "s1", "Write code", "do it", "operator", nil); err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	cur, _ := mgr.Store().GetPRD(id)
	for _, tk := range cur.Story[0].Tasks {
		if tk.Title == "Write code" && tk.ReadOnly {
			t.Fatal("AddTask produced a read_only task")
		}
	}
}

func TestPlanningPromptsTellThePlannerWhenToSetReadOnly(t *testing.T) {
	prompts := map[string]string{"PlanningPrompt": PlanningPrompt, "PlanningPromptSession": PlanningPromptSession}
	for name, p := range prompts {
		if !strings.Contains(p, planReadOnlyFlagRuleBullet) {
			t.Errorf("%s lacks the read_only rule bullet", name)
		}
		if !strings.Contains(p, `"read_only": false`) {
			t.Errorf("%s schema lacks the read_only field", name)
		}
	}
	if strings.Contains(planReadOnlyFlagRuleBullet, "%") {
		t.Error("the read_only bullet must not contain a percent sign: the tooled prompt goes through fmt.Sprintf")
	}
	if !strings.Contains(planReadOnlyFlagRuleBullet, "changes no files") {
		t.Error("the bullet must say read_only is for tasks that change no files")
	}
}

func TestParsePlanningPreservesReadOnly(t *testing.T) {
	raw := `{"title":"T","stories":[{"title":"S","tasks":[
		{"title":"Run gate","spec":"READ-ONLY: runs the suite","read_only":true},
		{"title":"Write code","spec":"edit things","files":["a.go"]}]}]}`
	_, stories, err := ParsePlanning(raw)
	if err != nil {
		t.Fatalf("ParsePlanning: %v", err)
	}
	if !stories[0].Tasks[0].ReadOnly {
		t.Error("read_only true was dropped by the parser")
	}
	if stories[0].Tasks[1].ReadOnly {
		t.Error("an absent read_only parsed as true")
	}
}

func TestNormalizeReadOnlyTasksClearsContradictions(t *testing.T) {
	stories := []Story{{Tasks: []Task{
		{Title: "gate", ReadOnly: true},
		{Title: "liar", ReadOnly: true, FilesPlanned: []string{"a.go"}},
		{Title: "writer", FilesPlanned: []string{"b.go"}},
	}}}
	if n := normalizeReadOnlyTasks(stories); n != 1 {
		t.Fatalf("cleared %d flags, want 1", n)
	}
	got := stories[0].Tasks
	if !got[0].ReadOnly || got[1].ReadOnly || got[2].ReadOnly {
		t.Fatalf("flags after normalize = %v %v %v, want true false false", got[0].ReadOnly, got[1].ReadOnly, got[2].ReadOnly)
	}
}

func TestPlanningPromptsTellThePlannerAboutReportPath(t *testing.T) {
	prompts := map[string]string{"PlanningPrompt": PlanningPrompt, "PlanningPromptSession": PlanningPromptSession}
	for name, p := range prompts {
		if !strings.Contains(p, planReadOnlyFlagRuleBullet) {
			t.Errorf("%s lacks the read_only rule bullet", name)
		}
		if !strings.Contains(p, `"report_path"`) {
			t.Errorf("%s schema lacks the report_path field", name)
		}
	}
	if !strings.Contains(planReadOnlyFlagRuleBullet, `report_path`) {
		t.Error("the read_only bullet must mention report_path")
	}
	if !strings.Contains(planReadOnlyFlagRuleBullet, ".datawatch/reports/") {
		t.Error("the read_only bullet must name .datawatch/reports/ as the report location")
	}
	if !strings.Contains(planReadOnlyFlagRuleBullet, "keeps \"files\" empty") {
		t.Error("the read_only bullet must say a report task keeps files empty")
	}
	if strings.Contains(planReadOnlyFlagRuleBullet, "%") {
		t.Error("the read_only bullet must not contain a percent sign: the tooled prompt goes through fmt.Sprintf")
	}
}

func TestParsePlanningPreservesReportPath(t *testing.T) {
	raw := `{"title":"T","stories":[{"title":"S","tasks":[
		{"title":"Audit","spec":"READ-ONLY: audit","read_only":true,"report_path":".datawatch/reports/audit.md"},
		{"title":"Write code","spec":"edit things","files":["a.go"]}]}]}`
	_, stories, err := ParsePlanning(raw)
	if err != nil {
		t.Fatalf("ParsePlanning: %v", err)
	}
	if got := stories[0].Tasks[0].ReportPath; got != ".datawatch/reports/audit.md" {
		t.Errorf("report_path dropped by the parser: got %q", got)
	}
	if !stories[0].Tasks[0].ReadOnly {
		t.Error("read_only true was dropped by the parser")
	}
	if got := stories[0].Tasks[1].ReportPath; got != "" {
		t.Errorf("an absent report_path parsed as %q", got)
	}
}

func TestNormalizeReadOnlyTasksKeepsAndClearsReportPath(t *testing.T) {
	const rp = ".datawatch/reports/gate.md"
	stories := []Story{{Tasks: []Task{
		{Title: "gate", ReadOnly: true, ReportPath: rp},
		{Title: "liar", ReadOnly: true, FilesPlanned: []string{"a.go"}, ReportPath: rp},
		{Title: "writer", ReportPath: rp},
	}}}
	if n := normalizeReadOnlyTasks(stories); n != 1 {
		t.Fatalf("cleared %d flags, want 1", n)
	}
	got := stories[0].Tasks
	if !got[0].ReadOnly || got[0].ReportPath != rp {
		t.Errorf("read-only task: flags/path = %v %q, want true %q", got[0].ReadOnly, got[0].ReportPath, rp)
	}
	if got[1].ReadOnly || got[1].ReportPath != "" {
		t.Errorf("contradictory task: flags/path = %v %q, want false and no report_path", got[1].ReadOnly, got[1].ReportPath)
	}
	if got[2].ReadOnly || got[2].ReportPath != "" {
		t.Errorf("writer task: flags/path = %v %q, want false and no report_path", got[2].ReadOnly, got[2].ReportPath)
	}
}
