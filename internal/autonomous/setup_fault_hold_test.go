// setup-fault verdict (three-way retry no-output path) — a 'setup'
// Category on a failed verification is an environment fault, not a task
// fault. The executor must hold the task (TaskBlocked) on the FIRST
// attempt, populate the structured FailureReason, consume none of the
// auto_fix_retries budget, and never reach TaskFailed.
package autonomous

import (
	"context"
	"testing"
)

func TestSetupFault_VerdictHoldsTaskInsteadOfRetryOrFail(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoFixRetries = 3 // budget the setup-fault path must NOT consume
	m, _ := NewManager(t.TempDir(), cfg, nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	spawnCalls, verifyCalls := 0, 0
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		spawnCalls++
		return SpawnResult{SessionID: "s"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		verifyCalls++
		return VerificationResult{
			OK:       false,
			Category: "setup",
			Summary:  "setup fault: required script run-rules.sh is missing at /home/user/.config/imap-mcp/run-rules",
		}, nil
	}

	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	final, _ := m.Store().GetPRD(prd.ID)
	tk := final.Story[0].Tasks[0]

	if tk.Status != TaskBlocked {
		t.Fatalf("task status = %s, want blocked (never failed)", tk.Status)
	}
	if tk.Status == TaskFailed {
		t.Fatal("a setup-fault verdict must never produce TaskFailed")
	}
	// Not a single retry: the budget must be untouched.
	if verifyCalls != 1 {
		t.Errorf("verify calls = %d, want 1 (no auto-fix retry for a setup fault)", verifyCalls)
	}
	if spawnCalls != 1 {
		t.Errorf("spawn calls = %d, want 1 (no respawn after a setup fault)", spawnCalls)
	}
	if want := "setup fault: required script run-rules.sh is missing at /home/user/.config/imap-mcp/run-rules"; tk.Error != want {
		t.Errorf("task.Error = %q, want the verdict summary (naming the missing script path)", tk.Error)
	}
	if tk.FailureReason == nil {
		t.Fatal("Task.FailureReason is nil, want populated")
	}
	if tk.FailureReason.Category != "setup" {
		t.Errorf("FailureReason.Category = %q, want setup", tk.FailureReason.Category)
	}
	if tk.FailureReason.Source != "setup-fault" {
		t.Errorf("FailureReason.Source = %q, want setup-fault", tk.FailureReason.Source)
	}
	if tk.FailureReason.Retryable {
		t.Error("FailureReason.Retryable = true, want false")
	}
	if tk.FailureReason.OccurredAt.IsZero() {
		t.Error("FailureReason.OccurredAt is zero, want now")
	}
	if tk.FailureReason.Detail != tk.Error {
		t.Errorf("FailureReason.Detail = %q, want the same summary as task.Error", tk.FailureReason.Detail)
	}
	if final.Status != PRDBlocked {
		t.Errorf("PRD status = %s, want blocked (walk halts like other blocked paths)", final.Status)
	}
}
