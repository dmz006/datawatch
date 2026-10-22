// Per-task / per-story / per-Automaton verifier LLM overrides:
// resolution precedence (task → story → Automaton → global), the
// same-level backend/model pairing rule, independent effort, the
// presence-aware setters, and JSON round-trip of the new fields.

package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
)

func verifGlobalCfg() Config {
	return Config{VerificationBackend: "ollama", VerificationModel: "qwen-global", VerificationEffort: "low"}
}

// verifPRD builds an Automaton with story s1 → task t1 and story s2 → task t2.
func verifPRD() *PRD {
	return &PRD{
		ID: "p1",
		Story: []Story{
			{ID: "s1", Tasks: []Task{{ID: "t1"}}},
			{ID: "s2", Tasks: []Task{{ID: "t2"}}},
		},
	}
}

func verifTask(prd *PRD, id string) *Task {
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == id {
				return &prd.Story[si].Tasks[ti]
			}
		}
	}
	return nil
}

func assertVerifLLM(t *testing.T, got, want VerificationLLM) {
	t.Helper()
	if got != want {
		t.Fatalf("resolved = %+v; want %+v", got, want)
	}
}

func TestResolveVerificationLLM_TaskOverride(t *testing.T) {
	prd := verifPRD()
	task := verifTask(prd, "t1")
	task.VerificationBackend, task.VerificationModel, task.VerificationEffort = "claude-code", "opus", EffortHigh
	assertVerifLLM(t, ResolveVerificationLLM(prd, task, nil, verifGlobalCfg()),
		VerificationLLM{Backend: "claude-code", Model: "opus", Effort: "high", Source: VerifierSourceTask})
}

func TestResolveVerificationLLM_StoryOverride(t *testing.T) {
	prd := verifPRD()
	prd.Story[0].VerificationBackend, prd.Story[0].VerificationModel, prd.Story[0].VerificationEffort = "claude-code", "sonnet", EffortMedium
	assertVerifLLM(t, ResolveVerificationLLM(prd, verifTask(prd, "t1"), nil, verifGlobalCfg()),
		VerificationLLM{Backend: "claude-code", Model: "sonnet", Effort: "medium", Source: VerifierSourceStory})
	// A task in a different story does not inherit s1's override.
	assertVerifLLM(t, ResolveVerificationLLM(prd, verifTask(prd, "t2"), nil, verifGlobalCfg()),
		VerificationLLM{Backend: "ollama", Model: "qwen-global", Effort: "low", Source: VerifierSourceGlobal})
}

func TestResolveVerificationLLM_PRDOverride(t *testing.T) {
	prd := verifPRD()
	prd.VerificationBackend, prd.VerificationModel, prd.VerificationEffort = "opencode", "glm", EffortMax
	for _, id := range []string{"t1", "t2"} {
		assertVerifLLM(t, ResolveVerificationLLM(prd, verifTask(prd, id), nil, verifGlobalCfg()),
			VerificationLLM{Backend: "opencode", Model: "glm", Effort: "max", Source: VerifierSourcePRD})
	}
}

func TestResolveVerificationLLM_GlobalFallback(t *testing.T) {
	prd := verifPRD()
	assertVerifLLM(t, ResolveVerificationLLM(prd, verifTask(prd, "t1"), nil, verifGlobalCfg()),
		VerificationLLM{Backend: "ollama", Model: "qwen-global", Effort: "low", Source: VerifierSourceGlobal})
	// Nil task and empty global config still resolve, with Source global.
	assertVerifLLM(t, ResolveVerificationLLM(prd, nil, nil, Config{}),
		VerificationLLM{Source: VerifierSourceGlobal})
}

func TestResolveVerificationLLM_TaskBeatsStoryBeatsPRD(t *testing.T) {
	cases := []struct {
		name               string
		task, story, prdLv bool
		want               VerificationLLM
	}{
		{"all set", true, true, true, VerificationLLM{"tb", "tm", "high", VerifierSourceTask}},
		{"story+prd", false, true, true, VerificationLLM{"sb", "sm", "medium", VerifierSourceStory}},
		{"prd only", false, false, true, VerificationLLM{"pb", "pm", "max", VerifierSourcePRD}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prd := verifPRD()
			task := verifTask(prd, "t1")
			if tc.task {
				task.VerificationBackend, task.VerificationModel, task.VerificationEffort = "tb", "tm", EffortHigh
			}
			if tc.story {
				prd.Story[0].VerificationBackend, prd.Story[0].VerificationModel, prd.Story[0].VerificationEffort = "sb", "sm", EffortMedium
			}
			if tc.prdLv {
				prd.VerificationBackend, prd.VerificationModel, prd.VerificationEffort = "pb", "pm", EffortMax
			}
			assertVerifLLM(t, ResolveVerificationLLM(prd, task, nil, verifGlobalCfg()), tc.want)
		})
	}
}

func TestResolveVerificationLLM_ModelNeverPairedAcrossLevels(t *testing.T) {
	cases := []struct {
		name  string
		setup func(prd *PRD, task *Task)
		want  VerificationLLM
	}{
		{
			// Task overrides backend with no model: the story's and
			// Automaton's models must not leak onto the task's backend.
			"task backend, no task model",
			func(prd *PRD, task *Task) {
				task.VerificationBackend = "claude-code"
				prd.Story[0].VerificationModel = "story-model"
				prd.VerificationBackend, prd.VerificationModel = "ollama", "qwen"
			},
			VerificationLLM{Backend: "claude-code", Model: "", Effort: "low", Source: VerifierSourceTask},
		},
		{
			// Story backend wins: the Automaton's model is not paired with it.
			"story backend, prd model ignored",
			func(prd *PRD, task *Task) {
				prd.Story[0].VerificationBackend = "claude-code"
				prd.VerificationBackend, prd.VerificationModel = "ollama", "qwen"
			},
			VerificationLLM{Backend: "claude-code", Model: "", Effort: "low", Source: VerifierSourceStory},
		},
		{
			// Global backend: the global model is not used when the
			// Automaton picked its own backend.
			"prd backend, global model ignored",
			func(prd *PRD, task *Task) { prd.VerificationBackend = "claude-code" },
			VerificationLLM{Backend: "claude-code", Model: "", Effort: "low", Source: VerifierSourcePRD},
		},
		{
			// Matches resolveTaskBackendModel: a more specific model-only
			// override applies to the backend inherited from below.
			"task model only rides story backend",
			func(prd *PRD, task *Task) {
				task.VerificationModel = "sonnet"
				prd.Story[0].VerificationBackend, prd.Story[0].VerificationModel = "claude-code", "opus"
			},
			VerificationLLM{Backend: "claude-code", Model: "sonnet", Effort: "low", Source: VerifierSourceStory},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prd := verifPRD()
			task := verifTask(prd, "t1")
			tc.setup(prd, task)
			assertVerifLLM(t, ResolveVerificationLLM(prd, task, nil, verifGlobalCfg()), tc.want)
		})
	}
}

func TestResolveVerificationLLM_EffortIndependent(t *testing.T) {
	prd := verifPRD()
	task := verifTask(prd, "t1")
	task.VerificationBackend = "claude-code"
	prd.Story[0].VerificationEffort = EffortThorough
	got := ResolveVerificationLLM(prd, task, nil, verifGlobalCfg())
	if got.Source != VerifierSourceTask || got.Effort != "thorough" {
		t.Fatalf("got %+v; want task backend with story effort", got)
	}
	// Effort set at task level while the backend comes from the Automaton.
	prd2 := verifPRD()
	task2 := verifTask(prd2, "t1")
	task2.VerificationEffort = EffortQuick
	prd2.VerificationBackend = "opencode"
	got = ResolveVerificationLLM(prd2, task2, nil, verifGlobalCfg())
	if got.Source != VerifierSourcePRD || got.Effort != "quick" {
		t.Fatalf("got %+v; want prd backend with task effort", got)
	}
}

func TestResolveVerificationLLM_LowercaseAlias(t *testing.T) {
	prd := verifPRD()
	if resolveVerificationLLM(prd, verifTask(prd, "t1"), nil, verifGlobalCfg()) != ResolveVerificationLLM(prd, verifTask(prd, "t1"), nil, verifGlobalCfg()) {
		t.Fatal("resolveVerificationLLM and ResolveVerificationLLM disagree")
	}
}

// ── setters ────────────────────────────────────────────────────────────

func newVerifManager(t *testing.T) (*Manager, *PRD) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("test spec", "/tmp/ts-verif-llm", "ollama", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	prd.Story = []Story{{ID: "s1", Title: "story", Tasks: []Task{{ID: "t1", Title: "task"}}}}
	prd.Status = PRDNeedsReview
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return m, prd
}

func hasDecision(prd *PRD, kind string) bool {
	for _, d := range prd.Decisions {
		if d.Kind == kind {
			return true
		}
	}
	return false
}

func TestSetVerificationLLM_TaskStoryPRD_PersistAndRecordDecision(t *testing.T) {
	m, prd := newVerifManager(t)
	if _, err := m.SetPRDVerificationLLM(prd.ID, strp("opencode"), strp("glm"), strp("max"), "test"); err != nil {
		t.Fatalf("SetPRDVerificationLLM: %v", err)
	}
	if _, err := m.SetStoryVerificationLLM(prd.ID, "s1", strp("claude-code"), strp("sonnet"), strp("medium"), "test"); err != nil {
		t.Fatalf("SetStoryVerificationLLM: %v", err)
	}
	if _, err := m.SetTaskVerificationLLM(prd.ID, "t1", strp("claude-code"), strp("opus"), strp("high"), "test"); err != nil {
		t.Fatalf("SetTaskVerificationLLM: %v", err)
	}
	stored, _ := m.Store().GetPRD(prd.ID)
	if stored.VerificationBackend != "opencode" || stored.VerificationModel != "glm" || stored.VerificationEffort != EffortMax {
		t.Fatalf("prd level = %q/%q/%q", stored.VerificationBackend, stored.VerificationModel, stored.VerificationEffort)
	}
	s := stored.Story[0]
	if s.VerificationBackend != "claude-code" || s.VerificationModel != "sonnet" || s.VerificationEffort != EffortMedium {
		t.Fatalf("story level = %q/%q/%q", s.VerificationBackend, s.VerificationModel, s.VerificationEffort)
	}
	tk := s.Tasks[0]
	if tk.VerificationBackend != "claude-code" || tk.VerificationModel != "opus" || tk.VerificationEffort != EffortHigh {
		t.Fatalf("task level = %q/%q/%q", tk.VerificationBackend, tk.VerificationModel, tk.VerificationEffort)
	}
	for _, k := range []string{"set_prd_verification_llm", "set_story_verification_llm", "set_task_verification_llm"} {
		if !hasDecision(stored, k) {
			t.Errorf("missing decision %q", k)
		}
	}
	// Worker and guardrail fields are untouched.
	if tk.Backend != "" || tk.GuardrailBackend != "" || stored.GuardrailBackend != "" {
		t.Errorf("verifier setters leaked into worker/guardrail fields: %+v", tk)
	}
}

func TestSetTaskVerificationLLM_ClearFallsBackToStory(t *testing.T) {
	m, prd := newVerifManager(t)
	if _, err := m.SetStoryVerificationLLM(prd.ID, "s1", strp("claude-code"), strp("sonnet"), strp("medium"), "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetTaskVerificationLLM(prd.ID, "t1", strp("opencode"), strp("glm"), strp("high"), "test"); err != nil {
		t.Fatal(err)
	}
	// nil leaves fields unchanged.
	if _, err := m.SetTaskVerificationLLM(prd.ID, "t1", nil, nil, nil, "test"); err != nil {
		t.Fatal(err)
	}
	stored, _ := m.Store().GetPRD(prd.ID)
	if got := ResolveVerificationLLM(stored, &stored.Story[0].Tasks[0], nil, verifGlobalCfg()); got.Source != VerifierSourceTask || got.Backend != "opencode" {
		t.Fatalf("after nil update got %+v; want task override intact", got)
	}
	// Pointer to "" clears and falls back to the story.
	if _, err := m.SetTaskVerificationLLM(prd.ID, "t1", strp(""), strp(""), strp(""), "test"); err != nil {
		t.Fatal(err)
	}
	stored, _ = m.Store().GetPRD(prd.ID)
	assertVerifLLM(t, ResolveVerificationLLM(stored, &stored.Story[0].Tasks[0], nil, verifGlobalCfg()),
		VerificationLLM{Backend: "claude-code", Model: "sonnet", Effort: "medium", Source: VerifierSourceStory})
}

func TestSetVerificationLLM_RejectsLockedStatusAndInvalidEffort(t *testing.T) {
	m, prd := newVerifManager(t)
	setters := map[string]func(effort *string) error{
		"task": func(e *string) error {
			_, err := m.SetTaskVerificationLLM(prd.ID, "t1", strp("claude-code"), nil, e, "test")
			return err
		},
		"story": func(e *string) error {
			_, err := m.SetStoryVerificationLLM(prd.ID, "s1", strp("claude-code"), nil, e, "test")
			return err
		},
		"prd": func(e *string) error {
			_, err := m.SetPRDVerificationLLM(prd.ID, strp("claude-code"), nil, e, "test")
			return err
		},
	}
	for name, set := range setters {
		if err := set(strp("ludicrous")); err == nil || !strings.Contains(err.Error(), "invalid verification effort") {
			t.Errorf("%s: invalid effort err = %v", name, err)
		}
		if err := set(strp("")); err != nil {
			t.Errorf("%s: empty effort should be allowed, got %v", name, err)
		}
	}
	for _, st := range []PRDStatus{PRDRunning, PRDCompleted, PRDApproved} {
		p, _ := m.Store().GetPRD(prd.ID)
		p.Status = st
		if err := m.Store().SavePRD(p); err != nil {
			t.Fatal(err)
		}
		for name, set := range setters {
			if err := set(nil); err == nil || !strings.Contains(err.Error(), "locked") {
				t.Errorf("%s in %s: err = %v; want locked", name, st, err)
			}
		}
	}
	if _, err := m.SetTaskVerificationLLM(prd.ID, "nope", nil, nil, nil, "test"); err == nil {
		t.Error("unknown task should error")
	}
	if _, err := m.SetStoryVerificationLLM(prd.ID, "nope", nil, nil, nil, "test"); err == nil {
		t.Error("unknown story should error")
	}
}

func TestVerificationLLMFields_JSONRoundTrip(t *testing.T) {
	in := PRD{
		ID: "p1", VerificationBackend: "pb", VerificationModel: "pm", VerificationEffort: EffortMax,
		Story: []Story{{
			ID: "s1", VerificationBackend: "sb", VerificationModel: "sm", VerificationEffort: EffortMedium,
			Tasks: []Task{{
				ID: "t1", VerificationBackend: "tb", VerificationModel: "tm", VerificationEffort: EffortHigh,
				Verification: &VerificationResult{
					OK: true, VerifierBackend: "tb", VerifierModel: "tm", VerifierEffort: "high", VerifierSource: VerifierSourcePool,
				},
			}},
		}},
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"verification_backend":"pb"`, `"verification_model":"sm"`, `"verification_effort":"high"`,
		`"verifier_backend":"tb"`, `"verifier_model":"tm"`, `"verifier_effort":"high"`, `"verifier_source":"pool"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("marshaled JSON missing %s", key)
		}
	}
	var out PRD
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.VerificationBackend != "pb" || out.VerificationModel != "pm" || out.VerificationEffort != EffortMax {
		t.Errorf("prd fields lost: %+v", out)
	}
	s := out.Story[0]
	if s.VerificationBackend != "sb" || s.VerificationModel != "sm" || s.VerificationEffort != EffortMedium {
		t.Errorf("story fields lost: %+v", s)
	}
	tk := s.Tasks[0]
	if tk.VerificationBackend != "tb" || tk.VerificationModel != "tm" || tk.VerificationEffort != EffortHigh {
		t.Errorf("task fields lost: %+v", tk)
	}
	if v := tk.Verification; v == nil || v.VerifierBackend != "tb" || v.VerifierModel != "tm" || v.VerifierEffort != "high" || v.VerifierSource != VerifierSourcePool {
		t.Errorf("verification result = %+v", tk.Verification)
	}
	// Empty fields are omitted.
	raw, _ = json.Marshal(Task{ID: "t"})
	if strings.Contains(string(raw), "verification_") {
		t.Errorf("empty task marshaled verification keys: %s", raw)
	}
}
