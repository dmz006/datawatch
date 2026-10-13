// BL410 Phase 2 — content auto-fix integration tests. Exercises the
// full Run() flow (not just the resolve functions Phase 1 already
// covers), proving auto-fix actually happens unattended: a content-
// category block retries itself (task level) or synthesizes
// corrective work on the SAME PRD (story/PRD level), never a new one.
package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestContentAutoFix_TaskLevel_RetriesWithHintThenPasses(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PerTaskGuardrails = []string{"release-readiness"}
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)

	calls := 0
	var seenHints []string
	m.SetGuardrail(func(_ context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		calls++
		if calls == 1 {
			return GuardrailVerdict{Outcome: "block", Summary: "missing version bump", Issues: []string{"bump var Version"}}, nil
		}
		return GuardrailVerdict{Outcome: "pass"}, nil
	})

	prd, _ := m.CreatePRD("spec", "/p", "claude", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{
		Title: "S",
		Tasks: []Task{{Title: "T", Spec: "do"}},
	}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	spawnWithHintCapture := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		seenHints = append(seenHints, req.RetryHint)
		return SpawnResult{SessionID: "ses_" + req.TaskID}, nil
	}

	if err := m.Run(context.Background(), prd.ID, spawnWithHintCapture, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	if prd.Status != PRDCompleted {
		t.Fatalf("status = %s, want completed (auto-fix should have resolved the block)", prd.Status)
	}
	task := prd.Story[0].Tasks[0]
	if task.Status != TaskCompleted {
		t.Fatalf("task status = %s, want completed", task.Status)
	}
	if calls != 2 {
		t.Fatalf("want 2 guardrail calls (1 block + 1 auto-fix retry that passed), got %d", calls)
	}
	if len(seenHints) != 2 || seenHints[1] == "" {
		t.Fatalf("want the second spawn to carry a non-empty RetryHint from the guardrail's own findings, got %+v", seenHints)
	}
	if prd.RetryCountsByCategory["content"] != 1 {
		t.Fatalf("want 1 consumed content retry, got %d", prd.RetryCountsByCategory["content"])
	}
}

func TestContentAutoFix_TaskLevel_BudgetExhaustedStillBlocks(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PerTaskGuardrails = []string{"release-readiness"}
	cfg.RetryPolicy = map[string]int{"content": 1} // tight budget
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)

	m.SetGuardrail(func(_ context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		return GuardrailVerdict{Outcome: "block", Summary: "never actually fixed"}, nil
	})

	prd, _ := m.CreatePRD("spec", "/p", "claude", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	_ = m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify)
	prd, _ = m.Store().GetPRD(prd.ID)
	if prd.Status != PRDBlocked {
		t.Fatalf("status = %s, want blocked once the content budget is exhausted", prd.Status)
	}
	if prd.Story[0].Tasks[0].Status != TaskBlocked {
		t.Fatalf("task status = %s, want blocked", prd.Story[0].Tasks[0].Status)
	}
}

func TestContentAutoFix_PRDLevel_SynthesizesOnSamePRD_NeverANewOne(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)

	calls := 0
	m.SetGuardrail(func(_ context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		calls++
		if req.Level != "prd" {
			return GuardrailVerdict{Outcome: "pass"}, nil
		}
		if calls <= 1 {
			return GuardrailVerdict{Outcome: "block", Summary: "missing capability docs", Issues: []string{"update docs/howto/x.md"}}, nil
		}
		return GuardrailVerdict{Outcome: "pass"}, nil
	})

	prd, _ := m.CreatePRD("spec", dir, "claude", "", EffortNormal)
	prd.PerPRDGuardrails = []string{"rules-review"}
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.PerPRDGuardrails = []string{"rules-review"}
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	beforeID := prd.ID
	if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	if prd.ID != beforeID {
		t.Fatalf("PRD ID changed (%s -> %s) — a new PRD must never be created for retry", beforeID, prd.ID)
	}
	if prd.Status != PRDCompleted {
		t.Fatalf("status = %s, want completed (the synthesized fix should have resolved the block)", prd.Status)
	}
	if len(prd.Story) != 2 {
		t.Fatalf("want 2 stories (1 original + 1 synthesized corrective story), got %d", len(prd.Story))
	}
	synthesized := prd.Story[1]
	if synthesized.Tasks[0].Status != TaskCompleted {
		t.Fatalf("synthesized task status = %s, want completed", synthesized.Tasks[0].Status)
	}
	if len(prd.Verdicts) != 2 || prd.Verdicts[0].Outcome != "block" || prd.Verdicts[1].Outcome != "pass" {
		t.Fatalf("want [block, pass] PRD verdicts (auto-fix attempt then success), got %+v", prd.Verdicts)
	}
}
