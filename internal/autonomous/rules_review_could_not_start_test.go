package autonomous

import (
	"context"
	"errors"
	"testing"
	"time"
)

const couldNotStartMsg = "task delivery failed: main prompt never appeared within 2m0s across 3 attempts"

func couldNotStartManager(t *testing.T, policy []string) *Manager {
	t.Helper()
	cfg := DefaultConfig()
	cfg.VerificationModel = "ollama/qwen3.8:27b"
	cfg.JudgePolicy = policy
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)
	return m
}

func TestRulesReviewJudgeCouldNotStartRetriesNextBackend(t *testing.T) {
	old := infraSleep
	infraSleep = func(context.Context, time.Duration) error { return nil }
	defer func() { infraSleep = old }()

	m := couldNotStartManager(t, []string{"claude-code", "pi-prompt"})
	prd, _ := bl451RunningPRD(t, m, []string{"rules-review"}, TaskCompleted)
	var backends []string
	fn := func(_ context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		backends = append(backends, req.GuardrailBackend)
		if len(backends) == 1 {
			return GuardrailVerdict{}, errors.New(couldNotStartMsg)
		}
		return GuardrailVerdict{Outcome: "pass"}, nil
	}
	inv := GuardrailInvocation{PRDID: prd.ID, Level: "prd", UnitID: prd.ID, Guardrail: "rules-review", GuardrailBackend: "claude-code"}
	v, err := m.runRulesReviewJudge(context.Background(), prd, inv, fn)
	if err != nil || v.Outcome != "pass" {
		t.Fatalf("want pass, got %+v err=%v", v, err)
	}
	if len(backends) != 2 || backends[1] != "pi-prompt" {
		t.Fatalf("backends = %v, want retry on pi-prompt", backends)
	}
	if left := m.JudgeTries(prd.ID, judgeCouldNotStartTryKey(inv)); len(left) != 0 {
		t.Fatalf("tries not cleared: %v", left)
	}
}

func TestRulesReviewJudgeCouldNotStartSameBackendThenGivesUp(t *testing.T) {
	var delays []time.Duration
	old := infraSleep
	infraSleep = func(_ context.Context, d time.Duration) error { delays = append(delays, d); return nil }
	defer func() { infraSleep = old }()

	m := couldNotStartManager(t, nil)
	prd, _ := bl451RunningPRD(t, m, []string{"rules-review"}, TaskCompleted)
	calls := 0
	fn := func(context.Context, GuardrailInvocation) (GuardrailVerdict, error) {
		calls++
		return GuardrailVerdict{}, errors.New(couldNotStartMsg)
	}
	inv := GuardrailInvocation{PRDID: prd.ID, Level: "prd", UnitID: prd.ID, Guardrail: "rules-review"}
	_, err := m.runRulesReviewJudge(context.Background(), prd, inv, fn)
	if err == nil {
		t.Fatal("want the error once tries are used up")
	}
	max := m.infraRetryConfig().EffectiveMaxTries()
	if calls != max+1 || len(delays) != max {
		t.Fatalf("calls=%d delays=%d, want %d calls and %d waits", calls, len(delays), max+1, max)
	}
}
