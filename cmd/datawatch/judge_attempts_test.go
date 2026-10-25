package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

func TestJudgeSessionTimeoutDefaultsToThirtyMinutes(t *testing.T) {
	if got := judgeSessionTimeout(autonomouspkg.Config{}); got != 30*time.Minute {
		t.Fatalf("unset limit = %v, want 30m", got)
	}
	if got := judgeSessionTimeout(autonomouspkg.Config{JudgeTimeoutSeconds: 900}); got != 15*time.Minute {
		t.Fatalf("900s limit = %v, want 15m", got)
	}
}

func TestJudgeBackendChooserForPinsAndListOrder(t *testing.T) {
	ctx := context.Background()
	req := autonomouspkg.GuardrailInvocation{PRDID: "prd-1", UnitID: "u-1", GuardrailBackend: "qwen"}
	gcfg := autonomouspkg.Config{JudgePolicy: []string{"claude-code", "qwen", "claude-code"}}

	// A unit that pins its own guardrail backend keeps that one backend.
	pinned := judgeBackendChooserFor(ctx, nil, req, gcfg, "qwen")
	if b, ok := pinned(nil); !ok || b != "qwen" {
		t.Fatalf("pinned first = %q %v, want qwen", b, ok)
	}
	if _, ok := pinned([]string{"qwen"}); ok {
		t.Fatal("pinned unit must not move on to the judge policy list")
	}

	// An explicit judge policy list is tried in order, duplicates dropped.
	req.GuardrailBackend = ""
	list := judgeBackendChooserFor(ctx, nil, req, gcfg, "ollama")
	if b, _ := list(nil); b != "claude-code" {
		t.Fatalf("list first = %q, want claude-code", b)
	}
	if b, _ := list([]string{"claude-code"}); b != "qwen" {
		t.Fatalf("list second = %q, want qwen", b)
	}
	if _, ok := list([]string{"claude-code", "qwen"}); ok {
		t.Fatal("list must offer nothing after every entry timed out")
	}
}

func TestJudgeVerifierChooserFollowsVerifierPolicy(t *testing.T) {
	// An empty judge policy follows the verifier policy: a fixed policy with a
	// backend list tries the list in order, one attempt each.
	cfg := autonomouspkg.Config{VerifierPolicy: config.VerifierPolicyConfig{Mode: "fixed", Backends: []string{"claude-code", "qwen"}}}
	choose := judgeVerifierChooser(nil, cfg, "ollama", nil)
	if b, ok := choose(nil); !ok || b != "claude-code" {
		t.Fatalf("first = %q %v, want claude-code", b, ok)
	}
	if b, ok := choose([]string{"claude-code"}); !ok || b != "qwen" {
		t.Fatalf("second = %q %v, want qwen", b, ok)
	}
	if _, ok := choose([]string{"claude-code", "qwen"}); ok {
		t.Fatal("every policy backend was tried; nothing may be left")
	}
}

func TestJudgeVerifierChooserBalanceUsesLoadNotListOrder(t *testing.T) {
	prev := verifierBackendLoad
	t.Cleanup(func() { verifierBackendLoad = prev })
	verifierBackendLoad = func(backend, _ string) float64 {
		if backend == "claude-code" {
			return 0.9
		}
		return 0.1
	}
	cfg := autonomouspkg.Config{VerifierPolicy: config.VerifierPolicyConfig{Mode: "balance", Backends: []string{"claude-code", "qwen"}}}
	choose := judgeVerifierChooser(nil, cfg, "ollama", nil)
	if b, _ := choose(nil); b != "qwen" {
		t.Fatalf("balance picked %q, want the lower-loaded qwen", b)
	}
}

func TestJudgeVerifierChooserSkipsBackendsThatDoNotAdmit(t *testing.T) {
	cfg := autonomouspkg.Config{VerifierPolicy: config.VerifierPolicyConfig{Mode: "prefer-free", Backends: []string{"claude-code", "qwen"}}}
	probe := func(backend, _ string) bool { return backend == "qwen" }
	choose := judgeVerifierChooser(nil, cfg, "ollama", probe)
	if b, _ := choose(nil); b != "qwen" {
		t.Fatalf("prefer-free picked %q, want the backend with a free slot", b)
	}
}

func TestJudgeVerifierChooserFallsBackToUnitBackendOnFirstAttemptOnly(t *testing.T) {
	// A fixed policy with no backend list does not apply: the unit's backend
	// is tried once, as before the policy existed.
	cfg := autonomouspkg.Config{VerifierPolicy: config.VerifierPolicyConfig{Mode: "fixed"}}
	choose := judgeVerifierChooser(nil, cfg, "ollama", nil)
	if b, ok := choose(nil); !ok || b != "ollama" {
		t.Fatalf("unit fallback = %q %v, want ollama", b, ok)
	}
	if _, ok := choose([]string{"ollama"}); ok {
		t.Fatal("the unit backend was already tried; nothing may be left")
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
	verdict, err := runJudgeAttempts(req, judgeChooserFromList([]string{"claude-code", "qwen"}), 30*time.Minute, recordAlerts(&alerts),
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
	for _, want := range []string{"prd-42", "claude-code", "retrying on backend qwen"} {
		if !strings.Contains(alerts[0].body, want) {
			t.Errorf("timeout warning %q lacks %q", alerts[0].body, want)
		}
	}
}

func TestRunJudgeAttemptsHoldsWhenEveryBackendTimesOut(t *testing.T) {
	var alerts []alertRecord
	var tried []string
	req := autonomouspkg.GuardrailInvocation{PRDID: "prd-77", Guardrail: "rules"}
	verdict, err := runJudgeAttempts(req, judgeChooserFromList([]string{"claude-code", "qwen"}), 30*time.Minute, recordAlerts(&alerts),
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
	if verdict.Outcome != "block" || verdict.Category != autonomouspkg.JudgeTimeoutCategory {
		t.Fatalf("verdict = %+v, want a judge-timeout block", verdict)
	}
	if len(verdict.Issues) != 2 {
		t.Fatalf("issues = %v, want a line per attempt", verdict.Issues)
	}
	if !strings.Contains(verdict.Summary, "prd-77") || !strings.Contains(verdict.Summary, "needs review") {
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
	_, err := runJudgeAttempts(req, judgeChooserFromList([]string{"claude-code", "qwen"}), time.Minute, recordAlerts(&alerts),
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
