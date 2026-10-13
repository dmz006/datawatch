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

func TestDecompose_ScopeValidationFailure_NoRetryByDefault(t *testing.T) {
	cfg := DefaultConfig() // scope budget = 0 by default — never retried
	calls := 0
	fn := func(req DecomposeRequest) (string, error) {
		calls++
		return planRespFilesNoScript, nil // real file changes, no verification script → B124 scope violation
	}
	m, err := NewManager(t.TempDir(), cfg, fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")

	_, err = m.Decompose(prd.ID)
	if err == nil {
		t.Fatal("Decompose: want error from validateVerificationScriptCoverage")
	}
	if calls != 1 {
		t.Fatalf("decompose call count = %d, want 1 (scope category defaults to budget=0, no retry)", calls)
	}
	final, _ := m.Store().GetPRD(prd.ID)
	if final.Status != PRDDraft {
		t.Fatalf("status = %s, want draft", final.Status)
	}
	if final.LastDecomposeFailure == nil || final.LastDecomposeFailure.Category != "scope" {
		t.Fatalf("LastDecomposeFailure = %+v, want category scope", final.LastDecomposeFailure)
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
