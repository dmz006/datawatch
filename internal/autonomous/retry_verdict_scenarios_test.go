// Retry-verdict scenarios (v9.2.1 unmodified-retry / three-way retry
// no-output path) — executor-level coverage for the three canned
// VerificationResults the cmd/datawatch three-way path
// (verdictForAlreadyLanded) produces on a retry of a task whose
// per-attempt diff window can be empty:
//
//  1. script exits 0       → pass verdict with UnmodifiedRetry=true;
//     the task completes and the PRD decision log carries the
//     auditable "unmodified_retry_completed" entry.
//  2. script exits non-zero → handled failure carrying the exit code and
//     the script output; the task fails in a single pass (no auto-fix
//     retries) with the script output in task.Error and a populated
//     FailureReason.
//  3. script missing        → OK=false, Category "setup" naming the
//     script path; the task is held (TaskBlocked, never TaskFailed)
//     on the first attempt, consuming no retry budget.
//
// fakeSpawn means no tmux and no real sessions are ever started or
// killed; each scenario's inline fake verify function returns exactly
// the canned result, mirroring bl410_remediation_failurereason_test.go
// and setup_fault_hold_test.go.
package autonomous

import (
	"context"
	"strings"
	"testing"
)

var retryVerdictFakeSpawn = func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
	return SpawnResult{SessionID: "s"}, nil // no tmux, no real sessions
}

// newRetryVerdictPRD wires the standard single-story / single-task PRD in
// the approved state, with content auto-fix retries disabled so a failed
// verification fails in one pass.
func newRetryVerdictPRD(t *testing.T) (*Manager, string) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.AutoFixRetries = 0            // single attempt: no auto-fix retries
	cfg.ContentAutoFixEnabled = false // single attempt, no auto-fix retry noise
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)
	return m, prd.ID
}

func TestRetryVerdictPassingScript_CompletesTheTask(t *testing.T) {
	m, prdID := newRetryVerdictPRD(t)

	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		// Canned "script exits 0" verdict from verdictForAlreadyLanded.
		return VerificationResult{
			OK:              true,
			Severity:        "low",
			Summary:         "verifier: no new changes; work already landed in an earlier attempt and its verification script passed",
			UnmodifiedRetry: true,
		}, nil
	}

	if err := m.Run(context.Background(), prdID, retryVerdictFakeSpawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	prd, _ := m.Store().GetPRD(prdID)
	tk := prd.Story[0].Tasks[0]

	if tk.Status != TaskCompleted {
		t.Fatalf("task status = %s, want completed", tk.Status)
	}
	if tk.Status == TaskBlocked || tk.Status == TaskFailed {
		t.Fatal("a passing script verdict must never block or fail the task")
	}
	found := false
	for _, d := range prd.Decisions {
		if d.Kind == "unmodified_retry_completed" && strings.Contains(d.Note, tk.ID) {
			found = true
		}
	}
	if !found {
		t.Errorf("prd.Decisions has no %q entry naming task %s; decisions = %+v",
			"unmodified_retry_completed", tk.ID, prd.Decisions)
	}
}

func TestRetryVerdictFailingScript_FailsWithScriptOutput(t *testing.T) {
	m, prdID := newRetryVerdictPRD(t)
	const distinctive = "assert: expected green, got red"

	verifyCalls := 0
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		verifyCalls++
		// Canned "script exists but exits non-zero" verdict from
		// verdictForAlreadyLanded, carrying the (truncated) output.
		return VerificationResult{
			OK:       false,
			Severity: "medium",
			Summary:  `verifier: no new changes were detected, and the task's own verification script "scripts/verify.sh" failed with exit code 1 (work did not land). Script output follows in issues.`,
			Issues: []string{
				`verification script "scripts/verify.sh" exited 1 (no new changes to verify after retry)`,
				"script output:\n" + distinctive,
			},
		}, nil
	}

	if err := m.Run(context.Background(), prdID, retryVerdictFakeSpawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if verifyCalls != 1 {
		t.Errorf("verify calls = %d, want 1 (single attempt: auto-fix retries disabled)", verifyCalls)
	}
	prd, _ := m.Store().GetPRD(prdID)
	tk := prd.Story[0].Tasks[0]

	if tk.Status != TaskFailed {
		t.Fatalf("task status = %s, want failed", tk.Status)
	}
	if !strings.Contains(tk.Error, distinctive) {
		t.Errorf("task.Error = %q, want it to contain the script output line %q", tk.Error, distinctive)
	}
	if tk.FailureReason == nil {
		t.Fatal("Task.FailureReason is nil, want populated")
	}
	if tk.FailureReason.Source != "task-exhausted" {
		t.Errorf("FailureReason.Source = %q, want task-exhausted", tk.FailureReason.Source)
	}
}

func TestRetryVerdictMissingScript_HoldsTheTask(t *testing.T) {
	m, prdID := newRetryVerdictPRD(t)
	const scriptPath = "scripts/missing-verify.sh"

	verifyCalls := 0
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		verifyCalls++
		// Canned "script missing" verdict from verdictForAlreadyLanded.
		return VerificationResult{
			OK:       false,
			Severity: "medium",
			Category: "setup",
			Summary: "verifier: SETUP FAULT — verification script \"" + scriptPath +
				"\" is missing from the worktree; restore that file before re-verifying. " +
				"This is a missing-script setup fault, NOT a 'task produced no output' judgment",
			Issues: []string{"missing verification script: " + scriptPath + " (script not found)"},
		}, nil
	}

	if err := m.Run(context.Background(), prdID, retryVerdictFakeSpawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if verifyCalls != 1 {
		t.Errorf("verify calls = %d, want 1 (the single attempt must NOT be retried)", verifyCalls)
	}
	prd, _ := m.Store().GetPRD(prdID)
	tk := prd.Story[0].Tasks[0]

	if tk.Status != TaskBlocked {
		t.Fatalf("task status = %s, want blocked", tk.Status)
	}
	if tk.Status == TaskFailed {
		t.Fatal("a missing-script setup fault must never produce TaskFailed")
	}
	// Single attempt only: AutoFixRetries=0 → attempt 0, no respawn.
	if tk.RetryCount != 0 {
		t.Errorf("task.RetryCount = %d, want 0 (the failed attempt's value, not retried)", tk.RetryCount)
	}
	if !strings.Contains(tk.Error, scriptPath) {
		t.Errorf("task.Error = %q, want it to name the missing script %q", tk.Error, scriptPath)
	}
	if tk.FailureReason == nil {
		t.Fatal("Task.FailureReason is nil, want populated")
	}
	if tk.FailureReason.Category != "setup" {
		t.Errorf("FailureReason.Category = %q, want setup", tk.FailureReason.Category)
	}
}
