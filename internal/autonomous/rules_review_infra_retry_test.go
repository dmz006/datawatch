package autonomous

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const infraJudgeMsg = "task delivery failed: main prompt never appeared within 1m0s across 3 attempts"

func noInfraSleep(t *testing.T) {
	t.Helper()
	old := infraSleep
	infraSleep = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { infraSleep = old })
}

// A judge that cannot start once and then returns a clean verdict is retried:
// no block is recorded, the Automaton is not blocked, and the final review is
// the clean verdict.
func TestRulesReviewInfraFailureThenCleanVerdictIsRetriedNotBlocked(t *testing.T) {
	noInfraSleep(t)
	m, run, _ := rulesReviewAutoFixFixture(t, DefaultConfig(), 0, GuardrailVerdict{})
	var (
		calls       int
		triesAtCall []int
		prdID       string
		key         string
	)
	m.SetGuardrail(func(_ context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		if req.Level != "prd" {
			return GuardrailVerdict{Outcome: "pass"}, nil
		}
		prdID, key = req.PRDID, judgeCouldNotStartTryKey(req)
		triesAtCall = append(triesAtCall, len(m.JudgeTries(prdID, key)))
		calls++
		if calls == 1 {
			if p, ok := m.Store().GetPRD(prdID); ok {
				for _, v := range p.Verdicts {
					if v.Guardrail == "rules-review" {
						t.Errorf("verdict recorded before the retry: %+v", v)
					}
				}
			}
			return GuardrailVerdict{}, errors.New(infraJudgeMsg)
		}
		return GuardrailVerdict{Outcome: "pass", Summary: "clean"}, nil
	})

	prd := run()
	if calls != 2 {
		t.Fatalf("judge calls = %d, want 2 (second try runs)", calls)
	}
	if prd.Status == PRDBlocked || prd.Status == PRDNeedsReview {
		t.Fatalf("status = %s, want not blocked or held", prd.Status)
	}
	var reviews []GuardrailVerdict
	for _, v := range prd.Verdicts {
		if v.Guardrail == "rules-review" {
			reviews = append(reviews, v)
		}
	}
	if len(reviews) != 1 || reviews[0].Outcome != "pass" || reviews[0].Summary != "clean" {
		t.Fatalf("rules-review verdicts = %+v, want one clean pass", reviews)
	}
	for _, v := range prd.Verdicts {
		if v.Outcome == "block" {
			t.Fatalf("block verdict recorded: %+v", v)
		}
	}
	// The tries record: empty at the first call, holding the failed try at the
	// second, cleared once the review passes.
	if len(triesAtCall) != 2 || triesAtCall[0] != 0 || triesAtCall[1] != 1 {
		t.Fatalf("tries seen per call = %v, want [0 1]", triesAtCall)
	}
	if left := m.JudgeTries(prdID, key); len(left) != 0 {
		t.Fatalf("tries after a clean verdict = %v, want cleared", left)
	}
}

// A judge that cannot start on any try ends as an infrastructure hold: an
// error, no verdict, no finding, and the Automaton is not blocked on one.
func TestRulesReviewInfraFailureEveryTryIsHoldNotBlock(t *testing.T) {
	noInfraSleep(t)
	m, _, _ := rulesReviewAutoFixFixture(t, DefaultConfig(), 0, GuardrailVerdict{})
	calls := 0
	m.SetGuardrail(func(_ context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		if req.Level != "prd" {
			return GuardrailVerdict{Outcome: "pass"}, nil
		}
		calls++
		return GuardrailVerdict{}, errors.New(infraJudgeMsg)
	})
	prd, _ := m.CreatePRD("spec", t.TempDir(), "claude", "", EffortNormal)
	prd.PerPRDGuardrails = []string{"rules-review"}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	prd.ProjectDir = dir
	_ = m.Store().SavePRD(prd)

	blocked, fix, err := m.runPRDCompletionRulesReview(context.Background(), prd)
	if blocked || fix != nil {
		t.Fatalf("blocked=%v fix=%v, want an infrastructure hold, not a block", blocked, fix)
	}
	if err == nil || !strings.Contains(err.Error(), "main prompt never appeared") {
		t.Fatalf("err = %v, want the infrastructure error", err)
	}
	if want := m.infraRetryConfig().EffectiveMaxTries() + 1; calls != want {
		t.Fatalf("judge calls = %d, want %d", calls, want)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	for _, v := range got.Verdicts {
		if v.Guardrail == "rules-review" || v.Outcome == "block" || len(v.Issues) > 0 {
			t.Fatalf("verdict recorded for an infrastructure failure: %+v", v)
		}
	}
	if got.Status == PRDBlocked {
		t.Fatalf("status = %s, want not blocked", got.Status)
	}
	if got.RulesReviewOutcome != nil {
		t.Fatalf("rules review outcome = %+v, want none (no finding)", got.RulesReviewOutcome)
	}
}
