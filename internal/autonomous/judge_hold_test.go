package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func judgeTimeoutVerdict() GuardrailVerdict {
	return GuardrailVerdict{Outcome: "block", Severity: "high", Category: JudgeTimeoutCategory, Summary: "timed out on every backend"}
}

func TestIsJudgeTimeoutHoldOnlyForTimeoutBlocks(t *testing.T) {
	if !IsJudgeTimeoutHold(judgeTimeoutVerdict()) {
		t.Fatal("a judge-timeout block must be a hold")
	}
	if IsJudgeTimeoutHold(GuardrailVerdict{Outcome: "block", Category: "infra"}) {
		t.Fatal("an infra block is not a judge hold")
	}
	if IsJudgeTimeoutHold(GuardrailVerdict{Outcome: "pass", Category: JudgeTimeoutCategory}) {
		t.Fatal("a pass is not a judge hold")
	}
}

func TestRulesReviewBlockStatusHoldsJudgeTimeoutAsNeedsReview(t *testing.T) {
	prd := &PRD{Verdicts: []GuardrailVerdict{judgeTimeoutVerdict()}}
	status, reason := rulesReviewBlockStatus(prd)
	if status != PRDNeedsReview || reason != "judge_timeout_hold" {
		t.Fatalf("judge timeout -> %s/%s, want needs_review/judge_timeout_hold", status, reason)
	}
	prd = &PRD{Verdicts: []GuardrailVerdict{{Outcome: "block", Category: "content", Summary: "rule broken"}}}
	if status, _ := rulesReviewBlockStatus(prd); status != PRDBlocked {
		t.Fatalf("content block -> %s, want blocked", status)
	}
}

func TestPRDHoldReasonHoldsJudgeTimeoutOnlyWhileNeedsReview(t *testing.T) {
	held := &PRD{Status: PRDNeedsReview, Verdicts: []GuardrailVerdict{judgeTimeoutVerdict()}}
	if PRDHoldReason(held, nil) == "" {
		t.Fatal("a needs-review PRD held by its judge must not be started by lane fill")
	}
	approved := &PRD{Status: PRDApproved, Verdicts: []GuardrailVerdict{judgeTimeoutVerdict()}}
	if reason := PRDHoldReason(approved, nil); reason != "" {
		t.Fatalf("an operator-approved PRD is not held by lane fill, got %q", reason)
	}
}

func TestRunPRDCompletionRulesReview_JudgeTimeoutIsBlockedWithHoldCategory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newRulesReviewManager(t, Config{VerificationModel: "ollama/qwen3.8:27b"})
	m.SetGuardrail(func(ctx context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		return judgeTimeoutVerdict(), nil
	})
	prd := &PRD{ID: "p1", ProjectDir: dir, PerPRDGuardrails: []string{"rules-review"}}
	blocked, fix, err := m.runPRDCompletionRulesReview(context.Background(), prd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !blocked || fix != nil {
		t.Fatalf("a judge hold must stop the rollup (blocked=%v fix=%v), not auto-fix", blocked, fix)
	}
	if status, _ := rulesReviewBlockStatus(prd); status != PRDNeedsReview {
		t.Fatalf("rollup status = %s, want needs_review", status)
	}
}

func heldPlanReviewVerdict() *PlanReviewVerdict {
	return &PlanReviewVerdict{
		Held: true, Severity: "high", Summary: "plan review timed out on every backend",
		Findings: []PlanFinding{{Category: JudgeTimeoutCategory, Detail: "backend qwen: timed out"}},
	}
}

func TestPRDHoldReasonHoldsHeldPlanReviewWhileNeedsReview(t *testing.T) {
	held := &PRD{Status: PRDNeedsReview, PlanReview: heldPlanReviewVerdict()}
	if PRDHoldReason(held, nil) == "" {
		t.Fatal("a needs-review PRD whose plan review was held must not be started by lane fill")
	}
	approved := &PRD{Status: PRDApproved, PlanReview: heldPlanReviewVerdict()}
	if reason := PRDHoldReason(approved, nil); reason != "" {
		t.Fatalf("an operator-approved PRD is not held by lane fill, got %q", reason)
	}
}

func TestPlanReviewHeldVerdictBlocksApprovalInBlockModeUntilRerun(t *testing.T) {
	held := true
	m := newPlanReviewManager(t, planReviewConfig("block"), func(ctx context.Context, req PlanReviewRequest) (PlanReviewVerdict, error) {
		if held {
			return *heldPlanReviewVerdict(), nil
		}
		return PlanReviewVerdict{Ok: true, Summary: "looks fine"}, nil
	})
	prd, _ := m.CreatePRD("spec", "", "", "", "")
	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if _, err := m.Approve(prd.ID, "op", ""); err == nil || !strings.Contains(err.Error(), JudgeTimeoutCategory) {
		t.Fatalf("Approve err = %v, want the held plan review to refuse approval", err)
	}
	held = false
	if _, err := m.ReviewPlan(prd.ID, "operator"); err != nil {
		t.Fatalf("ReviewPlan: %v", err)
	}
	if _, err := m.Approve(prd.ID, "op", ""); err != nil {
		t.Fatalf("Approve after a clearing re-run: %v", err)
	}
}
