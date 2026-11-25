package autonomous

// A verifier that could not answer, or whose reply could not be read, says
// nothing about the worker's result: only the verifier runs again. The spawn
// and verify functions are fakes and infraSleep is a recorder.

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// verifyScript returns the scripted results in order, repeating the last one.
// calls reports how many verifications ran.
func verifyScript(results ...VerificationResult) (VerifyFn, func() int) {
	var mu sync.Mutex
	n := 0
	fn := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		mu.Lock()
		defer mu.Unlock()
		i := n
		n++
		if i >= len(results) {
			i = len(results) - 1
		}
		return results[i], nil
	}
	return fn, func() int { mu.Lock(); defer mu.Unlock(); return n }
}

var (
	askFailed  = VerificationResult{Summary: "verifier: ask call failed: HTTP 502", Category: "infra"}
	unreadable = VerificationResult{Summary: "verifier judge returned 12 bytes — its response could not be parsed as a verdict", Category: "invalid-response"}
	passed     = VerificationResult{OK: true, Summary: "ok"}
)

func TestVerifierFault_OneFaultRunsOnlyTheVerifierAgain(t *testing.T) {
	for name, fault := range map[string]VerificationResult{"infrastructure": askFailed, "unreadable reply": unreadable} {
		t.Run(name, func(t *testing.T) {
			r := newInfraExecRun(t, nil, nil)
			verify, calls := verifyScript(fault, passed)
			_, task := r.run(t, nil, verify)
			if r.spawned() != 1 || calls() != 2 {
				t.Errorf("worker runs = %d, verifier runs = %d; want 1 and 2", r.spawned(), calls())
			}
			if task.Status != TaskCompleted {
				t.Errorf("status = %q (error %q), want completed", task.Status, task.Error)
			}
			if len(task.Attempts) != 0 || task.EscalationFailures != 0 || task.RetryCount != 0 {
				t.Errorf("a failure was counted: attempts %d, escalation %d, retry count %d",
					len(task.Attempts), task.EscalationFailures, task.RetryCount)
			}
		})
	}
}

// A verify path that knows the fault is the verifier's wins over the text: a
// verifier session killed before delivery must not run the worker again.
func TestVerifierFault_MarkedVerifierFaultWinsOverMachineryText(t *testing.T) {
	r := newInfraExecRun(t, nil, nil)
	fault := VerificationResult{Summary: "verifier(session): session killed before delivery", Category: "infra", VerifierFault: true}
	verify, calls := verifyScript(fault, passed)
	_, task := r.run(t, nil, verify)
	if r.spawned() != 1 || calls() != 2 || task.Status != TaskCompleted {
		t.Errorf("worker %d, verifier %d, status %q; want 1, 2, completed", r.spawned(), calls(), task.Status)
	}
}

func TestVerifierFault_RetriesUseTheInfraBudgetAndCountResets(t *testing.T) {
	r := newInfraExecRun(t, nil, nil)
	verify, calls := verifyScript(askFailed, unreadable, askFailed, passed)
	_, task := r.run(t, nil, verify)
	if r.spawned() != 1 || calls() != 4 || task.Status != TaskCompleted {
		t.Fatalf("worker %d, verifier %d, status %q; want 1, 4, completed", r.spawned(), calls(), task.Status)
	}
	want := []time.Duration{time.Minute, 5 * time.Minute, 9 * time.Minute}
	if got := r.slept(); !reflect.DeepEqual(got, want) {
		t.Errorf("back-off = %v, want %v", got, want)
	}
	if task.VerifierFaultRetries != 0 || task.InfraRetryCount != 0 || len(task.InfraRetries) != 3 ||
		task.InfraRetries[0].Class != InfraClassVerifierFault {
		t.Errorf("counts %d/%d, records %+v", task.VerifierFaultRetries, task.InfraRetryCount, task.InfraRetries)
	}
}

func TestVerifierFault_UsedUpHoldsWithOneQuestionAndNeverRunsTheWorkerAgain(t *testing.T) {
	cases := map[string]struct {
		adjust    func(*config.InfraRetryConfig)
		verifiers int
	}{
		"default":                {nil, 4},
		"exhausted action count": {func(c *config.InfraRetryConfig) { c.ExhaustedAction = config.ExhaustedActionCount }, 4},
		"infra retry off":        {func(c *config.InfraRetryConfig) { off := false; c.Enabled = &off }, 1},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := newInfraExecRun(t, tc.adjust, nil)
			alerts, store := wireInfraHoldRig(t, r, true)
			verify, calls := verifyScript(unreadable)
			spawn := func(context.Context, SpawnRequest, int) (SpawnResult, error) { return SpawnResult{}, nil }
			got, task := r.run(t, spawn, verify)
			if r.spawned() != 1 || calls() != tc.verifiers {
				t.Errorf("worker %d, verifier %d; want 1 and %d", r.spawned(), calls(), tc.verifiers)
			}
			if task.Status != TaskReviewNeeded || got.Status != PRDBlocked {
				t.Fatalf("task %q, Automaton %q (error %q)", task.Status, got.Status, task.Error)
			}
			fr := task.FailureReason
			if fr == nil || fr.Source != verifierFaultHoldSource || fr.Category != "infra" || fr.Retryable {
				t.Fatalf("FailureReason = %+v", fr)
			}
			// The supervisor loop retries infrastructure holds by itself, and
			// that retry runs the worker.
			if fr.Source == infraRetryHoldSource {
				t.Error("hold uses the source the supervisor loop retries by itself")
			}
			if task.EscalationFailures != 0 || got.RetryCeilingTotal != 0 || task.SessionID != "sess-1" {
				t.Errorf("escalation %d, ceiling %d, session %q", task.EscalationFailures, got.RetryCeilingTotal, task.SessionID)
			}
			if task.ReviewSummary == nil || !strings.Contains(task.ReviewSummary.Text, "could not be parsed") {
				t.Errorf("review summary does not name the cause: %+v", task.ReviewSummary)
			}
			if n := len(alertsTitled(alerts, "Automaton task held: no verdict from the verifier")); n != 1 {
				t.Errorf("hold alerts = %d (all %+v)", n, alerts.all())
			}
			open := store.List(QuestionFilter{State: QuestionOpen})
			if len(open) != 1 {
				t.Fatalf("open questions = %d: %+v", len(open), open)
			}
			q := open[0]
			if q.Default != "hold" || !q.Blocking || q.TaskID != task.ID || len(q.Options) != 2 ||
				!strings.Contains(q.Text, "never judged") {
				t.Errorf("question = %+v", q)
			}
		})
	}
}

// The operator's retry clears the hold and the verifier-only count.
func TestVerifierFault_OperatorRetryClearsTheHold(t *testing.T) {
	r := newInfraExecRun(t, nil, nil)
	verify, _ := verifyScript(askFailed)
	_, held := r.run(t, nil, verify)
	if held.Status != TaskReviewNeeded {
		t.Fatalf("status = %q", held.Status)
	}
	if _, err := r.m.ResolveTaskReview(r.prd.ID, held.ID, ReviewResolution{Action: reviewActionRetry}, "operator"); err != nil {
		t.Fatalf("retry: %v", err)
	}
	_, task := heldTask(t, r)
	if task.FailureReason != nil || task.VerifierFaultRetries != 0 {
		t.Errorf("after retry: reason %+v, verifier retries %d", task.FailureReason, task.VerifierFaultRetries)
	}
}

// A verdict that found a problem, and a result the verify path marks as the
// worker's own fault, still run the worker again.
func TestVerifierFault_FindingsAndWorkerFaultsStillRunTheWorkerAgain(t *testing.T) {
	cases := map[string]VerificationResult{
		"finding":       {Summary: "the change does not implement the spec", Severity: "medium"},
		"harness crash": {Summary: "verifier: harness crashed mid-run; diff empty", Category: "infra", WorkerFault: true},
	}
	for name, first := range cases {
		t.Run(name, func(t *testing.T) {
			r := newInfraExecRun(t, nil, nil)
			verify, calls := verifyScript(first, passed)
			_, task := r.run(t, nil, verify)
			if r.spawned() != 2 || calls() != 2 || task.Status != TaskCompleted {
				t.Errorf("worker %d, verifier %d, status %q; want 2, 2, completed", r.spawned(), calls(), task.Status)
			}
		})
	}
}

// Cancelling during a verifier back-off holds nothing and runs nothing more.
func TestVerifierFault_CancelDuringBackoffStops(t *testing.T) {
	r := newInfraExecRun(t, nil, nil)
	infraSleep = func(context.Context, time.Duration) error {
		if err := r.api.Cancel(r.prd.ID); err != nil {
			t.Errorf("cancel: %v", err)
		}
		return context.Canceled
	}
	verify, calls := verifyScript(askFailed, passed)
	_, task := r.run(t, nil, verify)
	if r.spawned() != 1 || calls() != 1 || task.Status == TaskReviewNeeded || task.Status == TaskCompleted {
		t.Errorf("worker %d, verifier %d, status %q", r.spawned(), calls(), task.Status)
	}
}

// A daemon restart during a back-off verifies again, with no new worker, even
// when the worker left no commit and no completion marker.
func TestVerifierFault_RestartDuringBackoffVerifiesWithoutRunningTheWorker(t *testing.T) {
	dead := func(string) bool { return false }
	mid := &Task{Status: TaskVerifying, SessionID: "sess-old", VerifierFaultRetries: 1}
	if got := ResolveVerifyingResume(dead, nil, nil, nil, mid); got != VerifyingResumeReverify {
		t.Errorf("boot resume = %v, want re-verify", got)
	}
	r := newInfraExecRun(t, nil, func(task *Task) {
		task.Status, task.SessionID, task.VerifierFaultRetries = TaskVerifying, "sess-old", 1
	})
	verify, calls := verifyScript(passed)
	_, task := r.run(t, nil, verify)
	if r.spawned() != 0 || calls() != 1 || task.Status != TaskCompleted {
		t.Errorf("worker %d, verifier %d, status %q; want 0, 1, completed", r.spawned(), calls(), task.Status)
	}
}
