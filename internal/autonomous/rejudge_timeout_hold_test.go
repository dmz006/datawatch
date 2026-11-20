package autonomous

import (
	"context"
	"strings"
	"testing"
	"time"
)

// newTimeoutHoldPRD returns a needs_review Automaton, all tasks completed,
// whose newest block verdict is a judge timeout hold judged on a head.
func newTimeoutHoldPRD(t *testing.T) (*Manager, *PRD) {
	t.Helper()
	m, prd, _ := newRejudgeBlockedPRD(t, TaskCompleted)
	setRejudgeHeadCommit(t, m, prd.ID, "sha-head-1")
	fresh, _ := m.Store().GetPRD(prd.ID)
	fresh.Verdicts = append(fresh.Verdicts, GuardrailVerdict{
		Outcome: "block", Category: JudgeTimeoutCategory, Guardrail: "rules-review",
		Summary: "judge timed out", JudgedCommitSHA: "sha-head-1",
		VerdictAt: time.Now().Add(-time.Hour),
	})
	fresh.Status = PRDNeedsReview
	if err := m.Store().SavePRD(fresh); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	return m, got
}

func newTimeoutJudge(t *testing.T) *rejudgeJudge {
	j := newRejudgeBlockJudge(t)
	j.v.Category = JudgeTimeoutCategory
	j.v.Summary = "judge timed out again"
	return j
}

func TestRejudgeTimeoutHoldAccepted(t *testing.T) {
	m, prd := newTimeoutHoldPRD(t)
	j := newRejudgePassJudge(t)
	m.SetGuardrail(j.fn)
	got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.Status != PRDCompleted || j.calls != 1 {
		t.Fatalf("status=%s calls=%d, want completed and 1 call", got.Status, j.calls)
	}
}

func TestRejudgeTimeoutHoldSameHeadReplays(t *testing.T) {
	m, prd := newTimeoutHoldPRD(t)
	j := newRejudgePassJudge(t)
	m.SetGuardrail(j.fn)
	_, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err != nil && strings.Contains(err.Error(), "unchanged") {
		t.Fatalf("same head refused: %v", err)
	}
	if j.calls != 1 {
		t.Fatalf("judge calls = %d, want 1", j.calls)
	}
}

func TestRejudgeTimeoutHoldStillTimedOut(t *testing.T) {
	m, prd := newTimeoutHoldPRD(t)
	j := newTimeoutJudge(t)
	m.SetGuardrail(j.fn)
	nv := len(prd.Verdicts)
	_, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want needs_review", got.Status)
	}
	if len(got.Verdicts) != nv+1 || !IsJudgeTimeoutHold(got.Verdicts[len(got.Verdicts)-1]) {
		t.Fatalf("want a fresh timeout hold verdict, got %d verdicts", len(got.Verdicts))
	}
}

func TestRejudgeTimeoutHoldOtherReviewStillRefused(t *testing.T) {
	m, prd, _ := newRejudgeBlockedPRD(t, TaskCompleted)
	fresh, _ := m.Store().GetPRD(prd.ID)
	fresh.Status = PRDNeedsReview
	_ = m.Store().SavePRD(fresh)
	j := newRejudgePassJudge(t)
	m.SetGuardrail(j.fn)
	_, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err == nil || !strings.Contains(err.Error(), "judge timeout hold") || j.calls != 0 {
		t.Fatalf("err = %v calls=%d, want refusal naming the timeout hold", err, j.calls)
	}
}

func TestRunTimeoutHoldReplays(t *testing.T) {
	m, prd := newTimeoutHoldPRD(t)
	j := newRejudgePassJudge(t)
	m.SetGuardrail(j.fn)
	if err := m.Run(context.Background(), prd.ID, nil, nil); err != nil {
		t.Fatalf("Run err = %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status != PRDCompleted || j.calls != 1 {
		t.Fatalf("status=%s calls=%d", got.Status, j.calls)
	}

	m2, p2, _ := newRejudgeBlockedPRD(t, TaskCompleted)
	f2, _ := m2.Store().GetPRD(p2.ID)
	f2.Status = PRDNeedsReview
	_ = m2.Store().SavePRD(f2)
	err := m2.Run(context.Background(), p2.ID, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "not runnable") {
		t.Fatalf("err = %v, want not runnable", err)
	}
}
