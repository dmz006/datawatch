package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// judgeAlertFn raises one operator alert. level is "warn" or "error".
type judgeAlertFn func(level, title, body string)

// judgeAttemptFn runs one judge attempt on backend. It sets att.TimedOut when
// the attempt hit its time limit.
type judgeAttemptFn func(backend string, att *judgeAttempt) (autonomouspkg.GuardrailVerdict, error)

// judgeBackendChooser picks the backend of the next judge attempt. tried lists
// the backends whose attempt already timed out on this judge call, so each
// backend is tried at most once. ok is false when no backend is left.
type judgeBackendChooser func(tried []string) (backend string, ok bool)

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

// judgeChooserFromList returns a chooser that tries the backends of an
// explicit judge policy list in order, skipping any that already timed out.
func judgeChooserFromList(backends []string) judgeBackendChooser {
	return func(tried []string) (string, bool) {
		for _, b := range backends {
			if !slices.Contains(tried, b) {
				return b, true
			}
		}
		return "", false
	}
}

// runJudgeAttempts runs the judge on the backends choose returns, one attempt
// each. An attempt that returns, or fails for any reason other than its time
// limit, is final. An attempt that hit its time limit raises a warning and the
// next backend is tried. When every backend has timed out, the verdict is a
// judge-timeout block that lists every attempt and an error alert is raised.
// The PRD then needs review: nothing is approved, passed or blocked silently.
// The chooser must offer at least one backend on the first call.
func runJudgeAttempts(req autonomouspkg.GuardrailInvocation, choose judgeBackendChooser, limit time.Duration, alert judgeAlertFn, run judgeAttemptFn) (autonomouspkg.GuardrailVerdict, error) {
	var attempts []judgeAttempt
	var tried []string
	backend, ok := choose(tried)
	for ok {
		att := judgeAttempt{Backend: backend, Limit: limit}
		started := time.Now()
		verdict, err := run(backend, &att)
		att.Elapsed = time.Since(started)
		if !att.TimedOut {
			return verdict, err
		}
		tried = append(tried, backend)
		attempts = append(attempts, att)
		backend, ok = choose(tried)
		alert("warn", judgeTimeoutTitle(req.PRDID), judgeTimeoutBody(req.PRDID, att, backend, ok))
	}
	alert("error", judgeHoldTitle(req.PRDID), judgeHoldBody(req.PRDID, attempts))
	return judgeHoldVerdict(req, attempts), nil
}

func judgeTimeoutTitle(prdID string) string {
	return fmt.Sprintf("judge timed out for Automaton %s", prdID)
}

// judgeTimeoutBody words the warning for one timed-out attempt. next is the
// backend the judge moves to, and more is false when none is left.
func judgeTimeoutBody(prdID string, att judgeAttempt, next string, more bool) string {
	after := "no backend is left in the judge policy, so the Automaton needs review"
	if more {
		after = "retrying on backend " + next + ", the next one in the judge policy"
	}
	return fmt.Sprintf("judge for Automaton %s timed out on backend %s after %s (limit %s); %s",
		prdID, att.Backend, att.Elapsed.Round(time.Second), att.Limit, after)
}

func judgeHoldTitle(prdID string) string {
	return fmt.Sprintf("Automaton %s needs review: judge timed out on every backend", prdID)
}

func judgeHoldBody(prdID string, attempts []judgeAttempt) string {
	return fmt.Sprintf("Automaton %s needs review: the judge timed out on every backend tried (%s), %s in total. Nothing was approved or passed. Attempts: %s",
		prdID, judgeBackendList(attempts), judgeTotalElapsed(attempts), strings.Join(judgeAttemptLines(attempts), "; "))
}

// judgeHoldVerdict is the block the PRD records when every backend has timed
// out. It carries one line per attempt, so the review shows every backend
// that was tried and the time spent on each.
func judgeHoldVerdict(req autonomouspkg.GuardrailInvocation, attempts []judgeAttempt) autonomouspkg.GuardrailVerdict {
	return autonomouspkg.GuardrailVerdict{
		Guardrail: req.Guardrail,
		Outcome:   "block",
		Severity:  "high",
		Category:  autonomouspkg.JudgeTimeoutCategory,
		Summary: fmt.Sprintf("judge for Automaton %s timed out on every backend tried (%s), %s in total; the Automaton needs review",
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
