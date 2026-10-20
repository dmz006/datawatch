// Exit-silence retry coverage for the executor's ErrWorkerStalled branch
// (internal/autonomous/executor.go): a verify() error wrapping
// ErrWorkerStalled — which ExitSilenceError is — must retry through the
// normal auto_fix_retries budget and record the stall reason on the task,
// exactly like a session-idle or SSE stall (see
// TestExecutor_WorkerStallRespectsRetryBudget for the plain-stall twin of
// these tests).
package autonomous

import (
	"context"
	"strings"
	"testing"
)

const exitSilenceReason = "pi print run exited without a completion or verdict"

// ExitSilenceRetry — first attempt stalls (pi print run exited silently),
// verifier is called exactly twice, the stall reason is recorded on the
// task after attempt 1, and the task completes on the retry.
func TestExitSilenceRetry_StallThenRecover(t *testing.T) {
	m, api, _, _ := apiFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "write docs/x.md"}}}})

	spawns := 0
	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		spawns++
		return SpawnResult{SessionID: "s-" + r.TaskID}, nil
	}
	verifyCalls := 0
	var errAfterFirst string
	verify := func(_ context.Context, _ *PRD, tk *Task) (VerificationResult, error) {
		verifyCalls++
		if verifyCalls == 1 {
			return VerificationResult{}, ExitSilenceError(exitSilenceReason)
		}
		// Capture what the executor recorded after the stalled attempt.
		errAfterFirst = tk.Error
		return VerificationResult{OK: true, Summary: "good"}, nil
	}
	got := runToTerminal(t, m, api, prd.ID, spawn, verify)
	if got.Status != PRDCompleted {
		t.Fatalf("an exit-silence stall that recovers must complete, got %s", got.Status)
	}
	if tk := got.Story[0].Tasks[0]; tk.Status != TaskCompleted {
		t.Fatalf("task must end completed, got %s", tk.Status)
	}
	if verifyCalls != 2 {
		t.Fatalf("verify must be called exactly twice, got %d", verifyCalls)
	}
	if spawns != 2 {
		t.Fatalf("want 2 spawns (initial + 1 retry after exit-silence stall), got %d", spawns)
	}
	if !strings.Contains(errAfterFirst, exitSilenceReason) {
		t.Fatalf("after the first attempt the task Error should contain %q, got %q", exitSilenceReason, errAfterFirst)
	}
}

// ExitSilenceRetry — a persistent exit-silence stall must consume the
// auto_fix_retries budget and end the task failed (not stuck verifying),
// classified as "infra" with the stall reason recorded.
func TestExitSilenceRetry_ExhaustsRetriesAndFailsInfra(t *testing.T) {
	m, api, _, _ := apiFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "write docs/x.md"}}}})

	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "s-" + r.TaskID}, nil
	}
	verify := func(context.Context, *PRD, *Task) (VerificationResult, error) {
		return VerificationResult{}, ExitSilenceError(exitSilenceReason)
	}
	got := runToTerminal(t, m, api, prd.ID, spawn, verify)
	if got.Status != PRDFailed {
		t.Fatalf("a persistently exit-silent task must fail once the budget is spent, got PRD %s", got.Status)
	}
	tk := got.Story[0].Tasks[0]
	if tk.Status == TaskVerifying {
		t.Fatal("task is stuck in verifying instead of a terminal status")
	}
	if tk.Status != TaskFailed {
		t.Fatalf("task must end failed, got %s", tk.Status)
	}
	if !strings.Contains(tk.Error, exitSilenceReason) {
		t.Fatalf("task Error should contain %q, got %q", exitSilenceReason, tk.Error)
	}
	if tk.FailureReason == nil {
		t.Fatal("task has no FailureReason")
	}
	if tk.FailureReason.Category != "infra" {
		t.Fatalf("FailureReason.Category = %q, want infra", tk.FailureReason.Category)
	}
	if !strings.Contains(tk.FailureReason.Detail, exitSilenceReason) {
		t.Fatalf("FailureReason.Detail should contain %q, got %q", exitSilenceReason, tk.FailureReason.Detail)
	}
}
