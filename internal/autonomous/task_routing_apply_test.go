package autonomous

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// routingFixture builds a manager with routing on, one draft Automaton and
// one story. No session is ever started.
func routingFixture(t *testing.T) (*Manager, *PRD, *Story) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.TaskRouting = config.DefaultTaskRouting()
	cfg.TaskRouting.Enabled = true
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	m.SetTaskRoutingBackendCheck(func(b string) bool { return b == "pi-prompt" || b == "claude-code" })
	prd, err := m.CreatePRD("spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	m.Store().WithLock(func() { prd.Status = PRDNeedsReview })
	if _, err := m.AddStory(prd.ID, "story", "desc", "op"); err != nil {
		t.Fatal(err)
	}
	return m, prd, &prd.Story[0]
}

func addRoutedTask(t *testing.T, m *Manager, prdID, storyID, title, spec string) *Task {
	t.Helper()
	if _, err := m.AddTask(prdID, storyID, title, spec, "op", nil); err != nil {
		t.Fatal(err)
	}
	prd, _ := m.Store().GetPRD(prdID)
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].Title == title {
				return &prd.Story[si].Tasks[ti]
			}
		}
	}
	t.Fatalf("task %q not found", title)
	return nil
}

func setProfileRow(m *Manager, profile string, taskType string, row config.TaskRoutingRow) {
	cfg := m.Config()
	tr := cfg.TaskRouting.Clone()
	tr.Profiles[profile][taskType] = row
	cfg.TaskRouting = tr
	m.SetConfig(cfg)
}

func TestTaskRoutingAddTaskAppliesActiveProfile(t *testing.T) {
	m, prd, st := routingFixture(t)
	task := addRoutedTask(t, m, prd.ID, st.ID, "Write unit tests for the router", "cover it")
	if task.Backend != "pi-prompt" || task.Model != "ollama/qwen3.8:27b" {
		t.Fatalf("tests row not applied: %q/%q", task.Backend, task.Model)
	}
	r := task.TaskRouting
	if r == nil || !r.Applied || r.TaskType != config.TaskTypeTests || r.Row != config.TaskTypeTests ||
		r.Profile != "default" || r.ClassifiedBy != TaskClassifiedByTitleSpec {
		t.Fatalf("bad record: %+v", r)
	}
	code := addRoutedTask(t, m, prd.ID, st.ID, "Implement the engine", "do the work")
	if code.Backend != "claude-code" || code.TaskRouting.TaskType != config.TaskTypeDaemonBackendCode ||
		code.TaskRouting.ClassifiedBy != TaskClassifiedByDefault {
		t.Fatalf("code task misrouted: %+v %q", code.TaskRouting, code.Backend)
	}
}

func TestTaskRoutingDecisionLogNamesTypeProfileRowResult(t *testing.T) {
	m, prd, st := routingFixture(t)
	task := addRoutedTask(t, m, prd.ID, st.ID, "Update the how-to docs", "")
	p, _ := m.Store().GetPRD(prd.ID)
	var found *Decision
	for i := range p.Decisions {
		if p.Decisions[i].Kind == "task_routing" {
			found = &p.Decisions[i]
		}
	}
	if found == nil {
		t.Fatal("no task_routing decision")
	}
	for _, want := range []string{"task=" + task.ID, "type=docs_set", "profile=default", "row=docs_set", "pi-prompt/ollama/qwen3.8:27b"} {
		if !strings.Contains(found.Note, want) {
			t.Errorf("note %q lacks %q", found.Note, want)
		}
	}
	if found.Actor != taskRoutingActor {
		t.Errorf("actor = %q", found.Actor)
	}
}

func TestTaskRoutingDisabledRecordsReason(t *testing.T) {
	m, prd, st := routingFixture(t)
	cfg := m.Config()
	cfg.TaskRouting.Enabled = false
	m.SetConfig(cfg)
	task := addRoutedTask(t, m, prd.ID, st.ID, "Write docs", "")
	if task.Backend != "" || task.TaskRouting == nil || task.TaskRouting.Applied || task.TaskRouting.Reason != "routing disabled" {
		t.Fatalf("disabled routing: %q %+v", task.Backend, task.TaskRouting)
	}
	p, _ := m.Store().GetPRD(prd.ID)
	for _, d := range p.Decisions {
		if d.Kind == "task_routing" {
			t.Error("a not-applied decision wrote a log entry")
		}
	}
}

func TestTaskRoutingPrecedenceTaskStoryAutomatonWin(t *testing.T) {
	m, prd, st := routingFixture(t)
	// Task LLM set by an operator.
	task := addRoutedTask(t, m, prd.ID, st.ID, "Write unit tests", "")
	if _, _, err := m.SetTaskLLM(prd.ID, task.ID, "claude-code", "", "haiku", "op"); err != nil {
		t.Fatal(err)
	}
	// Create a second task, set LLM before routing runs by clearing the record.
	fresh := &Task{ID: "fresh1", Title: "Write unit tests", Backend: "claude-code", Model: "opus-hand", Status: TaskPending}
	cfg, chk := m.taskRoutingSnapshot()
	d := planTaskRouting(prd, fresh, st, nil, cfg, chk, "", false)
	if d.Applied || d.Reason != "task LLM set by operator" {
		t.Fatalf("task LLM did not win: %+v", d)
	}
	// Story LLM.
	m.Store().WithLock(func() { st.Backend = "pi-prompt"; st.Model = "ollama/x" })
	plain := &Task{ID: "plain1", Title: "Write unit tests", Status: TaskPending}
	d = planTaskRouting(prd, plain, st, nil, cfg, chk, "", false)
	if d.Applied || d.Reason != "story LLM set" {
		t.Fatalf("story LLM did not win: %+v", d)
	}
	m.Store().WithLock(func() { st.Backend, st.Model = "", "" })
	// Automaton LLM (the Automaton's own record).
	m.Store().WithLock(func() { prd.Backend = "claude-code" })
	d = planTaskRouting(prd, plain, st, nil, cfg, chk, "", false)
	if d.Applied || d.Reason != "Automaton LLM set" {
		t.Fatalf("Automaton LLM did not win: %+v", d)
	}
	m.Store().WithLock(func() { prd.Backend = "" })
	auto := &Automation{Backend: "claude-code"}
	d = planTaskRouting(prd, plain, st, auto, cfg, chk, "", false)
	if d.Applied || d.Reason != "Automaton LLM set" {
		t.Fatalf("Automation LLM did not win: %+v", d)
	}
	// The hand-set task is untouched by a re-apply.
	got := m.ApplyTaskRouting(prd, task)
	if got.Applied {
		t.Fatalf("hand-set task re-routed: %+v", got)
	}
	again, _ := findTaskInPRD(prd, task.ID)
	if again.Model != "haiku" {
		t.Errorf("hand-set model overwritten: %q", again.Model)
	}
}

func TestTaskRoutingPlannerRerunKeepsHandEdits(t *testing.T) {
	m, prd, st := routingFixture(t)
	edited := addRoutedTask(t, m, prd.ID, st.ID, "Update the docs", "first spec")
	plain := addRoutedTask(t, m, prd.ID, st.ID, "Write regression tests", "")
	if edited.Model != "ollama/qwen3.8:27b" {
		t.Fatalf("not routed: %q", edited.Model)
	}
	if _, err := m.EditTask(prd.ID, edited.ID, "second spec", nil, "", "", "", "op"); err != nil {
		t.Fatal(err)
	}
	// The operator later changes the table; the planner runs again.
	setProfileRow(m, "default", config.TaskTypeDocsSet, config.TaskRoutingRow{WorkerBackend: "claude-code", WorkerModel: "sonnet"})
	setProfileRow(m, "default", config.TaskTypeTests, config.TaskRoutingRow{WorkerBackend: "claude-code", WorkerModel: "sonnet"})
	p, _ := m.Store().GetPRD(prd.ID)
	m.ApplyTaskRoutingToPRD(p)
	e, _ := findTaskInPRD(p, edited.ID)
	if e.Model != "ollama/qwen3.8:27b" || !e.TaskRouting.Applied {
		t.Errorf("hand-edited task overwritten: %q %+v", e.Model, e.TaskRouting)
	}
	r, _ := findTaskInPRD(p, plain.ID)
	if r.Backend != "claude-code" || r.Model != "sonnet" {
		t.Errorf("router-owned task not re-applied: %q/%q", r.Backend, r.Model)
	}
	// The planner's own earlier result is not mistaken for a hand set LLM,
	// and a second pass is a no-op with no extra log entry.
	n := len(p.Decisions)
	m.ApplyTaskRoutingToPRD(p)
	if len(p.Decisions) != n {
		t.Errorf("second pass appended %d decisions", len(p.Decisions)-n)
	}
}

func TestTaskRoutingProfileSwitchOnlyAffectsNewTasks(t *testing.T) {
	m, prd, st := routingFixture(t)
	old := addRoutedTask(t, m, prd.ID, st.ID, "Write unit tests", "")
	running := addRoutedTask(t, m, prd.ID, st.ID, "Fix the lint", "")
	m.Store().WithLock(func() {
		r, _ := findTaskInPRD(prd, running.ID)
		r.Status = TaskInProgress
		r.SessionID = "live-session"
		r.Backend, r.Model = "", ""
		r.TaskRouting = nil
	})
	cfg := m.Config()
	cfg.TaskRouting, _ = SaveProfile(cfg.TaskRouting, "alt", nil)
	cfg.TaskRouting.Profiles["alt"][config.TaskTypeTests] = config.TaskRoutingRow{WorkerBackend: "claude-code", WorkerModel: "sonnet"}
	cfg.TaskRouting.Profiles["alt"][config.TaskTypeFormattingCleanup] = config.TaskRoutingRow{WorkerBackend: "claude-code", WorkerModel: "sonnet"}
	cfg.TaskRouting, _ = SetActiveProfile(cfg.TaskRouting, "alt")
	m.SetConfig(cfg)

	p, _ := m.Store().GetPRD(prd.ID)
	o, _ := findTaskInPRD(p, old.ID)
	if o.Backend != "pi-prompt" {
		t.Errorf("existing task re-routed by the switch: %q", o.Backend)
	}
	// A running task is never touched, even when the table is applied to it.
	rt, _ := findTaskInPRD(p, running.ID)
	if got := m.ApplyTaskRouting(p, rt); got.Applied || got.Reason != "task already started" {
		t.Errorf("running task routed: %+v", got)
	}
	if rt.Backend != "" || rt.SessionID != "live-session" {
		t.Errorf("running task changed: %q %q", rt.Backend, rt.SessionID)
	}
	nt := addRoutedTask(t, m, prd.ID, st.ID, "Add more tests", "")
	if nt.Backend != "claude-code" || nt.TaskRouting.Profile != "alt" {
		t.Errorf("new task not routed by the new profile: %q %+v", nt.Backend, nt.TaskRouting)
	}
}

func TestTaskRoutingUnknownLLMRefusedAndRecorded(t *testing.T) {
	m, prd, st := routingFixture(t)
	setProfileRow(m, "default", config.TaskTypeDocsSet, config.TaskRoutingRow{WorkerBackend: "ghost-llm", WorkerModel: "m"})
	task := addRoutedTask(t, m, prd.ID, st.ID, "Write the docs", "")
	if task.Backend != "" || task.Model != "" {
		t.Fatalf("unknown LLM written to the task: %q/%q", task.Backend, task.Model)
	}
	r := task.TaskRouting
	if r == nil || r.Applied || !strings.Contains(r.Reason, "ghost-llm") || r.Row != config.TaskTypeDocsSet {
		t.Fatalf("refusal not recorded: %+v", r)
	}
	// A usable second choice is applied instead and marked.
	setProfileRow(m, "default", config.TaskTypeDocsSet, config.TaskRoutingRow{
		WorkerBackend: "ghost-llm", WorkerModel: "m",
		SecondChoice: &config.TaskRoutingChoice{Backend: "pi-prompt", Model: "ollama/qwen3.8:27b"},
	})
	t2 := addRoutedTask(t, m, prd.ID, st.ID, "Write the docs again", "")
	if t2.Backend != "pi-prompt" || !t2.TaskRouting.SecondChoiceUsed || !t2.TaskRouting.Applied {
		t.Fatalf("second choice: %q %+v", t2.Backend, t2.TaskRouting)
	}
}

func TestRoutingDryRunWritesNothing(t *testing.T) {
	m, prd, st := routingFixture(t)
	cfg := m.Config()
	cfg.TaskRouting.Enabled = false
	m.SetConfig(cfg)
	addRoutedTask(t, m, prd.ID, st.ID, "Write unit tests", "")
	addRoutedTask(t, m, prd.ID, st.ID, "Implement the engine", "")
	before, _ := m.Store().SnapshotPRD(prd.ID)
	out, err := m.TaskRoutingDryRun(prd.ID, "")
	if err != nil || len(out) != 2 {
		t.Fatalf("dry run: %v %v", err, out)
	}
	if !out[0].WouldApply || out[0].TaskType != config.TaskTypeTests || out[0].Result.WorkerBackend != "pi-prompt" {
		t.Errorf("entry 0: %+v", out[0])
	}
	if !out[1].WouldApply || out[1].Result.WorkerBackend != "claude-code" {
		t.Errorf("entry 1: %+v", out[1])
	}
	after, _ := m.Store().SnapshotPRD(prd.ID)
	if len(after.Decisions) != len(before.Decisions) {
		t.Error("dry run appended a decision")
	}
	for si := range after.Story {
		for ti := range after.Story[si].Tasks {
			a, b := after.Story[si].Tasks[ti], before.Story[si].Tasks[ti]
			if a.Backend != b.Backend || a.Model != b.Model || a.UpdatedAt != b.UpdatedAt {
				t.Error("dry run changed a task")
			}
		}
	}
	// A named profile.
	cfg = m.Config()
	cfg.TaskRouting, _ = SaveProfile(cfg.TaskRouting, "alt", nil)
	cfg.TaskRouting.Profiles["alt"][config.TaskTypeTests] = config.TaskRoutingRow{WorkerBackend: "claude-code", WorkerModel: "sonnet"}
	m.SetConfig(cfg)
	out, _ = m.TaskRoutingDryRun(prd.ID, "alt")
	if out[0].Profile != "alt" || out[0].Result.WorkerBackend != "claude-code" {
		t.Errorf("named profile: %+v", out[0])
	}
	if _, err := m.TaskRoutingDryRun(prd.ID, "nope"); err == nil {
		t.Error("unknown profile accepted")
	}
}

func TestTaskRoutingForAutomatonReportsRowAndEffective(t *testing.T) {
	m, prd, st := routingFixture(t)
	routed := addRoutedTask(t, m, prd.ID, st.ID, "Write unit tests", "")
	hand := addRoutedTask(t, m, prd.ID, st.ID, "Add more tests", "")
	if _, _, err := m.SetTaskLLM(prd.ID, hand.ID, "claude-code", "", "haiku", "op"); err != nil {
		t.Fatal(err)
	}
	views, err := m.TaskRoutingForAutomaton(prd.ID)
	if err != nil || len(views) != 2 {
		t.Fatalf("views: %v %v", err, views)
	}
	byID := map[string]TaskRoutingView{}
	for _, v := range views {
		byID[v.TaskID] = v
	}
	if v := byID[routed.ID]; v.Row != config.TaskTypeTests || v.Effective.Backend != "pi-prompt" || v.Reason != "" {
		t.Errorf("routed view: %+v", v)
	}
	if v := byID[hand.ID]; v.Row != "" || v.Reason != "task LLM set by operator" || v.Effective.Model != "haiku" {
		t.Errorf("hand view: %+v", v)
	}
}

func TestTaskRoutingDecomposeTailAppliesToAllTasks(t *testing.T) {
	m, prd, st := routingFixture(t)
	// Materialize tasks the way the planner does: directly in the store.
	m.Store().WithLock(func() {
		for _, title := range []string{"Write unit tests", "Implement the engine"} {
			st.Tasks = append(st.Tasks, Task{ID: "dec-" + title[:5], StoryID: st.ID, PRDID: prd.ID, Title: title})
		}
	})
	if n := m.ApplyTaskRoutingToPRD(prd); n != 2 {
		t.Fatalf("applied to %d tasks", n)
	}
	if st.Tasks[0].Backend != "pi-prompt" || st.Tasks[1].Backend != "claude-code" {
		t.Errorf("routing: %q %q", st.Tasks[0].Backend, st.Tasks[1].Backend)
	}
}
