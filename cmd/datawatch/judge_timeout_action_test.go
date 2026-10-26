package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// fakeJudgeTryLog is an in-memory judgeTryLog that behaves like the durable
// one: records survive across runJudgeAttempts calls, the way a restart keeps
// them on the PRD.
type fakeJudgeTryLog struct {
	recs     []autonomouspkg.JudgeAttemptRecord
	recorded []string
	cleared  int
	err      error
}

func (f *fakeJudgeTryLog) Tried() []autonomouspkg.JudgeAttemptRecord { return f.recs }

func (f *fakeJudgeTryLog) Record(rec autonomouspkg.JudgeAttemptRecord) error {
	f.recorded = append(f.recorded, rec.Backend)
	f.recs = append(f.recs, rec)
	return f.err
}

func (f *fakeJudgeTryLog) Clear() error {
	f.cleared++
	f.recs = nil
	return f.err
}

// timingOutRun is a judge attempt that hits its time limit on every backend
// listed in timeouts and passes on the rest.
func timingOutRun(timeouts ...string) (func(string, *judgeAttempt) (autonomouspkg.GuardrailVerdict, error), *[]string) {
	var tried []string
	return func(backend string, att *judgeAttempt) (autonomouspkg.GuardrailVerdict, error) {
		tried = append(tried, backend)
		for _, b := range timeouts {
			if b == backend {
				att.TimedOut = true
				return autonomouspkg.GuardrailVerdict{Outcome: "block", Category: "infra"}, nil
			}
		}
		return autonomouspkg.GuardrailVerdict{Outcome: "pass"}, nil
	}, &tried
}

func TestJudgeTimeoutWarningNamesPRDBackendsAndTimeSpent(t *testing.T) {
	var alerts []alertRecord
	run, _ := timingOutRun("claude-code")
	req := autonomouspkg.GuardrailInvocation{PRDID: "prd-warn", Guardrail: "rules"}
	if _, err := runJudgeAttempts(req, judgeChooserFromList([]string{"claude-code", "qwen"}), 30*time.Minute, recordAlerts(&alerts), nil, run); err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(alerts) != 1 || alerts[0].level != "warn" {
		t.Fatalf("alerts = %+v, want one warning", alerts)
	}
	body := alerts[0].body
	for _, want := range []string{"prd-warn", "claude-code", "backends tried so far: claude-code", "in total", "retrying on backend qwen"} {
		if !strings.Contains(body, want) {
			t.Errorf("timeout warning %q lacks %q", body, want)
		}
	}
	if !strings.Contains(alerts[0].title, "prd-warn") {
		t.Errorf("timeout warning title %q lacks the PRD id", alerts[0].title)
	}
}

func TestJudgeSingleBackendPolicyGetsOneAttemptThenHolds(t *testing.T) {
	var alerts []alertRecord
	run, tried := timingOutRun("qwen")
	req := autonomouspkg.GuardrailInvocation{PRDID: "prd-one", Guardrail: "rules"}
	verdict, err := runJudgeAttempts(req, judgeChooserFromList([]string{"qwen"}), 30*time.Minute, recordAlerts(&alerts), nil, run)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(*tried, []string{"qwen"}) {
		t.Fatalf("attempts = %v, want exactly one on the only backend", *tried)
	}
	if verdict.Category != autonomouspkg.JudgeTimeoutCategory || len(verdict.JudgeAttempts) != 1 {
		t.Fatalf("verdict = %+v, want a judge-timeout hold with one attempt", verdict)
	}
	if len(alerts) != 2 || alerts[0].level != "warn" || alerts[1].level != "error" {
		t.Fatalf("alerts = %+v, want one warning then one error", alerts)
	}
	if !strings.Contains(alerts[0].body, "no backend is left") {
		t.Errorf("warning %q should say no backend is left", alerts[0].body)
	}
}

func TestJudgeTimeoutFirstBackendThenSuccessRaisesOneWarning(t *testing.T) {
	var alerts []alertRecord
	run, tried := timingOutRun("claude-code")
	req := autonomouspkg.GuardrailInvocation{PRDID: "prd-ok", Guardrail: "rules"}
	log := &fakeJudgeTryLog{}
	verdict, err := runJudgeAttempts(req, judgeChooserFromList([]string{"claude-code", "qwen"}), time.Minute, recordAlerts(&alerts), log, run)
	if err != nil || verdict.Outcome != "pass" {
		t.Fatalf("verdict = %+v, err = %v; want the second backend's pass", verdict, err)
	}
	if !reflect.DeepEqual(*tried, []string{"claude-code", "qwen"}) {
		t.Fatalf("attempts = %v, want one per backend in order", *tried)
	}
	if len(alerts) != 1 || alerts[0].level != "warn" {
		t.Fatalf("alerts = %+v, want exactly one warning", alerts)
	}
	if !reflect.DeepEqual(log.recorded, []string{"claude-code"}) {
		t.Fatalf("recorded timeouts = %v, want the timed-out backend", log.recorded)
	}
	if log.cleared != 1 || len(log.recs) != 0 {
		t.Fatalf("try log cleared %d times, left %d records; want cleared once", log.cleared, len(log.recs))
	}
}

func TestJudgeAllBackendsTimeOutHoldsWithSummaryOfEveryAttempt(t *testing.T) {
	var alerts []alertRecord
	run, _ := timingOutRun("claude-code", "qwen")
	req := autonomouspkg.GuardrailInvocation{PRDID: "prd-hold", Guardrail: "rules"}
	log := &fakeJudgeTryLog{}
	verdict, err := runJudgeAttempts(req, judgeChooserFromList([]string{"claude-code", "qwen"}), time.Minute, recordAlerts(&alerts), log, run)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if verdict.Outcome != "block" || verdict.Category != autonomouspkg.JudgeTimeoutCategory {
		t.Fatalf("verdict = %+v, want a judge-timeout hold", verdict)
	}
	if !autonomouspkg.IsJudgeTimeoutHold(verdict) {
		t.Fatal("the hold verdict must be recognised as a judge-timeout hold")
	}
	if len(verdict.JudgeAttempts) != 2 {
		t.Fatalf("summary = %+v, want one record per attempt", verdict.JudgeAttempts)
	}
	for i, want := range []string{"claude-code", "qwen"} {
		got := verdict.JudgeAttempts[i]
		if got.Backend != want || got.Outcome != autonomouspkg.JudgeOutcomeTimedOut || got.Started.IsZero() {
			t.Errorf("attempt %d = %+v, want backend %s timed out with a start time", i, got, want)
		}
	}
	if len(verdict.Issues) != 2 || !strings.Contains(verdict.Issues[0], "started") {
		t.Fatalf("issues = %v, want a line per attempt that names its start", verdict.Issues)
	}
	if len(alerts) != 3 || alerts[0].level != "warn" || alerts[1].level != "warn" || alerts[2].level != "error" {
		t.Fatalf("alerts = %+v, want warn, warn, error", alerts)
	}
	if !strings.Contains(alerts[2].body, "prd-hold") {
		t.Errorf("hold alert %q lacks the PRD id", alerts[2].body)
	}
	if log.cleared != 1 {
		t.Fatalf("try log cleared %d times, want once after the hold", log.cleared)
	}
}

func TestJudgeTimeoutsSurviveRestartAndSkipTheTriedBackends(t *testing.T) {
	// Before the restart claude-code timed out. The restarted daemon reads the
	// record back, so the judge tries only qwen and does not run claude-code again.
	started := time.Now().Add(-time.Hour)
	log := &fakeJudgeTryLog{recs: []autonomouspkg.JudgeAttemptRecord{
		{Backend: "claude-code", Started: started, Duration: 30 * time.Minute, Outcome: autonomouspkg.JudgeOutcomeTimedOut},
	}}
	var alerts []alertRecord
	run, tried := timingOutRun()
	req := autonomouspkg.GuardrailInvocation{PRDID: "prd-restart", Guardrail: "rules"}
	verdict, err := runJudgeAttempts(req, judgeChooserFromList([]string{"claude-code", "qwen"}), 30*time.Minute, recordAlerts(&alerts), log, run)
	if err != nil || verdict.Outcome != "pass" {
		t.Fatalf("verdict = %+v, err = %v; want qwen's pass", verdict, err)
	}
	if !reflect.DeepEqual(*tried, []string{"qwen"}) {
		t.Fatalf("attempts after restart = %v, want only the untried backend", *tried)
	}
	if len(alerts) != 0 {
		t.Fatalf("alerts = %+v, a restored timeout must not warn again", alerts)
	}
}

func TestJudgeRestoredTimeoutsOnEveryBackendHoldWithoutRunning(t *testing.T) {
	// Both backends timed out before the restart: the judge must not run again,
	// it holds at once with the summary of the earlier attempts.
	log := &fakeJudgeTryLog{recs: []autonomouspkg.JudgeAttemptRecord{
		{Backend: "claude-code", Started: time.Now().Add(-2 * time.Hour), Duration: 30 * time.Minute, Outcome: autonomouspkg.JudgeOutcomeTimedOut},
		{Backend: "qwen", Started: time.Now().Add(-time.Hour), Duration: 30 * time.Minute, Outcome: autonomouspkg.JudgeOutcomeTimedOut},
	}}
	var alerts []alertRecord
	run, tried := timingOutRun()
	req := autonomouspkg.GuardrailInvocation{PRDID: "prd-held", Guardrail: "rules"}
	verdict, err := runJudgeAttempts(req, judgeChooserFromList([]string{"claude-code", "qwen"}), 30*time.Minute, recordAlerts(&alerts), log, run)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(*tried) != 0 {
		t.Fatalf("the judge ran on %v after a restart; every backend had already timed out", *tried)
	}
	if !autonomouspkg.IsJudgeTimeoutHold(verdict) || len(verdict.JudgeAttempts) != 2 {
		t.Fatalf("verdict = %+v, want a hold with both earlier attempts", verdict)
	}
	if len(alerts) != 1 || alerts[0].level != "error" || !strings.Contains(alerts[0].body, "prd-held") {
		t.Fatalf("alerts = %+v, want one error alert naming the PRD", alerts)
	}
}

func TestJudgeHoldAlertNamesPRDBackendsAndTotalTime(t *testing.T) {
	var alerts []alertRecord
	run, _ := timingOutRun("claude-code", "qwen", "ollama")
	req := autonomouspkg.GuardrailInvocation{PRDID: "prd-text", Guardrail: "rules"}
	if _, err := runJudgeAttempts(req, judgeChooserFromList([]string{"claude-code", "qwen", "ollama"}), 30*time.Minute, recordAlerts(&alerts), nil, run); err != nil {
		t.Fatalf("err = %v", err)
	}
	last := alerts[len(alerts)-1]
	if last.level != "error" {
		t.Fatalf("last alert = %+v, want the error for the hold", last)
	}
	for _, want := range []string{"prd-text", "claude-code, qwen, ollama", "in total"} {
		if !strings.Contains(last.body, want) {
			t.Errorf("hold alert %q lacks %q", last.body, want)
		}
	}
}

func TestJudgeTryLogWriteFailureDoesNotStopTheJudge(t *testing.T) {
	var alerts []alertRecord
	run, tried := timingOutRun("claude-code")
	log := &fakeJudgeTryLog{err: errors.New("disk full")}
	req := autonomouspkg.GuardrailInvocation{PRDID: "prd-io", Guardrail: "rules"}
	verdict, err := runJudgeAttempts(req, judgeChooserFromList([]string{"claude-code", "qwen"}), time.Minute, recordAlerts(&alerts), log, run)
	if err != nil || verdict.Outcome != "pass" {
		t.Fatalf("verdict = %+v, err = %v; a failed record write must not stop the next backend", verdict, err)
	}
	if !reflect.DeepEqual(*tried, []string{"claude-code", "qwen"}) {
		t.Fatalf("attempts = %v, want both backends", *tried)
	}
}
