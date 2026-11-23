package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// planner_no_task_backend_test.go — the planner never writes a worker backend
// or model onto a task. A task gets its own backend only when its decomposed
// JSON or its spec names one; otherwise Task.Backend and Task.Model stay empty
// and the task runs on the story's, then the Automaton's, setting. The plans
// are scripted through a fake decompose function; no LLM is called.

// noBackendPlanJSON is a plan that names no backend anywhere. Both titles
// classify to rows of the default routing table that name a worker backend.
const noBackendPlanJSON = `{
  "title": "planned",
  "stories": [
    {
      "id": "s1",
      "title": "Build",
      "tasks": [
        { "id": "t1", "title": "Write unit tests for A", "spec": "Cover requirement A\nverification_command: echo a", "files_planned": ["docs/a.md"], "verification_script": "tests/a.sh" },
        { "id": "t2", "title": "Update the docs for B", "spec": "Cover requirement B", "verification_script": "tests/b.sh" }
      ]
    }
  ]
}`

// namedBackendPlanJSON names a backend for t1 in the task JSON and for t2 in
// the spec text; t3 names none.
const namedBackendPlanJSON = `{
  "title": "planned",
  "stories": [
    {
      "id": "s1",
      "title": "Build",
      "tasks": [
        { "id": "t1", "title": "Write unit tests for A", "spec": "Cover requirement A\nverification_command: echo a", "backend": "claude-code", "files_planned": ["docs/a.md"], "verification_script": "tests/a.sh" },
        { "id": "t2", "title": "Update the docs for B", "spec": "Cover requirement B\nbackend: claude-code", "verification_script": "tests/b.sh" },
        { "id": "t3", "title": "Write regression tests for B", "spec": "Cover requirement B again", "verification_script": "tests/c.sh" }
      ]
    }
  ]
}`

// plannerNoBackendManager builds a manager whose routing table is on and
// whose rows name a worker backend and an effort, with a fake planner that
// returns plan.
func plannerNoBackendManager(t *testing.T, plan string, rowEffort string) (*Manager, *PRD) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.PlanningBackend = "global-b"
	cfg.PlanningModel = "global-m"
	cfg.TaskRouting = config.DefaultTaskRouting()
	cfg.TaskRouting.Enabled = true
	for typ, row := range cfg.TaskRouting.Profiles["default"] {
		row.Effort = rowEffort
		cfg.TaskRouting.Profiles["default"][typ] = row
	}
	f := &fakeDecomposer{resp: []string{plan}}
	m := newRoutingManager(t, cfg, f)
	m.SetTaskRoutingBackendCheck(func(b string) bool { return b == "pi-prompt" || b == "claude-code" })
	return m, newRoutingPRD(t, m)
}

// plannedTasks returns every task of the stored plan, by title.
func plannedTasks(t *testing.T, m *Manager, prdID string) (*PRD, map[string]*Task) {
	t.Helper()
	prd, ok := m.Store().SnapshotPRD(prdID)
	if !ok {
		t.Fatalf("prd %s not found", prdID)
	}
	out := map[string]*Task{}
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			tk := &prd.Story[si].Tasks[ti]
			out[tk.Title] = tk
		}
	}
	return prd, out
}

func decomposeBothPaths(t *testing.T, plan, rowEffort string, check func(t *testing.T, m *Manager, prdID string)) {
	t.Helper()
	for _, streaming := range []bool{false, true} {
		m, p := plannerNoBackendManager(t, plan, rowEffort)
		var err error
		if streaming {
			_, err = m.decomposeStreamingCore(p.ID, func(i, n int, s Story) {})
		} else {
			_, err = m.Decompose(p.ID)
		}
		if err != nil {
			t.Fatalf("streaming=%v decompose: %v", streaming, err)
		}
		check(t, m, p.ID)
	}
}

func TestPlannerNoTaskBackendDecomposeLeavesEveryTaskBackendEmpty(t *testing.T) {
	decomposeBothPaths(t, noBackendPlanJSON, "low", func(t *testing.T, m *Manager, prdID string) {
		prd, tasks := plannedTasks(t, m, prdID)
		if len(tasks) != 2 {
			t.Fatalf("planned %d tasks, want 2", len(tasks))
		}
		for title, tk := range tasks {
			if tk.Backend != "" || tk.Model != "" {
				t.Errorf("task %q got a planner-written LLM: %q/%q", title, tk.Backend, tk.Model)
			}
			// Effort routing still applies.
			if tk.Effort != "low" {
				t.Errorf("task %q effort = %q, want the row's low", title, tk.Effort)
			}
			r := tk.TaskRouting
			if r == nil || !r.Applied || r.Result == nil || r.Result.WorkerBackend != "" || r.Result.WorkerModel != "" {
				t.Errorf("task %q routing record names a worker LLM: %+v", title, r)
			}
		}
		// Nothing the router logged names a worker LLM either.
		for _, d := range prd.Decisions {
			if d.Kind == taskRoutingDecisionKind && (d.Backend != "" || d.Model != "") {
				t.Errorf("routing decision names a worker LLM: %+v", d)
			}
		}

		// Effective-backend resolution falls through to the Automaton.
		m.Store().WithLock(func() { prd.Backend, prd.Model = "claude-code", "sonnet" })
		for title, tk := range tasks {
			b, mod, src := resolveTaskExecutionLLM(tk, storyOf(prd, tk), prd, nil, "global-fallback")
			if b != "claude-code" || mod != "sonnet" || src != ExecutorSourcePRD {
				t.Errorf("task %q resolves to %q/%q (%s), want the Automaton's claude-code/sonnet", title, b, mod, src)
			}
		}
		// A story setting wins over the Automaton's, still with no task field.
		st := storyOf(prd, tasks["Write unit tests for A"])
		st.Backend, st.Model = "pi-prompt", "ollama/story"
		b, mod, src := resolveTaskExecutionLLM(tasks["Write unit tests for A"], st, prd, nil, "global-fallback")
		if b != "pi-prompt" || mod != "ollama/story" || src != ExecutorSourceStory {
			t.Errorf("story setting not inherited: %q/%q (%s)", b, mod, src)
		}
	})
}

func TestPlannerNoTaskBackendAutomationBackendIsInherited(t *testing.T) {
	decomposeBothPaths(t, noBackendPlanJSON, "low", func(t *testing.T, m *Manager, prdID string) {
		prd, tasks := plannedTasks(t, m, prdID)
		auto := &Automation{Backend: "claude-code", Model: "haiku"}
		for title, tk := range tasks {
			b, mod, src := resolveTaskExecutionLLM(tk, storyOf(prd, tk), prd, auto, "global-fallback")
			if b != "claude-code" || mod != "haiku" || src != ExecutorSourceAutomation {
				t.Errorf("task %q resolves to %q/%q (%s), want the Automaton's claude-code/haiku", title, b, mod, src)
			}
		}
		// With nothing set above, the global default applies.
		for title, tk := range tasks {
			if b, _, src := resolveTaskExecutionLLM(tk, storyOf(prd, tk), prd, nil, "global-fallback"); b != "global-fallback" || src != ExecutorSourceGlobal {
				t.Errorf("task %q resolves to %q (%s), want the global default", title, b, src)
			}
		}
	})
}

func TestPlannerNoTaskBackendRowWithOnlyWorkerChoiceChangesNothing(t *testing.T) {
	// The shipped rows name only a worker backend and model: with that dropped,
	// the table has nothing left to write, and the task records why.
	decomposeBothPaths(t, noBackendPlanJSON, "", func(t *testing.T, m *Manager, prdID string) {
		prd, tasks := plannedTasks(t, m, prdID)
		for title, tk := range tasks {
			if tk.Backend != "" || tk.Model != "" || tk.Effort != "" {
				t.Errorf("task %q was written to: %q/%q/%q", title, tk.Backend, tk.Model, tk.Effort)
			}
			r := tk.TaskRouting
			if r == nil || r.Applied || r.Reason != taskRoutingReasonInherits || r.Row == "" {
				t.Errorf("task %q record = %+v, want not applied with the inherit reason", title, r)
			}
		}
		for _, d := range prd.Decisions {
			if d.Kind == taskRoutingDecisionKind {
				t.Errorf("a routing decision was logged though nothing was written: %+v", d)
			}
		}
	})
}

func TestPlannerNoTaskBackendNamedBackendIsKept(t *testing.T) {
	decomposeBothPaths(t, namedBackendPlanJSON, "low", func(t *testing.T, m *Manager, prdID string) {
		_, tasks := plannedTasks(t, m, prdID)
		if len(tasks) != 3 {
			t.Fatalf("planned %d tasks, want 3", len(tasks))
		}
		named := map[string]string{
			"Write unit tests for A": "task JSON",
			"Update the docs for B":  "spec text",
		}
		for title, where := range named {
			tk := tasks[title]
			if tk.Backend != "claude-code" {
				t.Errorf("task %q: backend named in the %s was lost: %q", title, where, tk.Backend)
			}
			if r := tk.TaskRouting; r == nil || r.Applied || r.Reason != taskRoutingReasonTaskLLM {
				t.Errorf("task %q: the table must not decide a named backend: %+v", title, r)
			}
		}
		// The task that names nothing stays empty.
		if tk := tasks["Write regression tests for B"]; tk.Backend != "" || tk.Model != "" {
			t.Errorf("unnamed task got an LLM: %q/%q", tk.Backend, tk.Model)
		}
		// The named backend is the effective one, over the Automaton's.
		prd, _ := m.Store().SnapshotPRD(prdID)
		prd.Backend = "pi-prompt"
		b, _, src := resolveTaskExecutionLLM(tasks["Update the docs for B"], storyOf(prd, tasks["Update the docs for B"]), prd, nil, "global-fallback")
		if b != "claude-code" || src != ExecutorSourceTask {
			t.Errorf("named backend not effective: %q (%s)", b, src)
		}
	})
}

func TestPlannerNoTaskBackendParseBackendLine(t *testing.T) {
	cases := []struct {
		name, spec, want string
		ok               bool
	}{
		{"plain line", "backend: claude-code", "claude-code", true},
		{"among other lines", "Do it.\n  backend: pi-prompt  \nverification_command: true", "pi-prompt", true},
		{"first wins", "backend: a\nbackend: b", "a", true},
		{"no space after colon", "backend:claude-code", "", false},
		{"empty name", "backend: ", "", false},
		{"two words", "backend: claude code", "", false},
		{"inside prose", "use the backend: claude-code here", "", false},
		{"different key", "worker_profile: x", "", false},
		{"wrong case", "Backend: claude-code", "", false},
		{"none", "just a spec", "", false},
	}
	for _, c := range cases {
		got, ok := ParseBackendLine(c.spec)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: ParseBackendLine(%q) = %q, %v; want %q, %v", c.name, c.spec, got, ok, c.want, c.ok)
		}
	}
}

func TestPlannerNoTaskBackendParsedPlanKeepsExistingFieldOverSpecLine(t *testing.T) {
	_, stories, err := ParsePlanning(`{"title":"x","stories":[{"id":"s","title":"s","tasks":[
		{"id":"a","title":"a","spec":"do\nbackend: pi-prompt","backend":"claude-code"},
		{"id":"b","title":"b","spec":"do\nbackend: pi-prompt"},
		{"id":"c","title":"c","spec":"do"}]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{stories[0].Tasks[0].Backend, stories[0].Tasks[1].Backend, stories[0].Tasks[2].Backend}
	want := []string{"claude-code", "pi-prompt", ""}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("task %d backend = %q, want %q", i, got[i], want[i])
		}
	}
}

// storyOf returns the story of prd that holds task t.
func storyOf(prd *PRD, t *Task) *Story {
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == t.ID {
				return &prd.Story[si]
			}
		}
	}
	return nil
}
