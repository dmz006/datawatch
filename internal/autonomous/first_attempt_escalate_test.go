package autonomous

import (
	"context"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// escalateAfterConfig configures the implementation pair and the escalation
// threshold, so applyFirstAttempt can be driven with a single kind.
func escalateAfterConfig(escalateAfter int) Config {
	cfg := firstAttemptFakeConfig()
	cfg.FirstAttemptEscalateAfterFailures = escalateAfter
	return cfg
}

func TestFirstAttemptEscalated_Thresholds(t *testing.T) {
	// Attempt n runs after n-1 failed attempts. The pair applies while the
	// failure count is below the threshold and stops once it reaches it.
	cases := []struct {
		escalateAfter int
		// pairOnAttempt[n-1] is whether attempt n still uses the pair.
		pairOnAttempt [4]bool
	}{
		{escalateAfter: 1, pairOnAttempt: [4]bool{true, false, false, false}},
		{escalateAfter: 2, pairOnAttempt: [4]bool{true, true, false, false}},
		{escalateAfter: 3, pairOnAttempt: [4]bool{true, true, true, false}},
	}
	for _, c := range cases {
		cfg := escalateAfterConfig(c.escalateAfter)
		task := firstAttemptFakeTask(attemptKindImplementation)
		for attempt := 1; attempt <= 4; attempt++ {
			failed := attempt - 1
			want := c.pairOnAttempt[attempt-1]
			if got := firstAttemptEscalated(failed, c.escalateAfter); got == want {
				t.Fatalf("escalateAfter=%d attempt=%d: firstAttemptEscalated=%v, want %v",
					c.escalateAfter, attempt, got, !want)
			}
			backend, model, source, reason := applyFirstAttempt(cfg, task, failed, "opencode", "m-default", ExecutorSourceGlobal)
			if want {
				if backend != "claude-code" || model != "claude-sonnet-5-5" || source != ExecutorSourceFirstAttempt || reason == "" {
					t.Fatalf("escalateAfter=%d attempt=%d: pair not used: %q %q %q reason=%q",
						c.escalateAfter, attempt, backend, model, source, reason)
				}
			} else if backend != "opencode" || model != "m-default" || source != ExecutorSourceGlobal || reason != "" {
				t.Fatalf("escalateAfter=%d attempt=%d: pair still used after escalation: %q %q %q reason=%q",
					c.escalateAfter, attempt, backend, model, source, reason)
			}
		}
	}
}

func TestFirstAttemptEscalated_UnsetThresholdReadsAsOne(t *testing.T) {
	// Zero (unset) and a negative value both behave as 1: the first failure
	// moves the task off the pair.
	for _, after := range []int{0, -2} {
		if !firstAttemptEscalated(1, after) {
			t.Fatalf("escalateAfter=%d: one failure must escalate", after)
		}
		if firstAttemptEscalated(0, after) {
			t.Fatalf("escalateAfter=%d: zero failures must keep the pair", after)
		}
		task := firstAttemptFakeTask(attemptKindImplementation)
		if _, _, _, ok := firstAttemptChoice(escalateAfterConfig(after), task, 0); !ok {
			t.Fatalf("escalateAfter=%d: first attempt lost its pair", after)
		}
		if _, _, _, ok := firstAttemptChoice(escalateAfterConfig(after), task, 1); ok {
			t.Fatalf("escalateAfter=%d: second attempt kept the pair", after)
		}
	}
}

func TestFirstAttemptEscalated_InfraRetryIsNotCounted(t *testing.T) {
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	task := &prd.Story[0].Tasks[0]
	cfg := escalateAfterConfig(1)
	cfg.FirstAttempt = map[string]config.FirstAttemptChoice{
		taskAttemptKind(task): {Backend: "claude-code", Model: "claude-sonnet-5-5"},
	}

	// An infrastructure retry is recorded as an attempt that did not run the
	// model (Requeue). It must not count as a failed attempt.
	m.recordFailedAttempt(context.Background(), prd, task, attemptRun{}, attemptFailure{
		Outcome: attemptOutcomeFailed, Category: infraRetryFailureCategory, Summary: "worker failed to start", Requeue: true,
	})
	if len(task.Attempts) != 1 {
		t.Fatalf("attempts = %d, want 1 recorded infra retry", len(task.Attempts))
	}
	if n := m.escalationFailureCount(task); n != 0 {
		t.Fatalf("escalationFailureCount after infra retry = %d, want 0", n)
	}
	if backend, _, source, reason := applyFirstAttempt(cfg, task, m.escalationFailureCount(task), "opencode", "", ExecutorSourceGlobal); backend != "claude-code" || source != ExecutorSourceFirstAttempt || reason == "" {
		t.Fatalf("infra retry moved the task off the pair: backend=%q source=%q", backend, source)
	}

	// A real failed attempt is counted, and with threshold 1 the pair stops.
	m.recordFailedAttempt(context.Background(), prd, task, attemptRun{}, attemptFailure{
		Outcome: attemptOutcomeFailed, Category: "verify", Summary: "tests failed",
	})
	if n := m.escalationFailureCount(task); n != 1 {
		t.Fatalf("escalationFailureCount after real failure = %d, want 1", n)
	}
	if backend, _, source, reason := applyFirstAttempt(cfg, task, m.escalationFailureCount(task), "opencode", "", ExecutorSourceGlobal); backend != "opencode" || source != ExecutorSourceGlobal || reason != "" {
		t.Fatalf("real failure did not stop the pair: backend=%q source=%q reason=%q", backend, source, reason)
	}
}

func TestFirstAttemptEscalated_FailureLadderStillAppliesAfterPairStops(t *testing.T) {
	// Fake ladder: step 1 covers one failed attempt, step 2 covers the rest.
	eff := EffectiveFailureEscalation{Enabled: true, MaxFailures: 9, Steps: []FailureEscalationStep{
		{Backend: "claude-code", Model: "claude-haiku-5-5", Attempts: 1},
		{Backend: "pi-prompt", Model: "anthropic/claude-haiku-5-5"},
	}}
	cfg := escalateAfterConfig(1)
	task := firstAttemptFakeTask(attemptKindImplementation)

	// Attempt 1: no failures, the first-attempt pair applies. The ladder is
	// not consulted because the executor only escalates after a failure.
	if backend, _, source, _ := applyFirstAttempt(cfg, task, 0, "opencode", "", ExecutorSourceGlobal); backend != "claude-code" || source != ExecutorSourceFirstAttempt {
		t.Fatalf("attempt 1: pair not used: backend=%q source=%q", backend, source)
	}

	// Attempt 2: one failure, the pair has stopped. The normally resolved
	// worker is kept, and the ladder picks its step for this failure count.
	backend, model, source, reason := applyFirstAttempt(cfg, task, 1, "opencode", "m-default", ExecutorSourceGlobal)
	if backend != "opencode" || model != "m-default" || source != ExecutorSourceGlobal || reason != "" {
		t.Fatalf("attempt 2: pair not stopped: backend=%q model=%q source=%q reason=%q", backend, model, source, reason)
	}
	step, idx, ok := selectEscalationStep(eff, 1)
	if !ok || idx != 1 || step.Backend != "pi-prompt" {
		t.Fatalf("attempt 2: ladder step = %q index=%d ok=%v, want pi-prompt index=1 ok=true", step.Backend, idx, ok)
	}

	// Attempt 3: two failures, the last ladder step still covers the attempt.
	step, idx, ok = selectEscalationStep(eff, 2)
	if !ok || idx != 1 || step.Backend != "pi-prompt" {
		t.Fatalf("attempt 3: ladder step = %q index=%d ok=%v, want pi-prompt index=1 ok=true", step.Backend, idx, ok)
	}
}
