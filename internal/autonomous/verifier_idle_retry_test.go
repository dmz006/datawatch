package autonomous

import (
	"context"
	"sync"
	"testing"
	"time"
)

// idleThenPass returns a verify function that fails with a VerifierIdleError
// for the first n calls, then passes. calls reports how many ran.
func idleThenPass(n int) (VerifyFn, func() int) {
	var mu sync.Mutex
	calls := 0
	fn := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		mu.Lock()
		calls++
		c := calls
		mu.Unlock()
		if c <= n {
			return VerificationResult{}, NewVerifierIdleError(15 * time.Minute)
		}
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}
	return fn, func() int { mu.Lock(); defer mu.Unlock(); return calls }
}

// TestVerifierIdleDoesNotCountWorkerAttempt: two consecutive idle verifiers
// are retried on a fresh verifier, leave the worker failure count at zero,
// record no worker attempt, and the task still completes with one spawn.
func TestVerifierIdleDoesNotCountWorkerAttempt(t *testing.T) {
	r := newInfraExecRun(t, nil, nil)
	verify, calls := idleThenPass(2)
	_, task := r.run(t, nil, verify)

	if task.Status != TaskCompleted {
		t.Fatalf("task status = %s, want completed (error %q)", task.Status, task.Error)
	}
	if task.EscalationFailures != 0 {
		t.Errorf("EscalationFailures = %d, want 0", task.EscalationFailures)
	}
	if len(task.Attempts) != 0 {
		t.Errorf("Attempts = %d, want 0 (an idle verifier is not a worker attempt)", len(task.Attempts))
	}
	if n := len(task.VerifierIncidentEvents); n != 2 {
		t.Errorf("VerifierIncidentEvents = %d, want 2", n)
	}
	if calls() != 3 {
		t.Errorf("verify calls = %d, want 3", calls())
	}
	if r.spawned() != 1 {
		t.Errorf("spawns = %d, want 1 (the worker must not rerun)", r.spawned())
	}
}

// TestEscalationFailuresIgnoreVerifierIdle: a verifier that is idle on every
// try uses up the fixed retry cap and holds the task for review, still with
// zero escalation failures and no recorded worker attempt.
func TestEscalationFailuresIgnoreVerifierIdle(t *testing.T) {
	r := newInfraExecRun(t, nil, nil)
	verify, calls := idleThenPass(1 << 30)
	_, task := r.run(t, nil, verify)

	if task.Status != TaskReviewNeeded {
		t.Fatalf("task status = %s, want review_needed", task.Status)
	}
	if task.EscalationFailures != 0 {
		t.Errorf("EscalationFailures = %d, want 0", task.EscalationFailures)
	}
	if len(task.Attempts) != 0 {
		t.Errorf("Attempts = %d, want 0", len(task.Attempts))
	}
	if want := maxVerifierIdleRetries + 1; calls() != want {
		t.Errorf("verify calls = %d, want %d (cap %d retries)", calls(), want, maxVerifierIdleRetries)
	}
	if task.FailureReason == nil || task.FailureReason.Source != verifierIdleHoldSource {
		t.Errorf("FailureReason = %+v, want source %s", task.FailureReason, verifierIdleHoldSource)
	}
}
