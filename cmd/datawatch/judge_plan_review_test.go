package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

func planReviewTestRequest(prdID string) autonomouspkg.PlanReviewRequest {
	return autonomouspkg.PlanReviewRequest{
		PRDID: prdID,
		Spec:  "add a retry to the uploader",
		Stories: []autonomouspkg.PlanReviewStory{{
			Title: "uploader",
			Tasks: []autonomouspkg.PlanReviewTask{{ID: "t1", Title: "add retry", Spec: "retry three times"}},
		}},
	}
}

func TestPlanReviewRunsAsAJudgeInvocationForItsAutomaton(t *testing.T) {
	var got autonomouspkg.GuardrailInvocation
	judge := func(_ context.Context, inv autonomouspkg.GuardrailInvocation) (autonomouspkg.GuardrailVerdict, error) {
		got = inv
		return autonomouspkg.GuardrailVerdict{Outcome: "pass", Summary: "plan is sound"}, nil
	}
	review := planReviewViaJudge(judge,
		func(prdID string) string { return "/work/" + prdID },
		func() (string, string) { return "qwen", "qwen3.8:27b" })
	v, err := review(context.Background(), planReviewTestRequest("prd-5"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !v.Ok || v.ReviewerFailed || v.Held {
		t.Fatalf("verdict = %+v, want a plain pass", v)
	}
	if got.PRDID != "prd-5" || got.UnitID != "plan-review-prd-5" || got.Guardrail != "plan-review" {
		t.Fatalf("invocation = %+v, want the plan-review judge of prd-5", got)
	}
	if got.ProjectDir != "/work/prd-5" || got.GuardrailBackend != "qwen" || got.GuardrailModel != "qwen3.8:27b" {
		t.Fatalf("invocation = %+v, want the project dir and the configured reviewer backend", got)
	}
	if !got.GuardrailPinned {
		t.Fatalf("GuardrailPinned = false, want true: plan_review_backend is an explicit pin")
	}
	for _, want := range []string{"add a retry to the uploader", "retry three times", "missing", "fragile_verification", "STRICT JSON"} {
		if !strings.Contains(got.Prompt, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	other := planReviewUnitID("prd-6")
	if other == got.UnitID {
		t.Fatalf("two Automata share the lease holder unit %q", other)
	}
}

func TestPlanReviewPinFlagFollowsTheConfiguredBackend(t *testing.T) {
	for _, tc := range []struct {
		name       string
		backend    string
		wantPinned bool
	}{
		{"backend set", "qwen", true},
		{"backend empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got autonomouspkg.GuardrailInvocation
			judge := func(_ context.Context, inv autonomouspkg.GuardrailInvocation) (autonomouspkg.GuardrailVerdict, error) {
				got = inv
				return autonomouspkg.GuardrailVerdict{Outcome: "pass", Summary: "plan is sound"}, nil
			}
			review := planReviewViaJudge(judge, nil,
				func() (string, string) { return tc.backend, "" })
			if _, err := review(context.Background(), planReviewTestRequest("prd-7")); err != nil {
				t.Fatalf("err = %v", err)
			}
			if got.GuardrailPinned != tc.wantPinned {
				t.Fatalf("GuardrailPinned = %v, want %v for backend %q", got.GuardrailPinned, tc.wantPinned, tc.backend)
			}
		})
	}
}

func TestPlanReviewVerdictCarriesTheJudgesFindings(t *testing.T) {
	judge := func(context.Context, autonomouspkg.GuardrailInvocation) (autonomouspkg.GuardrailVerdict, error) {
		return autonomouspkg.GuardrailVerdict{
			Outcome: "block", Severity: "high", Summary: "a requirement is dropped",
			Issues: []string{"missing||no task covers the retry limit", "weakened|t1|retries once, not three times", "free text"},
		}, nil
	}
	v, err := planReviewViaJudge(judge, nil, nil)(context.Background(), planReviewTestRequest("prd-5"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if v.Ok || v.ReviewerFailed || v.Held || v.Severity != "high" {
		t.Fatalf("verdict = %+v, want a negative judgment", v)
	}
	want := []autonomouspkg.PlanFinding{
		{Category: "missing", Detail: "no task covers the retry limit"},
		{Category: "weakened", TaskID: "t1", Detail: "retries once, not three times"},
		{Detail: "free text"},
	}
	if len(v.Findings) != len(want) {
		t.Fatalf("findings = %+v, want %+v", v.Findings, want)
	}
	for i := range want {
		if v.Findings[i] != want[i] {
			t.Errorf("finding %d = %+v, want %+v", i, v.Findings[i], want[i])
		}
	}
}

func TestPlanReviewJudgeHoldIsAHeldVerdictWithEveryAttempt(t *testing.T) {
	judge := func(context.Context, autonomouspkg.GuardrailInvocation) (autonomouspkg.GuardrailVerdict, error) {
		return autonomouspkg.GuardrailVerdict{
			Outcome: "block", Severity: "high", Category: autonomouspkg.JudgeTimeoutCategory,
			Summary: "judge for Automaton prd-5 timed out on every backend tried",
			Issues:  []string{"backend qwen: timed out after 30m0s", "backend claude-code: timed out after 30m0s"},
		}, nil
	}
	v, err := planReviewViaJudge(judge, nil, nil)(context.Background(), planReviewTestRequest("prd-5"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if v.Ok || v.ReviewerFailed || !v.Held {
		t.Fatalf("verdict = %+v, want a held verdict that is neither a pass nor an outage", v)
	}
	if len(v.Findings) != 2 || v.Findings[0].Category != autonomouspkg.JudgeTimeoutCategory ||
		!strings.Contains(v.Findings[1].Detail, "claude-code") {
		t.Fatalf("findings = %+v, want one per attempt", v.Findings)
	}
}

func TestPlanReviewJudgeThatCouldNotRunIsAReviewerOutage(t *testing.T) {
	for _, category := range []string{"infra", "invalid-response"} {
		judge := func(context.Context, autonomouspkg.GuardrailInvocation) (autonomouspkg.GuardrailVerdict, error) {
			return autonomouspkg.GuardrailVerdict{Outcome: "block", Category: category, Summary: "no answer"}, nil
		}
		v, err := planReviewViaJudge(judge, nil, nil)(context.Background(), planReviewTestRequest("prd-5"))
		if err != nil || !v.ReviewerFailed || v.Ok || v.Held {
			t.Fatalf("category %s: verdict = %+v err = %v, want a reviewer outage", category, v, err)
		}
	}
	boom := errors.New("cancelled")
	judge := func(context.Context, autonomouspkg.GuardrailInvocation) (autonomouspkg.GuardrailVerdict, error) {
		return autonomouspkg.GuardrailVerdict{}, boom
	}
	if _, err := planReviewViaJudge(judge, nil, nil)(context.Background(), planReviewTestRequest("prd-5")); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the judge's error handed to the caller", err)
	}
}

func TestPlanReviewRemovesTheJudgeOutputFileOnceRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".guardrail-output-x.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	judge := func(context.Context, autonomouspkg.GuardrailInvocation) (autonomouspkg.GuardrailVerdict, error) {
		return autonomouspkg.GuardrailVerdict{Outcome: "pass", JudgeOutputPath: path}, nil
	}
	if _, err := planReviewViaJudge(judge, nil, nil)(context.Background(), planReviewTestRequest("prd-5")); err != nil {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("judge output file still present: %v", err)
	}
}

// planReviewThroughSelector runs a plan review over the real judge path: the
// judge role's chooser, one attempt per backend, and the lease helper, with the
// lease and the judge body faked. It returns the verdict and the backends the
// body ran on.
func planReviewThroughSelector(t *testing.T, gcfg autonomouspkg.Config, admit judgeLeaseAdmit, body func(backend string, att *judgeAttempt) (autonomouspkg.GuardrailVerdict, bool), alerts *[]alertRecord) (autonomouspkg.PlanReviewVerdict, []string) {
	t.Helper()
	var ranOn []string
	judge := func(ctx context.Context, inv autonomouspkg.GuardrailInvocation) (autonomouspkg.GuardrailVerdict, error) {
		choose := judgeBackendChooserFor(ctx, nil, inv, gcfg, "ollama")
		return runJudgeAttempts(inv, choose, time.Minute, recordAlerts(alerts), nil,
			func(backend string, att *judgeAttempt) (autonomouspkg.GuardrailVerdict, error) {
				att.Backend = backend
				return runJudgeLeased(ctx, admit, att, "", inv.UnitID, inv.PRDID,
					func(string) (autonomouspkg.GuardrailVerdict, error) {
						ranOn = append(ranOn, backend)
						v, _ := body(backend, att)
						return v, nil
					})
			})
	}
	v, err := planReviewViaJudge(judge, nil, nil)(context.Background(), planReviewTestRequest("prd-5"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	return v, ranOn
}

func TestPlanReviewTakesTheNextBackendWhenThePreferredPoolIsFull(t *testing.T) {
	prev := judgeCapacityTryAdmit
	judgeCapacityTryAdmit = func(_ context.Context, backend, _, _, _ string) bool { return backend == "claude-code" }
	t.Cleanup(func() { judgeCapacityTryAdmit = prev })

	var releases atomic.Int32
	var alerts []alertRecord
	gcfg := autonomouspkg.Config{
		VerifierPolicy: config.VerifierPolicyConfig{Mode: "prefer-free", Backends: []string{"qwen", "claude-code"}},
	}
	v, ranOn := planReviewThroughSelector(t, gcfg, grantingLease(&releases, ""),
		func(string, *judgeAttempt) (autonomouspkg.GuardrailVerdict, bool) {
			return autonomouspkg.GuardrailVerdict{Outcome: "pass"}, true
		}, &alerts)
	if !v.Ok || len(ranOn) != 1 || ranOn[0] != "claude-code" {
		t.Fatalf("verdict = %+v ran on %v, want one run on the free claude-code lane", v, ranOn)
	}
	if releases.Load() != 1 {
		t.Fatalf("lease released %d times, want 1", releases.Load())
	}
}

func TestPlanReviewIsHeldWithAnErrorAlertWhenEveryBackendTimesOut(t *testing.T) {
	var releases atomic.Int32
	var alerts []alertRecord
	gcfg := autonomouspkg.Config{
		VerifierPolicy: config.VerifierPolicyConfig{Mode: "prefer-free", Backends: []string{"qwen", "claude-code"}},
	}
	v, ranOn := planReviewThroughSelector(t, gcfg, grantingLease(&releases, ""),
		func(_ string, att *judgeAttempt) (autonomouspkg.GuardrailVerdict, bool) {
			att.TimedOut = true
			return autonomouspkg.GuardrailVerdict{Outcome: "block", Category: "infra"}, true
		}, &alerts)
	if len(ranOn) != 2 {
		t.Fatalf("ran on %v, want one attempt per backend", ranOn)
	}
	if !v.Held || v.Ok || v.ReviewerFailed {
		t.Fatalf("verdict = %+v, want the held verdict", v)
	}
	if len(alerts) != 3 || alerts[0].level != "warn" || alerts[1].level != "warn" || alerts[2].level != "error" {
		t.Fatalf("alerts = %+v, want a warning per timeout and an error for the hold", alerts)
	}
	for _, want := range []string{"prd-5", "qwen", "claude-code"} {
		if !strings.Contains(alerts[2].body, want) {
			t.Errorf("hold alert %q lacks %q", alerts[2].body, want)
		}
	}
	if releases.Load() != 2 {
		t.Fatalf("leases released %d times, want 2", releases.Load())
	}
}
