package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// judgeTimeoutCategory is the verdict category of a judge that hit its time
// limit on every backend in its policy. It is neither "infra" (the judge is
// not re-run as a whole) nor "content" (no content auto-fix is attempted), so
// the PRD stops visibly with every attempt listed and nothing is passed.
const judgeTimeoutCategory = "judge-timeout"

// judgeAlertFn raises one operator alert. level is "warn" or "error".
type judgeAlertFn func(level, title, body string)

// judgeAttemptFn runs one judge attempt on backend. It sets att.TimedOut when
// the attempt hit its time limit.
type judgeAttemptFn func(backend string, att *judgeAttempt) (autonomouspkg.GuardrailVerdict, error)

// judgeAttempt is one attempt of a judge on one backend.
type judgeAttempt struct {
	Backend  string
	Limit    time.Duration
	Elapsed  time.Duration
	TimedOut bool
}

// isOneShotTimeout reports whether err is a one-shot session that hit its
// time limit, as opposed to a no-progress stall or a session that ended
// without output.
func isOneShotTimeout(err error) bool {
	var noOutput *autonomouspkg.OneShotNoOutputError
	return errors.As(err, &noOutput) && noOutput.TimedOut
}

// judgeSessionTimeout is the time limit for one judge run, from
// autonomous.judge_timeout_seconds (default 30 minutes). It starts when the
// judge session starts: the wait for a capacity slot comes before it.
func judgeSessionTimeout(gcfg autonomouspkg.Config) time.Duration {
	return time.Duration(config.NormalizeJudgeTimeoutSeconds(gcfg.JudgeTimeoutSeconds)) * time.Second
}

// judgeAttemptBackends returns the backends one judge call tries, in order,
// one attempt each. A unit that pins its own guardrail backend keeps that one
// backend. Otherwise the judge policy list applies. An empty policy means the
// unit's resolved backend alone.
func judgeAttemptBackends(policy []string, unitBackend string, pinned bool) []string {
	if pinned {
		return []string{unitBackend}
	}
	if backends := config.NormalizeJudgePolicy(policy); len(backends) > 0 {
		return backends
	}
	return []string{unitBackend}
}

// runJudgeAttempts runs the judge on each backend in order, one attempt each.
// An attempt that returns, or fails for any reason other than its time limit,
// is final. An attempt that hit its time limit raises a warning and the next
// backend is tried. When every backend has timed out, the PRD is blocked for operator review: the
// verdict is a judge-timeout block that lists every attempt, and an error alert
// is raised. Nothing is approved, passed or blocked silently on that path.
func runJudgeAttempts(req autonomouspkg.GuardrailInvocation, backends []string, limit time.Duration, alert judgeAlertFn, run judgeAttemptFn) (autonomouspkg.GuardrailVerdict, error) {
	var attempts []judgeAttempt
	for i, backend := range backends {
		att := judgeAttempt{Backend: backend, Limit: limit}
		started := time.Now()
		verdict, err := run(backend, &att)
		att.Elapsed = time.Since(started)
		if !att.TimedOut {
			return verdict, err
		}
		attempts = append(attempts, att)
		alert("warn", judgeTimeoutTitle(req.PRDID), judgeTimeoutBody(req.PRDID, att, i < len(backends)-1))
	}
	alert("error", judgeHoldTitle(req.PRDID), judgeHoldBody(req.PRDID, attempts))
	return judgeHoldVerdict(req, attempts), nil
}

func judgeTimeoutTitle(prdID string) string {
	return fmt.Sprintf("judge timed out for PRD %s", prdID)
}

func judgeTimeoutBody(prdID string, att judgeAttempt, more bool) string {
	next := "no backend is left in the judge policy"
	if more {
		next = "retrying on the next backend in the judge policy"
	}
	return fmt.Sprintf("judge for PRD %s timed out on backend %s after %s (limit %s); %s",
		prdID, att.Backend, att.Elapsed.Round(time.Second), att.Limit, next)
}

func judgeHoldTitle(prdID string) string {
	return fmt.Sprintf("PRD %s blocked for operator review: judge timed out on every backend", prdID)
}

func judgeHoldBody(prdID string, attempts []judgeAttempt) string {
	return fmt.Sprintf("PRD %s is blocked for operator review because the judge timed out on every backend tried (%s), %s in total. Nothing was approved or passed. Attempts: %s",
		prdID, judgeBackendList(attempts), judgeTotalElapsed(attempts), strings.Join(judgeAttemptLines(attempts), "; "))
}

// judgeHoldVerdict is the block the PRD records when every backend has timed out. It carries
// a line per
// attempt, so the review shows every backend that was tried and the time spent.
func judgeHoldVerdict(req autonomouspkg.GuardrailInvocation, attempts []judgeAttempt) autonomouspkg.GuardrailVerdict {
	return autonomouspkg.GuardrailVerdict{
		Guardrail: req.Guardrail,
		Outcome:   "block",
		Severity:  "high",
		Category:  judgeTimeoutCategory,
		Summary: fmt.Sprintf("judge for PRD %s timed out on every backend tried (%s), %s in total; the PRD is blocked for operator review",
			req.PRDID, judgeBackendList(attempts), judgeTotalElapsed(attempts)),
		Issues:    judgeAttemptLines(attempts),
		VerdictAt: time.Now(),
	}
}

func judgeAttemptLines(attempts []judgeAttempt) []string {
	lines := make([]string, 0, len(attempts))
	for _, a := range attempts {
		lines = append(lines, fmt.Sprintf("backend %s: timed out after %s (limit %s)", a.Backend, a.Elapsed.Round(time.Second), a.Limit))
	}
	return lines
}

func judgeBackendList(attempts []judgeAttempt) string {
	names := make([]string, 0, len(attempts))
	for _, a := range attempts {
		names = append(names, a.Backend)
	}
	return strings.Join(names, ", ")
}

func judgeTotalElapsed(attempts []judgeAttempt) time.Duration {
	var total time.Duration
	for _, a := range attempts {
		total += a.Elapsed
	}
	return total.Round(time.Second)
}

// emitJudgeAlert sends one alert through the manager's alert sink. With no
// sink wired it prints the alert, so a judge timeout is never dropped silently.
func emitJudgeAlert(mgr *autonomouspkg.Manager, level, title, body string) {
	if mgr != nil {
		if sink := mgr.AlertSink(); sink != nil {
			sink(level, title, body)
			return
		}
	}
	fmt.Printf("[%s] %s: %s\n", level, title, body)
}
