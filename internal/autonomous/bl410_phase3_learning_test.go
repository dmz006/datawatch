// BL410 Phase 3 — Learning writer revival tests. store.AddLearning
// already existed (BL411's own "fully plumbed read side, zero
// writers" finding); this proves recordFailureLearning actually calls
// it, exactly once per genuinely exhausted failure, and never for a
// transient one that self-heals on a later retry.
package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLearning_TaskExhaustedBudget_WritesExactlyOneRow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PerTaskGuardrails = []string{"release-readiness"}
	cfg.RetryPolicy = map[string]int{"content": 1}
	cfg.VerificationModel = "ollama/qwen3.8:27b" // v9.10.18 judge model for the fake fn
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)
	m.SetGuardrail(func(_ context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		return GuardrailVerdict{Outcome: "block", Summary: "never actually fixed", Issues: []string{"x"}}, nil
	})

	prd, _ := m.CreatePRD("spec", "/p", "claude", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	_ = m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify)

	learnings := m.Store().ListLearnings()
	if len(learnings) != 1 {
		t.Fatalf("want exactly 1 Learning row, got %d: %+v", len(learnings), learnings)
	}
	if learnings[0].PRDID != prd.ID || learnings[0].TaskID == "" || learnings[0].Category != "content" {
		t.Fatalf("Learning row = %+v, want prd=%s content category, non-empty task id", learnings[0], prd.ID)
	}
}

func TestLearning_SucceedsOnRetry_WritesZeroRows(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PerTaskGuardrails = []string{"release-readiness"}
	cfg.VerificationModel = "ollama/qwen3.8:27b" // v9.10.18 judge model for the fake fn
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)
	calls := 0
	m.SetGuardrail(func(_ context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		calls++
		if calls == 1 {
			return GuardrailVerdict{Outcome: "block", Summary: "fixable", Issues: []string{"x"}}, nil
		}
		return GuardrailVerdict{Outcome: "pass"}, nil
	})

	prd, _ := m.CreatePRD("spec", "/p", "claude", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if learnings := m.Store().ListLearnings(); len(learnings) != 0 {
		t.Fatalf("a transient failure that self-healed must write zero Learning rows, got %d: %+v", len(learnings), learnings)
	}
}

func TestLearning_PRDLevelRulesReviewExhausted_WritesRow(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.RetryPolicy = map[string]int{"content": 1}
	cfg.VerificationModel = "ollama/qwen3.8:27b" // v9.10.18 judge model for the fake fn
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)
	m.SetGuardrail(func(_ context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		if req.Level != "prd" {
			return GuardrailVerdict{Outcome: "pass"}, nil
		}
		return GuardrailVerdict{Outcome: "block", Summary: "never actually fixed", Issues: []string{"x"}}, nil
	})

	prd, _ := m.CreatePRD("spec", dir, "claude", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.PerPRDGuardrails = []string{"rules-review"}
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	_ = m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify)
	prd, _ = m.Store().GetPRD(prd.ID)
	if prd.Status != PRDBlocked {
		t.Fatalf("status = %s, want blocked once the content budget is exhausted", prd.Status)
	}

	learnings := m.Store().ListLearnings()
	found := false
	for _, l := range learnings {
		if l.PRDID == prd.ID && l.TaskID == prd.ID && l.Category == "content" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a PRD-level Learning row (TaskID==PRDID for a PRD-wide finding), got %+v", learnings)
	}
}
