// BL191 Q6 (v5.10.0) — per-story / per-task guardrail invocation tests.

package autonomous

import (
	"context"
	"testing"
)

// fakeGuardrailAlwaysPass returns a green verdict for every invocation.
func fakeGuardrailAlwaysPass(_ context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
	return GuardrailVerdict{Outcome: "pass", Summary: "ok " + req.Level + ":" + req.UnitID}, nil
}

func fakeGuardrailBlocking(_ context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
	return GuardrailVerdict{
		Outcome:  "block",
		Severity: "high",
		Summary:  "blocked at " + req.Level + " level",
		Issues:   []string{"would cause an incident"},
	}, nil
}

func TestPerTaskGuardrails_NoConfigured_NoOp(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PerTaskGuardrails = nil
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)
	m.SetGuardrail(fakeGuardrailBlocking) // would block if invoked

	prd, _ := m.CreatePRD("spec", "/p", "claude", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{
		Title: "S",
		Tasks: []Task{{Title: "T", Spec: "do"}},
	}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	if prd.Status != PRDCompleted {
		t.Fatalf("status = %s, want completed (no guardrails should run)", prd.Status)
	}
	if len(prd.Story[0].Tasks[0].Verdicts) != 0 {
		t.Fatalf("task got verdicts despite empty PerTaskGuardrails")
	}
}

func TestPerTaskGuardrails_AllPass_AppendsVerdicts(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PerTaskGuardrails = []string{"rules", "security"}
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)
	m.SetGuardrail(fakeGuardrailAlwaysPass)

	prd, _ := m.CreatePRD("spec", "/p", "claude", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{
		Title: "S",
		Tasks: []Task{{Title: "T", Spec: "do"}},
	}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	_ = m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify)
	prd, _ = m.Store().GetPRD(prd.ID)

	if prd.Status != PRDCompleted {
		t.Fatalf("status = %s, want completed", prd.Status)
	}
	tk := prd.Story[0].Tasks[0]
	if len(tk.Verdicts) != 2 {
		t.Fatalf("task verdict count = %d, want 2", len(tk.Verdicts))
	}
	for i, name := range []string{"rules", "security"} {
		if tk.Verdicts[i].Guardrail != name {
			t.Fatalf("verdict %d guardrail = %q, want %q", i, tk.Verdicts[i].Guardrail, name)
		}
		if tk.Verdicts[i].Outcome != "pass" {
			t.Fatalf("verdict %d outcome = %q, want pass", i, tk.Verdicts[i].Outcome)
		}
	}
}

func TestPerTaskGuardrails_Block_HaltsPRD(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PerTaskGuardrails = []string{"security"}
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)
	m.SetGuardrail(fakeGuardrailBlocking)

	prd, _ := m.CreatePRD("spec", "/p", "claude", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{
		Title: "S",
		Tasks: []Task{
			{Title: "first", Spec: "do"},
			{Title: "second", Spec: "more"},
		},
	}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	_ = m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify)
	prd, _ = m.Store().GetPRD(prd.ID)

	if prd.Status != PRDBlocked {
		t.Fatalf("status = %s, want blocked", prd.Status)
	}
	first := prd.Story[0].Tasks[0]
	if first.Status != TaskBlocked {
		t.Fatalf("first task = %s, want blocked", first.Status)
	}
	// Second task must NOT have run — block halts the walk.
	second := prd.Story[0].Tasks[1]
	if second.Status == TaskCompleted || second.Status == TaskInProgress {
		t.Fatalf("second task = %s; expected pending (walk should have halted)", second.Status)
	}
}

func TestPerStoryGuardrails_FireAfterAllTasksDone(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PerStoryGuardrails = []string{"docs-diagrams-architecture"}
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)
	m.SetGuardrail(fakeGuardrailAlwaysPass)

	prd, _ := m.CreatePRD("spec", "/p", "claude", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{
		Title: "S",
		Tasks: []Task{
			{Title: "T1", Spec: "a"},
			{Title: "T2", Spec: "b"},
		},
	}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	_ = m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify)
	prd, _ = m.Store().GetPRD(prd.ID)

	if prd.Status != PRDCompleted {
		t.Fatalf("status = %s, want completed", prd.Status)
	}
	if len(prd.Story[0].Verdicts) != 1 {
		t.Fatalf("story verdict count = %d, want 1", len(prd.Story[0].Verdicts))
	}
	if prd.Story[0].Verdicts[0].Guardrail != "docs-diagrams-architecture" {
		t.Fatalf("story verdict name = %q", prd.Story[0].Verdicts[0].Guardrail)
	}
}

func TestPerStoryGuardrails_Block_HaltsPRD(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PerStoryGuardrails = []string{"release-readiness"}
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)
	m.SetGuardrail(fakeGuardrailBlocking)

	prd, _ := m.CreatePRD("spec", "/p", "claude", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{
		Title: "S",
		Tasks: []Task{{Title: "T", Spec: "do"}},
	}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	_ = m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify)
	prd, _ = m.Store().GetPRD(prd.ID)

	if prd.Status != PRDBlocked {
		t.Fatalf("status = %s, want blocked", prd.Status)
	}
	if len(prd.Story[0].Verdicts) != 1 || prd.Story[0].Verdicts[0].Outcome != "block" {
		t.Fatalf("story verdict missing or wrong outcome: %+v", prd.Story[0].Verdicts)
	}
}

func TestPerTaskGuardrails_NoFnWired_SilentNoOp(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PerTaskGuardrails = []string{"rules"}
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)
	// Intentionally no SetGuardrail call.

	prd, _ := m.CreatePRD("spec", "/p", "claude", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{
		Title: "S",
		Tasks: []Task{{Title: "T", Spec: "do"}},
	}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	if prd.Status != PRDCompleted {
		t.Fatalf("status = %s, want completed (guardrail fn nil → silent no-op)", prd.Status)
	}
}
