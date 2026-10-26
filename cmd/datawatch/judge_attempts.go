package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
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

// judgeAttempt is one attempt of a judge on one backend. TimedOut is set when
// the run hit its time limit. NoSlot is set when the capacity lease could not
// be granted in time, so the judge never ran on that backend.
type judgeAttempt struct {
	Backend  string
	Started  time.Time
	Limit    time.Duration
	Elapsed  time.Duration
	TimedOut bool
	NoSlot   bool
}

// judgeTryLog keeps the timed-out attempts of one judge call durable, so a
// daemon restart sees which backends were already tried. A nil log keeps them
// in memory only.
type judgeTryLog interface {
	Tried() []autonomouspkg.JudgeAttemptRecord
	Record(rec autonomouspkg.JudgeAttemptRecord) error
	Clear() error
}

// prdJudgeTryLog is the judgeTryLog of one judge call on a PRD, stored on it.
type prdJudgeTryLog struct {
	mgr   *autonomouspkg.Manager
	prdID string
	key   string
}

func (l prdJudgeTryLog) Tried() []autonomouspkg.JudgeAttemptRecord {
	return l.mgr.JudgeTries(l.prdID, l.key)
}

func (l prdJudgeTryLog) Record(rec autonomouspkg.JudgeAttemptRecord) error {
	return l.mgr.RecordJudgeTry(l.prdID, l.key, rec)
}

func (l prdJudgeTryLog) Clear() error {
	return l.mgr.ClearJudgeTries(l.prdID, l.key)
}

// judgeTryLogFor returns the durable try log of the judge call that runs for
// req, or nil when there is no manager or PRD to keep it on.
func judgeTryLogFor(mgr *autonomouspkg.Manager, req autonomouspkg.GuardrailInvocation) judgeTryLog {
	if mgr == nil || req.PRDID == "" {
		return nil
	}
	return prdJudgeTryLog{mgr: mgr, prdID: req.PRDID, key: req.UnitID + "|" + req.Guardrail}
}

// judgeRunContext bounds one judge run by its time limit. A zero limit is no
// bound. It is called after the lease is granted, so the wait for a slot is not
// counted against the limit.
func judgeRunContext(parent context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	if limit <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, limit)
}

// judgeRunTimedOut reports whether a judge run hit its own time limit: the run
// context expired while the judge's parent context was still live. A cancelled
// judge is not a timeout.
func judgeRunTimedOut(parent, run context.Context) bool {
	return errors.Is(run.Err(), context.DeadlineExceeded) && parent.Err() == nil
}

// judgeAskRun posts one judge ask to the /api/ask endpoint and reads the answer
// within the judge's time limit. timedOut is true when the limit expired before
// the answer was read. A failure before or while the answer is read returns
// its error and no body: a partial answer is never handed back as a verdict.
func judgeAskRun(parent context.Context, limit time.Duration, url, token string, body []byte) (status int, rb []byte, timedOut bool, err error) {
	ctx, cancel := judgeRunContext(parent, limit)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, false, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return 0, nil, judgeRunTimedOut(parent, ctx), err
	}
	defer resp.Body.Close() //nolint:errcheck
	rb, rerr := io.ReadAll(resp.Body)
	if rerr != nil {
		return 0, nil, judgeRunTimedOut(parent, ctx), rerr
	}
	return resp.StatusCode, rb, false, nil
}

// judgeAskTimeoutVerdict is the verdict of an ask call that hit its time limit.
// It is "infra": no judgment came back.
func judgeAskTimeoutVerdict(limit time.Duration) autonomouspkg.GuardrailVerdict {
	return autonomouspkg.GuardrailVerdict{
		Outcome:   "block",
		Severity:  "medium",
		Summary:   fmt.Sprintf("guardrail(ask): no answer within the judge time limit (%s)", limit),
		VerdictAt: time.Now(),
		Category:  "infra",
	}
}

// judgeAskFailedVerdict is the verdict of an ask call that failed before its
// answer was read in full. It is "infra": no judgment came back, so the infra
// retry may run the judge again, and the partial body is never parsed.
func judgeAskFailedVerdict(err error) autonomouspkg.GuardrailVerdict {
	return autonomouspkg.GuardrailVerdict{
		Outcome:   "block",
		Severity:  "medium",
		Summary:   "guardrail(ask): " + err.Error(),
		VerdictAt: time.Now(),
		Category:  "infra",
	}
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
// each. An attempt that returns, or fails for any reason other than the two
// below, is final. An attempt that hit its time limit raises a warning and the
// next backend is tried. An attempt whose capacity lease was not granted in
// time (NoSlot) moves on to the next backend without a warning, since the
// judge never ran there and no time limit was hit. When every backend has
// timed out, the verdict is a judge-timeout block that lists every attempt and
// an error alert is raised; the PRD then needs review, so nothing is approved,
// passed or blocked silently. When no attempt timed out and some lacked a slot,
// the judge never ran, so the last attempt's infra verdict is returned and
// nothing is held. Once any attempt has timed out the PRD is held, even when a
// later backend lacked a slot: an infra return would make the infra retry run
// the whole judge again, and the timed-out backend would run for another full
// time limit.
//
// tries, when set, makes the timed-out attempts durable: backends already
// recorded there (before a daemon restart, say) are excluded up front, each
// new timeout is recorded before the next backend is chosen, and the record is
// cleared once the call finishes. A call whose every backend timed out, in this
// run or an earlier one, holds without running the judge again. The chooser
// must offer at least one backend on the first call of a fresh judge.
func runJudgeAttempts(req autonomouspkg.GuardrailInvocation, choose judgeBackendChooser, limit time.Duration, alert judgeAlertFn, tries judgeTryLog, run judgeAttemptFn) (autonomouspkg.GuardrailVerdict, error) {
	attempts, tried := judgeRestoredAttempts(tries, limit)
	var last autonomouspkg.GuardrailVerdict
	var lastErr error
	backend, ok := choose(tried)
	for ok {
		att := judgeAttempt{Backend: backend, Limit: limit, Started: time.Now()}
		verdict, err := run(backend, &att)
		att.Elapsed = time.Since(att.Started)
		if !att.TimedOut && !att.NoSlot {
			judgeClearTries(tries)
			return verdict, err
		}
		tried = append(tried, backend)
		attempts = append(attempts, att)
		last, lastErr = verdict, err
		if att.TimedOut {
			judgeRecordTry(tries, att)
		}
		backend, ok = choose(tried)
		if att.TimedOut {
			alert("warn", judgeTimeoutTitle(req.PRDID), judgeTimeoutBody(req.PRDID, att, attempts, backend, ok))
		}
	}
	judgeClearTries(tries)
	if judgeHasNoSlot(attempts) && !judgeHasTimeout(attempts) {
		return last, lastErr
	}
	alert("error", judgeHoldTitle(req.PRDID, attempts), judgeHoldBody(req.PRDID, attempts))
	return judgeHoldVerdict(req, attempts), nil
}

// judgeHasNoSlot reports whether any attempt never got its capacity lease, so
// the judge never ran on that backend.
func judgeHasNoSlot(attempts []judgeAttempt) bool {
	for _, a := range attempts {
		if a.NoSlot {
			return true
		}
	}
	return false
}

// judgeHasTimeout reports whether any attempt hit its time limit.
func judgeHasTimeout(attempts []judgeAttempt) bool {
	for _, a := range attempts {
		if a.TimedOut {
			return true
		}
	}
	return false
}

func judgeTimeoutTitle(prdID string) string {
	return fmt.Sprintf("judge timed out for Automaton %s", prdID)
}

// judgeRestoredAttempts returns the attempts a judge call timed out on before
// this run (read back from tries), and the backends they used, so the chooser
// skips them. Nothing is restored without a log.
func judgeRestoredAttempts(tries judgeTryLog, limit time.Duration) ([]judgeAttempt, []string) {
	if tries == nil {
		return nil, nil
	}
	var attempts []judgeAttempt
	var tried []string
	for _, rec := range tries.Tried() {
		attempts = append(attempts, judgeAttempt{
			Backend: rec.Backend, Started: rec.Started, Limit: limit, Elapsed: rec.Duration, TimedOut: true,
		})
		tried = append(tried, rec.Backend)
	}
	return attempts, tried
}

// judgeRecordTry stores one timed-out attempt in the try log. A failed write is
// logged, not returned: the attempt still counts for this run, and the warning
// alert is raised either way.
func judgeRecordTry(tries judgeTryLog, att judgeAttempt) {
	if tries == nil {
		return
	}
	rec := autonomouspkg.JudgeAttemptRecord{
		Backend: att.Backend, Started: att.Started, Duration: att.Elapsed, Outcome: autonomouspkg.JudgeOutcomeTimedOut,
	}
	if err := tries.Record(rec); err != nil {
		log.Printf("judge: record timed-out attempt on backend %s: %v", att.Backend, err)
	}
}

// judgeClearTries drops the try log once the judge call has finished. Its
// summary is kept on the verdict. A failed write is logged, not returned.
func judgeClearTries(tries judgeTryLog) {
	if tries == nil {
		return
	}
	if err := tries.Clear(); err != nil {
		log.Printf("judge: clear timed-out attempts: %v", err)
	}
}

// judgeTimeoutBody words the warning for one timed-out attempt. attempts holds
// every attempt so far, the one just timed out last; next is the backend the
// judge moves to, and more is false when none is left. The warning names the
// PRD, the backends tried so far and the time spent.
func judgeTimeoutBody(prdID string, att judgeAttempt, attempts []judgeAttempt, next string, more bool) string {
	after := "no backend is left in the judge policy, so the Automaton needs review"
	if more {
		after = "retrying on backend " + next + ", the next one in the judge policy"
	}
	return fmt.Sprintf("judge for Automaton %s timed out on backend %s after %s (limit %s); backends tried so far: %s, %s in total; %s",
		prdID, att.Backend, att.Elapsed.Round(time.Second), att.Limit, judgeBackendList(attempts), judgeTotalElapsed(attempts), after)
}

// judgeHoldReason says why the judge is held, in words that suit the attempts
// made. A backend that never got a capacity slot did not time out, so the
// reason covers that case too.
func judgeHoldReason(attempts []judgeAttempt) string {
	if judgeHasNoSlot(attempts) {
		return "did not finish on any backend tried: timed out or no capacity slot"
	}
	return "timed out on every backend tried"
}

func judgeHoldTitle(prdID string, attempts []judgeAttempt) string {
	return fmt.Sprintf("Automaton %s needs review: the judge %s", prdID, judgeHoldReason(attempts))
}

func judgeHoldBody(prdID string, attempts []judgeAttempt) string {
	return fmt.Sprintf("Automaton %s needs review: the judge %s (%s), %s in total. Nothing was approved or passed. Attempts: %s",
		prdID, judgeHoldReason(attempts), judgeBackendList(attempts), judgeTotalElapsed(attempts), strings.Join(judgeAttemptLines(attempts), "; "))
}

// judgeHoldVerdict is the block the PRD records when the judge is held. It
// carries one line per attempt, so the review shows every backend that was
// tried and the time spent on each.
func judgeHoldVerdict(req autonomouspkg.GuardrailInvocation, attempts []judgeAttempt) autonomouspkg.GuardrailVerdict {
	return autonomouspkg.GuardrailVerdict{
		Guardrail: req.Guardrail,
		Outcome:   "block",
		Severity:  "high",
		Category:  autonomouspkg.JudgeTimeoutCategory,
		Summary: fmt.Sprintf("judge for Automaton %s %s (%s), %s in total; the Automaton needs review",
			req.PRDID, judgeHoldReason(attempts), judgeBackendList(attempts), judgeTotalElapsed(attempts)),
		Issues:        judgeAttemptLines(attempts),
		JudgeAttempts: judgeAttemptRecords(attempts),
		VerdictAt:     time.Now(),
	}
}

// judgeAttemptRecords is the structured summary of every attempt, one record
// per backend in the order tried, kept on the hold verdict.
func judgeAttemptRecords(attempts []judgeAttempt) []autonomouspkg.JudgeAttemptRecord {
	out := make([]autonomouspkg.JudgeAttemptRecord, 0, len(attempts))
	for _, a := range attempts {
		rec := autonomouspkg.JudgeAttemptRecord{
			Backend: a.Backend, Started: a.Started, Duration: a.Elapsed.Round(time.Second), Outcome: autonomouspkg.JudgeOutcomeTimedOut,
		}
		if a.NoSlot {
			rec.Outcome = autonomouspkg.JudgeOutcomeNoSlot
		}
		out = append(out, rec)
	}
	return out
}

// judgeAttemptLines words each attempt for the hold verdict: the backend, when
// it started, and what happened.
func judgeAttemptLines(attempts []judgeAttempt) []string {
	lines := make([]string, 0, len(attempts))
	for _, a := range attempts {
		start := a.Started.Format(time.RFC3339)
		if a.NoSlot {
			lines = append(lines, fmt.Sprintf("backend %s, started %s: no capacity slot within the wait, the judge did not run", a.Backend, start))
			continue
		}
		lines = append(lines, fmt.Sprintf("backend %s, started %s: timed out after %s (limit %s)", a.Backend, start, a.Elapsed.Round(time.Second), a.Limit))
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
