// BL410 remediation (2026-10-13) — Task.FailureReason/Story.FailureReason
// population. Both fields existed since Phase 1 but were never set
// anywhere (flagged live: "what about task.failurereason/
// story.failurereason?"). These tests prove the same data
// recordFailureLearning already writes to a Learning row also lands on
// the unit's own FailureReason field, structured instead of a bare
// string, additive alongside the existing Task.Error.
package autonomous

import (
	"context"
	"testing"
)

func TestFailureReason_TaskGuardrailBlock_Populated(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PerTaskGuardrails = []string{"security"}
	cfg.ContentAutoFixEnabled = false // single attempt, no auto-fix retry noise
	// v9.10.18 — a configured judge model (tier-4 global verification
	// model) so the fake blocking guardrail is actually reachable; the
	// test asserts its verdict, not a resolution-failure block.
	cfg.VerificationModel = "ollama/qwen3.8:27b"
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)
	m.SetGuardrail(fakeGuardrailBlocking)

	prd, _ := m.CreatePRD("spec", "/p", "claude", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	_ = m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify)
	prd, _ = m.Store().GetPRD(prd.ID)

	tk := prd.Story[0].Tasks[0]
	if tk.Status != TaskBlocked {
		t.Fatalf("task status = %s, want blocked", tk.Status)
	}
	if tk.FailureReason == nil {
		t.Fatal("Task.FailureReason is nil, want populated")
	}
	if tk.FailureReason.Category != "content" {
		t.Errorf("FailureReason.Category = %q, want content (fakeGuardrailBlocking's verdict has no Category)", tk.FailureReason.Category)
	}
	if tk.FailureReason.Source != "guardrail-task-block" {
		t.Errorf("FailureReason.Source = %q, want guardrail-task-block", tk.FailureReason.Source)
	}
	if tk.FailureReason.Retryable {
		t.Error("FailureReason.Retryable = true, want false (budget already exhausted at this point)")
	}
	if tk.FailureReason.OccurredAt.IsZero() {
		t.Error("FailureReason.OccurredAt is zero")
	}
}

func TestFailureReason_StoryGuardrailBlock_Populated(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PerStoryGuardrails = []string{"release-readiness"}
	cfg.ContentAutoFixEnabled = false // single attempt, mirrors ContentAutoFixDisabled_SingleAttempt
	// v9.10.18 — a configured judge model (tier-4 global verification
	// model) so the fake blocking guardrail is actually reachable.
	cfg.VerificationModel = "ollama/qwen3.8:27b"
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)
	m.SetGuardrail(fakeGuardrailBlocking)

	prd, _ := m.CreatePRD("spec", "/p", "claude", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	_ = m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify)
	prd, _ = m.Store().GetPRD(prd.ID)

	if prd.Status != PRDBlocked {
		t.Fatalf("status = %s, want blocked", prd.Status)
	}
	story := prd.Story[0]
	if story.FailureReason == nil {
		t.Fatal("Story.FailureReason is nil, want populated")
	}
	if story.FailureReason.Category != "content" {
		t.Errorf("FailureReason.Category = %q, want content", story.FailureReason.Category)
	}
	if story.FailureReason.Source != "guardrail-story-block" {
		t.Errorf("FailureReason.Source = %q, want guardrail-story-block", story.FailureReason.Source)
	}
}

func TestFailureReason_TaskExhausted_VerificationFailure_Populated(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoFixRetries = 1 // only one retry allowed, mirrors TestPRDE2E_RetryExhaustedFails
	m, _ := NewManager(t.TempDir(), cfg, nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "s"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: false, Summary: "still broken", Issues: []string{"x stays broken"}}, nil
	}

	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	final, _ := m.Store().GetPRD(prd.ID)
	tk := final.Story[0].Tasks[0]
	if tk.Status != TaskFailed {
		t.Fatalf("task status = %s, want failed", tk.Status)
	}
	if tk.FailureReason == nil {
		t.Fatal("Task.FailureReason is nil, want populated")
	}
	if tk.FailureReason.Source != "task-exhausted" {
		t.Errorf("FailureReason.Source = %q, want task-exhausted", tk.FailureReason.Source)
	}
	// vr.Category is empty on VerificationResult{OK:false} with no
	// explicit Category set, so orContentCategory normalizes it to
	// "content" — the same convention Learning.Category already uses.
	if tk.FailureReason.Category != "content" {
		t.Errorf("FailureReason.Category = %q, want content", tk.FailureReason.Category)
	}
}

func TestFailureReason_SucceedsOnRetry_StaysNil(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoFixRetries = 2
	m, _ := NewManager(t.TempDir(), cfg, nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	calls := 0
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "s"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		calls++
		if calls == 1 {
			return VerificationResult{OK: false, Summary: "fixable"}, nil
		}
		return VerificationResult{OK: true}, nil
	}

	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	final, _ := m.Store().GetPRD(prd.ID)
	tk := final.Story[0].Tasks[0]
	if tk.Status != TaskCompleted {
		t.Fatalf("task status = %s, want completed", tk.Status)
	}
	if tk.FailureReason != nil {
		t.Errorf("a transient failure that self-healed must leave FailureReason nil, got %+v", tk.FailureReason)
	}
}
