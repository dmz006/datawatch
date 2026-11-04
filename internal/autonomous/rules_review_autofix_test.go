// Completion rules review auto-fix ordering: the corrective story and task
// are created before any retry budget is spent, and every outcome that
// cannot be fixed automatically holds the Automaton in needs_review.
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

// rulesReviewAutoFixFixture builds a manager whose completion rules review
// blocks for the first blockN reviews (all of them when blockN < 0), then
// passes. verdict customises the block verdict. The returned func runs the
// single-task Automaton and returns it as stored afterwards.
func rulesReviewAutoFixFixture(t *testing.T, cfg Config, blockN int, verdict GuardrailVerdict) (*Manager, func() *PRD, *int) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.VerificationModel = "ollama/qwen3.8:27b"
	m, err := NewManager(t.TempDir(), cfg, fakeDecompose)
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	m.attemptClock = func() time.Time { return fixed }
	reviews := 0
	m.SetGuardrail(func(_ context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		if req.Level != "prd" {
			return GuardrailVerdict{Outcome: "pass"}, nil
		}
		reviews++
		if blockN < 0 || reviews <= blockN {
			return verdict, nil
		}
		return GuardrailVerdict{Outcome: "pass"}, nil
	})
	run := func() *PRD {
		prd, _ := m.CreatePRD("spec", dir, "claude", "", EffortNormal)
		_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}})
		prd, _ = m.Store().GetPRD(prd.ID)
		prd.PerPRDGuardrails = []string{"rules-review"}
		prd.Status = PRDApproved
		_ = m.Store().SavePRD(prd)
		if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err != nil {
			t.Fatalf("Run: %v", err)
		}
		prd, _ = m.Store().GetPRD(prd.ID)
		return prd
	}
	return m, run, &reviews
}

func contentBlock() GuardrailVerdict {
	return GuardrailVerdict{Outcome: "block", Summary: "docs missing", Issues: []string{"update docs/howto/x.md"}}
}

func findRulesReviewDecision(prd *PRD, kind string) *Decision {
	for i := range prd.Decisions {
		if prd.Decisions[i].Kind == kind {
			return &prd.Decisions[i]
		}
	}
	return nil
}

func TestRulesReviewAutoFixBlockWithBudgetCreatesTaskThenRereviews(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoFixRetries = 1
	m, run, reviews := rulesReviewAutoFixFixture(t, cfg, 1, contentBlock())
	var outcomeAtRereview *RulesReviewOutcome
	inner := m.guardrail
	m.SetGuardrail(func(ctx context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		if req.Level == "prd" && *reviews == 1 {
			for _, p := range m.Store().ListPRDs() {
				outcomeAtRereview = p.RulesReviewOutcome
			}
		}
		return inner(ctx, req)
	})
	prd := run()
	if prd.Status != PRDCompleted {
		t.Fatalf("status = %s, want completed", prd.Status)
	}
	if len(prd.Story) != 2 || prd.Story[1].Tasks[0].Status != TaskCompleted {
		t.Fatalf("want a completed corrective story, got %+v", prd.Story)
	}
	if prd.RetryCountsByCategory["content"] != 1 {
		t.Fatalf("content retries = %d, want 1", prd.RetryCountsByCategory["content"])
	}
	if findRulesReviewDecision(prd, "auto_retry") == nil {
		t.Fatal("want an auto_retry decision")
	}
	if *reviews != 2 {
		t.Fatalf("reviews = %d, want 2", *reviews)
	}
	if outcomeAtRereview == nil || outcomeAtRereview.Action != RulesReviewActionFixTaskCreated ||
		outcomeAtRereview.Attempt != 1 || outcomeAtRereview.RetriesSpent != 1 || outcomeAtRereview.RetryBudget != 1 {
		t.Fatalf("outcome while the fix ran = %+v", outcomeAtRereview)
	}
	if !outcomeAtRereview.At.Equal(time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("outcome time = %v, want the fixed clock", outcomeAtRereview.At)
	}
}

func TestRulesReviewAutoFixSynthesisFailureHoldsWithoutSpendingBudget(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoFixRetries = 2
	m, run, _ := rulesReviewAutoFixFixture(t, cfg, -1, contentBlock())
	m.synthesizeFn = func(*PRD, GuardrailVerdict, string) (*Story, *Task, error) {
		return nil, nil, errors.New("store unavailable")
	}
	prd := run()
	if prd.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want needs_review", prd.Status)
	}
	if prd.RetryCountsByCategory["content"] != 0 {
		t.Fatalf("content retries = %d, want 0", prd.RetryCountsByCategory["content"])
	}
	d := findRulesReviewDecision(prd, "rules_review_fix_failed")
	if d == nil || d.Actor != "autonomous" || !strings.Contains(d.Note, "store unavailable") {
		t.Fatalf("fix_failed decision = %+v", d)
	}
	if findRulesReviewDecision(prd, "auto_retry") != nil {
		t.Fatal("no auto_retry decision may be recorded when synthesis failed")
	}
	o := prd.RulesReviewOutcome
	if o == nil || o.Action != RulesReviewActionFixTaskFailed || o.Reason != "store unavailable" {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestRulesReviewAutoFixSpentRetryHoldsNeedsReviewWithFindings(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoFixRetries = 1
	_, run, _ := rulesReviewAutoFixFixture(t, cfg, -1, contentBlock())
	prd := run()
	if prd.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want needs_review", prd.Status)
	}
	d := findRulesReviewDecision(prd, "rules_review_retries_spent")
	if d == nil || !strings.Contains(d.Note, "1 of 1") {
		t.Fatalf("retries_spent decision = %+v", d)
	}
	o := prd.RulesReviewOutcome
	if o == nil || o.Action != RulesReviewActionRetriesSpent || len(o.Findings) != 1 || o.Findings[0] != "update docs/howto/x.md" {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestRulesReviewAutoFixZeroRetriesNeverFixes(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoFixRetries = 0
	cfg.RetryPolicy = nil
	_, run, _ := rulesReviewAutoFixFixture(t, cfg, -1, contentBlock())
	prd := run()
	if prd.Status != PRDNeedsReview || len(prd.Story) != 1 {
		t.Fatalf("status = %s stories = %d, want needs_review with no fix story", prd.Status, len(prd.Story))
	}
	if prd.RetryCountsByCategory["content"] != 0 {
		t.Fatalf("content retries = %d, want 0", prd.RetryCountsByCategory["content"])
	}
	if o := prd.RulesReviewOutcome; o == nil || o.Action != RulesReviewActionRetriesSpent {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestRulesReviewAutoFixTwoRetriesGiveTwoFixTasksThenHold(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoFixRetries = 2
	cfg.RetryPolicy = nil
	_, run, reviews := rulesReviewAutoFixFixture(t, cfg, -1, contentBlock())
	prd := run()
	if prd.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want needs_review", prd.Status)
	}
	if len(prd.Story) != 3 {
		t.Fatalf("stories = %d, want original + 2 fix stories", len(prd.Story))
	}
	if prd.RetryCountsByCategory["content"] != 2 || *reviews != 3 {
		t.Fatalf("retries = %d reviews = %d, want 2 and 3", prd.RetryCountsByCategory["content"], *reviews)
	}
}

func TestRulesReviewAutoFixCeilingReachedHolds(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoFixRetries = 3
	cfg.RetryPolicy = nil
	cfg.RetryCeilingTotal = 1
	_, run, _ := rulesReviewAutoFixFixture(t, cfg, -1, contentBlock())
	prd := run()
	if prd.Status != PRDNeedsReview || len(prd.Story) != 2 {
		t.Fatalf("status = %s stories = %d, want needs_review after one fix", prd.Status, len(prd.Story))
	}
	d := findRulesReviewDecision(prd, "rules_review_retries_spent")
	if d == nil || !strings.Contains(d.Note, "1 of 3") || !strings.Contains(d.Note, "ceiling") {
		t.Fatalf("decision = %+v", d)
	}
}

func TestRulesReviewAutoFixAutoFixDisabledHolds(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ContentAutoFixEnabled = false
	_, run, _ := rulesReviewAutoFixFixture(t, cfg, -1, contentBlock())
	prd := run()
	if prd.Status != PRDNeedsReview || len(prd.Story) != 1 {
		t.Fatalf("status = %s stories = %d", prd.Status, len(prd.Story))
	}
	if findRulesReviewDecision(prd, "rules_review_auto_fix_disabled") == nil {
		t.Fatal("want a rules_review_auto_fix_disabled decision")
	}
	if o := prd.RulesReviewOutcome; o == nil || o.Action != RulesReviewActionAutoFixDisabled {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestRulesReviewAutoFixAwaitingApprovalStoryHolds(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoFixRetries = 1
	cfg.AutoFixSkipApproval = false
	cfg.PerStoryApproval = true
	_, run, _ := rulesReviewAutoFixFixture(t, cfg, -1, contentBlock())
	prd := run()
	if prd.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want needs_review", prd.Status)
	}
	if len(prd.Story) != 2 || prd.Story[1].Status != StoryAwaitingApproval {
		t.Fatalf("want a corrective story awaiting approval, got %+v", prd.Story)
	}
	if findRulesReviewDecision(prd, "rules_review_fix_awaiting_approval") == nil {
		t.Fatal("want an awaiting approval decision")
	}
	if prd.RetryCountsByCategory["content"] != 1 {
		t.Fatalf("content retries = %d, want 1", prd.RetryCountsByCategory["content"])
	}
	if o := prd.RulesReviewOutcome; o == nil || o.Action != RulesReviewActionFixTaskCreated {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestRulesReviewAutoFixInfraCategoryStaysBlocked(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RetryPolicy = map[string]int{"infra": 0}
	_, run, _ := rulesReviewAutoFixFixture(t, cfg, -1,
		GuardrailVerdict{Outcome: "block", Category: "infra", Summary: "judge session failed"})
	prd := run()
	if prd.Status != PRDBlocked {
		t.Fatalf("status = %s, want blocked", prd.Status)
	}
	if len(prd.Story) != 1 || prd.RetryCountsByCategory["content"] != 0 {
		t.Fatalf("an infra block must not create fix work: stories=%d", len(prd.Story))
	}
}

func TestRulesReviewAutoFixPassingRereviewClearsOutcome(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoFixRetries = 1
	_, run, _ := rulesReviewAutoFixFixture(t, cfg, 1, contentBlock())
	prd := run()
	if prd.Status != PRDCompleted {
		t.Fatalf("status = %s, want completed", prd.Status)
	}
	if prd.RulesReviewOutcome != nil {
		t.Fatalf("outcome = %+v, want cleared after a passing review", prd.RulesReviewOutcome)
	}
}
