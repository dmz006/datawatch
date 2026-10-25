package main

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

func TestJudgeSessionTimeoutDefaultsToThirtyMinutes(t *testing.T) {
	if got := judgeSessionTimeout(autonomouspkg.Config{}); got != 30*time.Minute {
		t.Fatalf("unset limit = %v, want 30m", got)
	}
	if got := judgeSessionTimeout(autonomouspkg.Config{JudgeTimeoutSeconds: 900}); got != 15*time.Minute {
		t.Fatalf("900s limit = %v, want 15m", got)
	}
}

func TestJudgeAttemptBackendsPolicyPinAndDefault(t *testing.T) {
	// The policy list is the attempt order, de-duplicated.
	if got := judgeAttemptBackends([]string{"claude-code", "qwen", "claude-code"}, "ollama", false); !reflect.DeepEqual(got, []string{"claude-code", "qwen"}) {
		t.Fatalf("policy order = %v", got)
	}
	// A pinned unit backend is the only candidate, whatever the policy says.
	if got := judgeAttemptBackends([]string{"claude-code", "qwen"}, "ollama", true); !reflect.DeepEqual(got, []string{"ollama"}) {
		t.Fatalf("pinned = %v, want the unit backend only", got)
	}
	// An empty policy keeps the unit's resolved backend, one attempt.
	if got := judgeAttemptBackends(nil, "ollama", false); !reflect.DeepEqual(got, []string{"ollama"}) {
		t.Fatalf("empty policy = %v, want the unit backend", got)
	}
}

type alertRecord struct{ level, title, body string }

func recordAlerts(out *[]alertRecord) judgeAlertFn {
	return func(level, title, body string) {
		*out = append(*out, alertRecord{level, title, body})
	}
}

func TestRunJudgeAttemptsTimeoutThenPasses(t *testing.T) {
	var alerts []alertRecord
	var tried []string
	req := autonomouspkg.GuardrailInvocation{PRDID: "prd-42", Guardrail: "rules"}
	verdict, err := runJudgeAttempts(req, []string{"claude-code", "qwen"}, 30*time.Minute, recordAlerts(&alerts),
		func(backend string, att *judgeAttempt) (autonomouspkg.GuardrailVerdict, error) {
			tried = append(tried, backend)
			if backend == "claude-code" {
				att.TimedOut = true
				return autonomouspkg.GuardrailVerdict{Outcome: "block", Category: "infra"}, nil
			}
			return autonomouspkg.GuardrailVerdict{Outcome: "pass"}, nil
		})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if verdict.Outcome != "pass" {
		t.Fatalf("verdict = %+v, want the second backend's pass", verdict)
	}
	if !reflect.DeepEqual(tried, []string{"claude-code", "qwen"}) {
		t.Fatalf("attempts = %v, want one per backend in order", tried)
	}
	if len(alerts) != 1 || alerts[0].level != "warn" {
		t.Fatalf("alerts = %+v, want one warning for the timeout", alerts)
	}
	for _, want := range []string{"prd-42", "claude-code", "retrying on the next backend"} {
		if !strings.Contains(alerts[0].body, want) {
			t.Errorf("timeout warning %q lacks %q", alerts[0].body, want)
		}
	}
}

func TestRunJudgeAttemptsHoldsWhenEveryBackendTimesOut(t *testing.T) {
	var alerts []alertRecord
	var tried []string
	req := autonomouspkg.GuardrailInvocation{PRDID: "prd-77", Guardrail: "rules"}
	verdict, err := runJudgeAttempts(req, []string{"claude-code", "qwen"}, 30*time.Minute, recordAlerts(&alerts),
		func(backend string, att *judgeAttempt) (autonomouspkg.GuardrailVerdict, error) {
			tried = append(tried, backend)
			att.TimedOut = true
			return autonomouspkg.GuardrailVerdict{Outcome: "block", Category: "infra"}, nil
		})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(tried, []string{"claude-code", "qwen"}) {
		t.Fatalf("attempts = %v, want one per backend", tried)
	}
	if verdict.Outcome != "block" || verdict.Category != judgeTimeoutCategory {
		t.Fatalf("verdict = %+v, want a judge-timeout block", verdict)
	}
	if len(verdict.Issues) != 2 {
		t.Fatalf("issues = %v, want a line per attempt", verdict.Issues)
	}
	if !strings.Contains(verdict.Summary, "prd-77") || !strings.Contains(verdict.Summary, "blocked for operator review") {
		t.Fatalf("summary %q lacks the PRD id or the hold", verdict.Summary)
	}
	// One warning per timeout, then one error for the hold.
	if len(alerts) != 3 || alerts[0].level != "warn" || alerts[1].level != "warn" || alerts[2].level != "error" {
		t.Fatalf("alerts = %+v, want warn, warn, error", alerts)
	}
	last := alerts[2].body
	for _, want := range []string{"prd-77", "claude-code", "qwen", "in total"} {
		if !strings.Contains(last, want) {
			t.Errorf("hold alert %q lacks %q", last, want)
		}
	}
	if !strings.Contains(alerts[1].body, "no backend is left") {
		t.Errorf("last timeout warning %q should say no backend is left", alerts[1].body)
	}
}

func TestRunJudgeAttemptsNonTimeoutFailureIsFinal(t *testing.T) {
	var alerts []alertRecord
	var tried []string
	boom := errors.New("ask call failed")
	req := autonomouspkg.GuardrailInvocation{PRDID: "prd-9"}
	_, err := runJudgeAttempts(req, []string{"claude-code", "qwen"}, time.Minute, recordAlerts(&alerts),
		func(backend string, att *judgeAttempt) (autonomouspkg.GuardrailVerdict, error) {
			tried = append(tried, backend)
			return autonomouspkg.GuardrailVerdict{}, boom
		})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the attempt's own error", err)
	}
	if !reflect.DeepEqual(tried, []string{"claude-code"}) {
		t.Fatalf("attempts = %v, a non-timeout failure must not move on", tried)
	}
	if len(alerts) != 0 {
		t.Fatalf("alerts = %+v, want none for a non-timeout failure", alerts)
	}
}

func TestIsOneShotTimeoutOnlyForTimeLimitErrors(t *testing.T) {
	timeout := &autonomouspkg.OneShotNoOutputError{Msg: "guardrail session timed out after 30m0s", TimedOut: true}
	if !isOneShotTimeout(timeout) {
		t.Fatal("a session that hit its time limit must be recognised")
	}
	if !isOneShotTimeout(fmt.Errorf("judge: %w", timeout)) {
		t.Fatal("a wrapped time-limit error must be recognised")
	}
	stall := &autonomouspkg.OneShotNoOutputError{Msg: "verifier no progress"}
	if isOneShotTimeout(stall) {
		t.Fatal("a no-progress stall is not a time-limit timeout")
	}
	if isOneShotTimeout(errors.New("session failed")) {
		t.Fatal("an unrelated error is not a time-limit timeout")
	}
}
