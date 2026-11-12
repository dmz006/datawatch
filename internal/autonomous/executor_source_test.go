package autonomous

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type llmCase struct {
	name        string
	task        Task
	story       *Story
	prd         PRD
	auto        *Automation
	fallback    string
	wantBackend string
	wantModel   string
	wantSource  string
}

func TestResolveTaskBackendModelAutomationTier(t *testing.T) {
	auto := &Automation{Backend: "claude-code", Model: "sonnet"}
	cases := []llmCase{
		{name: "task level", task: Task{Backend: "pi-prompt", Model: "m-task"}, story: &Story{Backend: "opencode", Model: "m-story"},
			prd: PRD{Backend: "ollama", Model: "m-prd"}, auto: auto, fallback: "fb",
			wantBackend: "pi-prompt", wantModel: "m-task", wantSource: ExecutorSourceTask},
		{name: "story level", story: &Story{Backend: "opencode", Model: "m-story"},
			prd: PRD{Backend: "ollama", Model: "m-prd"}, auto: auto, fallback: "fb",
			wantBackend: "opencode", wantModel: "m-story", wantSource: ExecutorSourceStory},
		{name: "prd level", story: &Story{}, prd: PRD{Backend: "ollama", Model: "m-prd"}, auto: auto, fallback: "fb",
			wantBackend: "ollama", wantModel: "m-prd", wantSource: ExecutorSourcePRD},
		{name: "automation level", story: &Story{}, prd: PRD{}, auto: auto, fallback: "fb",
			wantBackend: "claude-code", wantModel: "sonnet", wantSource: ExecutorSourceAutomation},
		{name: "automation beats global", prd: PRD{}, auto: &Automation{Backend: "claude-code"}, fallback: "fb",
			wantBackend: "claude-code", wantModel: "", wantSource: ExecutorSourceAutomation},
		{name: "nil automation falls to global", prd: PRD{}, fallback: "fb",
			wantBackend: "fb", wantModel: "", wantSource: ExecutorSourceGlobal},
		{name: "automation without backend falls to global", prd: PRD{}, auto: &Automation{Model: "m-auto"}, fallback: "fb",
			wantBackend: "fb", wantModel: "m-auto", wantSource: ExecutorSourceGlobal},
		{name: "nil automation reproduces old story model fallthrough", story: &Story{Model: "m-story"}, prd: PRD{Model: "m-prd"}, fallback: "fb",
			wantBackend: "fb", wantModel: "m-story", wantSource: ExecutorSourceGlobal},
		{name: "nil automation reproduces old prd model fallthrough", prd: PRD{Model: "m-prd"}, fallback: "fb",
			wantBackend: "fb", wantModel: "m-prd", wantSource: ExecutorSourceGlobal},
		{name: "task model only, backend from automation", task: Task{Model: "m-task"}, prd: PRD{}, auto: auto, fallback: "fb",
			wantBackend: "claude-code", wantModel: "m-task", wantSource: ExecutorSourceAutomation},
		{name: "everything unset", prd: PRD{}, fallback: "",
			wantBackend: "", wantModel: "", wantSource: ExecutorSourceGlobal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, m, s := resolveTaskExecutionLLM(&c.task, c.story, &c.prd, c.auto, c.fallback)
			if b != c.wantBackend || m != c.wantModel || s != c.wantSource {
				t.Fatalf("got (%q, %q, %q), want (%q, %q, %q)", b, m, s, c.wantBackend, c.wantModel, c.wantSource)
			}
			wb, wm := resolveTaskBackendModel(&c.task, c.story, &c.prd, c.auto, c.fallback)
			if wb != b || wm != m {
				t.Fatalf("wrapper got (%q, %q), want (%q, %q)", wb, wm, b, m)
			}
		})
	}
}

func TestResolveTaskBackendModelAutomationLosesToMoreSpecificLevels(t *testing.T) {
	auto := &Automation{Backend: "claude-code", Model: "sonnet"}
	tests := []struct {
		name       string
		task       Task
		story      *Story
		prd        PRD
		wantSource string
	}{
		{"task wins", Task{Backend: "pi-prompt"}, &Story{Backend: "opencode"}, PRD{Backend: "ollama"}, ExecutorSourceTask},
		{"story wins", Task{}, &Story{Backend: "opencode"}, PRD{Backend: "ollama"}, ExecutorSourceStory},
		{"prd wins", Task{}, &Story{}, PRD{Backend: "ollama"}, ExecutorSourcePRD},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			b, m, s := resolveTaskExecutionLLM(&c.task, c.story, &c.prd, auto, "fb")
			if b == auto.Backend || m == auto.Model || s != c.wantSource {
				t.Fatalf("automation leaked through: got (%q, %q, %q), want source %q", b, m, s, c.wantSource)
			}
		})
	}
}

func TestResolveTaskBackendModelAutomationSameLevelModel(t *testing.T) {
	auto := &Automation{Backend: "claude-code", Model: "sonnet"}

	// An Automation model never pairs with another level's backend.
	for name, c := range map[string]struct {
		task  Task
		story *Story
		prd   PRD
	}{
		"task backend":  {Task{Backend: "pi-prompt"}, nil, PRD{}},
		"story backend": {Task{}, &Story{Backend: "opencode"}, PRD{}},
		"prd backend":   {Task{}, nil, PRD{Backend: "ollama"}},
	} {
		_, m, _ := resolveTaskExecutionLLM(&c.task, c.story, &c.prd, auto, "fb")
		if m == "sonnet" {
			t.Errorf("%s: automation model paired with another level's backend", name)
		}
	}

	// With no backend at any level the Automation's own backend is used with
	// its own model.
	b, m, s := resolveTaskExecutionLLM(&Task{}, nil, &PRD{}, auto, "fb")
	if b != "claude-code" || m != "sonnet" || s != ExecutorSourceAutomation {
		t.Fatalf("got (%q, %q, %q)", b, m, s)
	}

	// An Automation with a model but no backend joins the fallthrough only
	// when no level sets a backend, after the story and PRD models.
	modelOnly := &Automation{Model: "m-auto"}
	if _, m, _ := resolveTaskExecutionLLM(&Task{}, &Story{Model: "m-story"}, &PRD{Model: "m-prd"}, modelOnly, "fb"); m != "m-story" {
		t.Errorf("story model should win the fallthrough, got %q", m)
	}
	if _, m, _ := resolveTaskExecutionLLM(&Task{}, &Story{}, &PRD{Model: "m-prd"}, modelOnly, "fb"); m != "m-prd" {
		t.Errorf("prd model should win over the automation model, got %q", m)
	}
	if b, m, s := resolveTaskExecutionLLM(&Task{}, &Story{}, &PRD{}, modelOnly, "fb"); b != "fb" || m != "m-auto" || s != ExecutorSourceGlobal {
		t.Errorf("got (%q, %q, %q), want (fb, m-auto, global)", b, m, s)
	}
	if _, m, _ := resolveTaskExecutionLLM(&Task{}, nil, &PRD{Backend: "ollama"}, modelOnly, "fb"); m != "" {
		t.Errorf("a model-only automation must not pair with the PRD backend, got %q", m)
	}
}

func TestResolveTaskEffortAutomationTier(t *testing.T) {
	auto := &Automation{Effort: EffortHigh}
	if got := resolveTaskEffort(&Task{Effort: EffortLow}, &Story{Effort: EffortNormal}, &PRD{Effort: EffortNormal}, auto); got != EffortLow {
		t.Errorf("task: got %q", got)
	}
	if got := resolveTaskEffort(&Task{}, &Story{Effort: EffortNormal}, &PRD{Effort: EffortLow}, auto); got != EffortNormal {
		t.Errorf("story: got %q", got)
	}
	if got := resolveTaskEffort(&Task{}, &Story{}, &PRD{Effort: EffortLow}, auto); got != EffortLow {
		t.Errorf("prd: got %q", got)
	}
	if got := resolveTaskEffort(&Task{}, &Story{}, &PRD{}, auto); got != EffortHigh {
		t.Errorf("automation: got %q", got)
	}
	if got := resolveTaskEffort(&Task{}, nil, &PRD{}, auto); got != EffortHigh {
		t.Errorf("nil story, automation: got %q", got)
	}
	if got := resolveTaskEffort(&Task{}, nil, &PRD{}, nil); got != "" {
		t.Errorf("none set: got %q, want empty", got)
	}
	if got := resolveTaskEffort(&Task{}, nil, &PRD{}, &Automation{}); got != "" {
		t.Errorf("automation without effort: got %q, want empty", got)
	}
}

func TestExecutorSourceValues(t *testing.T) {
	for got, want := range map[string]string{
		ExecutorSourceTask:       "task",
		ExecutorSourceStory:      "story",
		ExecutorSourcePRD:        "prd",
		ExecutorSourceAutomation: "automation",
		ExecutorSourceGlobal:     "global",
	} {
		if got != want {
			t.Errorf("constant = %q, want %q", got, want)
		}
	}
}

// runOneTaskPRD runs a single-task PRD through the dispatch loop with a fake
// spawn and returns the persisted task plus the request the spawn saw.
func runOneTaskPRD(t *testing.T, m *Manager, prdID string) (*Task, SpawnRequest) {
	t.Helper()
	var req SpawnRequest
	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		req = r
		return SpawnResult{SessionID: "sess-" + r.TaskID}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}
	if err := m.Run(context.Background(), prdID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, ok := m.Store().SnapshotPRD(prdID)
	if !ok || len(got.Story) == 0 || len(got.Story[0].Tasks) == 0 {
		t.Fatalf("prd %s has no task after run", prdID)
	}
	task := got.Story[0].Tasks[0]
	return &task, req
}

func executorSourceFixture(t *testing.T, prdBackend string) (*Manager, *PRD) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.ExecutionBackend = "opencode"
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "/projsrc", prdBackend, "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do it"}}}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return m, prd
}

func TestExecutorSourceRecordedOnTask(t *testing.T) {
	t.Run("automation member with backend", func(t *testing.T) {
		m, prd := executorSourceFixture(t, "")
		if _, err := m.CreateAutomation(AutomationInput{
			Name:    "chain",
			Members: []AutomationMember{{PRDID: prd.ID}},
			Backend: "claude-code",
			Model:   "sonnet",
		}, "operator"); err != nil {
			t.Fatalf("CreateAutomation: %v", err)
		}
		task, req := runOneTaskPRD(t, m, prd.ID)
		if task.ExecutorSource != ExecutorSourceAutomation {
			t.Errorf("ExecutorSource = %q, want automation", task.ExecutorSource)
		}
		if req.Backend != "claude-code" || req.Model != "sonnet" {
			t.Errorf("spawned with %q/%q, want claude-code/sonnet", req.Backend, req.Model)
		}
	})
	t.Run("standalone prd without backend", func(t *testing.T) {
		m, prd := executorSourceFixture(t, "")
		task, req := runOneTaskPRD(t, m, prd.ID)
		if task.ExecutorSource != ExecutorSourceGlobal {
			t.Errorf("ExecutorSource = %q, want global", task.ExecutorSource)
		}
		if req.Backend != "opencode" {
			t.Errorf("spawned with %q, want the fallback opencode", req.Backend)
		}
	})
	t.Run("prd level backend", func(t *testing.T) {
		m, prd := executorSourceFixture(t, "ollama")
		task, req := runOneTaskPRD(t, m, prd.ID)
		if task.ExecutorSource != ExecutorSourcePRD {
			t.Errorf("ExecutorSource = %q, want prd", task.ExecutorSource)
		}
		if req.Backend != "ollama" {
			t.Errorf("spawned with %q, want ollama", req.Backend)
		}
	})
}

func TestExecutorSourceJSONRoundTrip(t *testing.T) {
	b, err := json.Marshal(Task{Title: "t", ExecutorSource: ExecutorSourceAutomation})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"executor_source":"automation"`) {
		t.Fatalf("executor_source missing from %s", b)
	}
	var out Task
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.ExecutorSource != ExecutorSourceAutomation {
		t.Fatalf("round trip got %q", out.ExecutorSource)
	}
	b, err = json.Marshal(Task{Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "executor_source") {
		t.Fatalf("empty executor_source should be omitted: %s", b)
	}
}

func TestPlanningFieldDoesNotAffectExecution(t *testing.T) {
	planOnly := &Automation{PlanningBackend: "pi-prompt", PlanningModel: "qwen3.8:27b"}
	tasks := []struct {
		name  string
		task  Task
		story *Story
		prd   PRD
	}{
		{"nothing set", Task{}, nil, PRD{}},
		{"model fallthrough", Task{}, &Story{Model: "m-story"}, PRD{Model: "m-prd"}},
		{"prd backend", Task{}, &Story{}, PRD{Backend: "ollama", Model: "m"}},
		{"task backend", Task{Backend: "claude-code", Model: "haiku"}, nil, PRD{}},
	}
	for _, c := range tasks {
		t.Run(c.name, func(t *testing.T) {
			wb, wm, ws := resolveTaskExecutionLLM(&c.task, c.story, &c.prd, nil, "fb")
			gb, gm, gs := resolveTaskExecutionLLM(&c.task, c.story, &c.prd, planOnly, "fb")
			if wb != gb || wm != gm || ws != gs {
				t.Fatalf("planning-only automation changed execution: (%q,%q,%q) vs (%q,%q,%q)", gb, gm, gs, wb, wm, ws)
			}
			if got, want := resolveTaskEffort(&c.task, c.story, &c.prd, planOnly), resolveTaskEffort(&c.task, c.story, &c.prd, nil); got != want {
				t.Fatalf("planning-only automation changed effort: %q vs %q", got, want)
			}
		})
	}
}
