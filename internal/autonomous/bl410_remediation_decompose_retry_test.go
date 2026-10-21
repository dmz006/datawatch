// BL410 remediation (2026-10-13) — Manager.Decompose's own infra/scope
// retry, wired via the shared retryDecomposeFailure helper. Flagged
// live: "why was manager.decompose not wired in?" — PRD.LastDecomposeFailure
// existed since Phase 1 but nothing ever set or consulted it; every
// decompose failure rolled straight back to PRDDraft on the very first
// attempt, regardless of category or configured budget. These tests
// cover both Decompose() and decomposeStreamingCore() since they are a
// long-standing drifted-duplicate pair (see their own comments).
package autonomous

import (
	"fmt"
	"strings"
	"testing"
)

func TestDecompose_LLMCallFails_RetriesWithinBudget_ThenSucceeds(t *testing.T) {
	cfg := DefaultConfig() // infra budget = 4 by default
	calls := 0
	var promptSeenOnRetry string
	fn := func(req DecomposeRequest) (string, error) {
		calls++
		if calls < 3 {
			return "", fmt.Errorf("llm timeout attempt %d", calls)
		}
		promptSeenOnRetry = req.Spec
		return `{"title":"T","stories":[{"title":"S","tasks":[{"title":"Task","spec":"do it"}]}]}`, nil
	}
	m, err := NewManager(t.TempDir(), cfg, fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")

	updated, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("Decompose: %v (want success after retries, calls=%d)", err, calls)
	}
	if calls != 3 {
		t.Fatalf("decompose call count = %d, want 3 (1 initial + 2 failed retries before success)", calls)
	}
	if updated.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want needs_review", updated.Status)
	}
	if updated.LastDecomposeFailure != nil {
		t.Fatalf("LastDecomposeFailure should be cleared on success, got %+v", updated.LastDecomposeFailure)
	}
	if !strings.Contains(promptSeenOnRetry, "Your previous attempt failed") {
		t.Fatalf("retry prompt did not include the failure hint: %q", promptSeenOnRetry)
	}
	if !strings.Contains(promptSeenOnRetry, "llm timeout attempt 2") {
		t.Fatalf("retry prompt did not include the PREVIOUS attempt's own detail: %q", promptSeenOnRetry)
	}
}

func TestDecompose_LLMCallFails_ExhaustsBudget_RollsBackToDraft(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RetryPolicy = map[string]int{"infra": 1, "scope": 0} // tight budget for a fast test
	calls := 0
	fn := func(req DecomposeRequest) (string, error) {
		calls++
		return "", fmt.Errorf("llm always down")
	}
	m, err := NewManager(t.TempDir(), cfg, fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")

	_, err = m.Decompose(prd.ID)
	if err == nil {
		t.Fatal("Decompose: want error when the LLM call always fails")
	}
	if calls != 2 {
		t.Fatalf("decompose call count = %d, want 2 (1 initial + 1 retry, budget=1)", calls)
	}
	final, _ := m.Store().GetPRD(prd.ID)
	if final.Status != PRDDraft {
		t.Fatalf("status = %s, want draft (rolled back after exhaustion)", final.Status)
	}
	if final.LastDecomposeFailure == nil {
		t.Fatal("LastDecomposeFailure is nil, want populated with the terminal failure")
	}
	if final.LastDecomposeFailure.Category != "infra" {
		t.Errorf("LastDecomposeFailure.Category = %q, want infra", final.LastDecomposeFailure.Category)
	}
	if final.LastDecomposeFailure.Source != "decompose-llm" {
		t.Errorf("LastDecomposeFailure.Source = %q, want decompose-llm", final.LastDecomposeFailure.Source)
	}
	if final.LastDecomposeFailure.Retryable {
		t.Error("LastDecomposeFailure.Retryable = true, want false (this was the exhaustion point)")
	}
	learnings := m.Store().ListLearnings()
	found := false
	for _, l := range learnings {
		if l.PRDID == prd.ID && l.Category == "infra" {
			found = true
		}
	}
	if !found {
		t.Errorf("want a Learning row for the exhausted infra failure, got %+v", learnings)
	}
}

func TestDecompose_MissingScriptPlan_RetriedWithinContentBudget_ThenRollsBackToDraft(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoFixRetries = 1 // "content" budget — explicit so the call count below is deterministic
	calls := 0
	var retryPrompt string
	fn := func(req DecomposeRequest) (string, error) {
		calls++
		if calls > 1 {
			retryPrompt = req.Spec // the retry attempt's prompt
		}
		return planRespFilesNoScript, nil // real file changes, no verification script → coverage gate rejection, every attempt
	}
	m, err := NewManager(t.TempDir(), cfg, fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")

	_, err = m.Decompose(prd.ID)
	if err == nil {
		t.Fatal("Decompose: want error from validateVerificationScriptCoverage once the content budget is exhausted")
	}
	if calls != 2 {
		t.Fatalf("decompose call count = %d, want 2 (1 initial + 1 retry, content budget=1)", calls)
	}
	// The retry must carry the previous attempt's failure detail (which
	// names the offending task) — the existing LastDecomposeFailure.
	// Detail hint in the retry prompt, no second mechanism needed.
	if !strings.Contains(retryPrompt, "Your previous attempt failed") {
		t.Fatalf("retry prompt did not include the failure hint: %q", retryPrompt)
	}
	if !strings.Contains(retryPrompt, `task "Task 1"`) {
		t.Errorf("retry prompt did not include the offending task named by the previous failure: %q", retryPrompt)
	}
	final, _ := m.Store().GetPRD(prd.ID)
	if final.Status != PRDDraft {
		t.Fatalf("status = %s, want draft (rolled back after exhaustion)", final.Status)
	}
	if final.LastDecomposeFailure == nil || final.LastDecomposeFailure.Category != "content" {
		t.Fatalf("LastDecomposeFailure = %+v, want category content (the coverage branch retries as a content rejection)", final.LastDecomposeFailure)
	}
	if final.LastDecomposeFailure.Source != "verification-script-coverage" {
		t.Errorf("LastDecomposeFailure.Source = %q, want verification-script-coverage", final.LastDecomposeFailure.Source)
	}
}

func TestDecomposeStreamingCore_RetriesLikeDecompose(t *testing.T) {
	cfg := DefaultConfig()
	calls := 0
	fn := func(req DecomposeRequest) (string, error) {
		calls++
		if calls < 2 {
			return "", fmt.Errorf("transient session error")
		}
		return `{"title":"T","stories":[{"title":"S","tasks":[{"title":"Task","spec":"do it"}]}]}`, nil
	}
	m, err := NewManager(t.TempDir(), cfg, fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")

	updated, err := m.decomposeStreamingCore(prd.ID, nil)
	if err != nil {
		t.Fatalf("decomposeStreamingCore: %v", err)
	}
	if calls != 2 {
		t.Fatalf("decompose call count = %d, want 2 (1 initial + 1 retry before success)", calls)
	}
	if updated.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want needs_review", updated.Status)
	}
}
