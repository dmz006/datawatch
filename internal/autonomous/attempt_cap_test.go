package autonomous

// The daemon-owned attempt cap: a task keeps a total attempt count across
// cancel, reset, review retry and daemon restart. Every test runs on fakes: a
// fake spawn, a fake verifier that always fails, a fake alert sink, a fake
// question store and a recording sleep, so nothing waits and nothing reaches a
// real service.

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// capRig is one Automaton run under an attempt cap on the fakes.
type capRig struct {
	m      *Manager
	api    *API
	dir    string
	spawns escalationSpawnLog
	alerts holdAlertLog
	store  *QuestionStore
	prd    *PRD
}

// newCapRig builds a Manager over dir under cap, with the alert sink and the
// operator question channel wired, and one approved single-task Automaton.
func newCapRig(t *testing.T, dir string, cap config.AttemptCapConfig) *capRig {
	t.Helper()
	fakeLLM := func(_ DecomposeRequest) (string, error) {
		return `{"title":"T","stories":[{"title":"S","tasks":[{"title":"Task","spec":"do it"}]}]}`, nil
	}
	cfg := DefaultConfig()
	cfg.AutoFixRetries = 3
	cfg.Automation.ProtectedPathsAction = "off"
	cfg.AttemptCap = cap
	m, err := NewManager(dir, cfg, fakeLLM)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	api := NewAPI(m)
	drainOnCleanup(t, m, api)
	r := &capRig{m: m, api: api, dir: dir}
	m.SetAlertSink(r.alerts.sink)
	store, err := NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	store.SetChangeHook(func(c QuestionChange) { m.RecordQuestionChange(c) })
	m.SetDecisionGate(store, func() config.AutonomousQuestionsConfig {
		return config.AutonomousQuestionsConfig{Enabled: true}
	})
	r.store = store
	return r
}

// noBackoff is a cap of max total attempts with no wait between attempts.
func noBackoff(max int) config.AttemptCapConfig {
	return config.AttemptCapConfig{MaxTotal: max, BackoffMinutes: []int{}}
}

// addPRD creates the one-task Automaton the rig runs.
func (r *capRig) addPRD(t *testing.T) {
	t.Helper()
	r.prd = newFailureEscalationPRD(t, r.m, t.TempDir(), nil)
}

// run starts the Automaton and waits until it is held for review.
func (r *capRig) run(t *testing.T) {
	t.Helper()
	r.api.SetExecutors(r.spawns.spawn, alwaysFailVerify)
	if err := r.api.Run(r.prd.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	pollEscalationSettled(t, r.m, r.prd.ID, 10*time.Second)
	waitExecutorGone(t, r.api, r.prd.ID, 10*time.Second)
}

func (r *capRig) task(t *testing.T) *Task { t.Helper(); return escalationTask(t, r.m, r.prd.ID) }

// supervisorRetry is a retry by a doer or supervisor session: the review retry
// path, not an operator force reset.
func (r *capRig) supervisorRetry(t *testing.T) {
	t.Helper()
	if _, err := r.api.ResolveTaskReview(r.prd.ID, r.task(t).ID, ReviewResolution{Action: reviewActionRetry}, "doer"); err != nil {
		t.Fatalf("review retry: %v", err)
	}
	pollEscalationSettled(t, r.m, r.prd.ID, 10*time.Second)
	waitExecutorGone(t, r.api, r.prd.ID, 10*time.Second)
}

// TestAttemptCap_CancelAndRetryKeepsCount: the failure ladder resets on a
// review retry, a cancel and a force requeue by a session, the total attempt
// count does not.
func TestAttemptCap_CancelAndRetryKeepsCount(t *testing.T) {
	r := newCapRig(t, t.TempDir(), noBackoff(50))
	r.addPRD(t)
	r.run(t)
	if got := r.task(t); got.AttemptsTotal != 3 || got.EscalationFailures != 3 {
		t.Fatalf("after the first ladder: attempts_total %d escalation_failures %d, want 3 and 3", got.AttemptsTotal, got.EscalationFailures)
	}

	r.supervisorRetry(t)
	if got := r.task(t); got.AttemptsTotal != 6 || got.EscalationFailures != 3 {
		t.Fatalf("after a review retry and a second ladder: attempts_total %d escalation_failures %d, want 6 and 3", got.AttemptsTotal, got.EscalationFailures)
	}

	taskID := r.task(t).ID
	if _, err := r.api.CancelTask(r.prd.ID, taskID, "doer", "start over"); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	if _, err := r.api.ResetTaskWithOptions(r.prd.ID, taskID, "doer", true, false); err != nil {
		t.Fatalf("force reset by a session: %v", err)
	}
	got := r.task(t)
	if got.EscalationFailures != 0 {
		t.Fatalf("the force reset must still restart the ladder: escalation_failures = %d", got.EscalationFailures)
	}
	if got.AttemptsTotal != 6 {
		t.Fatalf("cancel then force reset by a session cleared the count: attempts_total = %d, want 6", got.AttemptsTotal)
	}
}

// TestAttemptCap_RestartKeepsCount: the count is part of the stored task.
func TestAttemptCap_RestartKeepsCount(t *testing.T) {
	dir := t.TempDir()
	r := newCapRig(t, dir, noBackoff(50))
	r.addPRD(t)
	r.run(t)
	if got := r.task(t).AttemptsTotal; got != 3 {
		t.Fatalf("attempts_total = %d, want 3", got)
	}
	// Opening the same directory again is a daemon restart: the new Manager's
	// store reloads its Automata from disk.
	again := newCapRig(t, dir, noBackoff(50))
	if got := escalationTask(t, again.m, r.prd.ID).AttemptsTotal; got != 3 {
		t.Fatalf("after a restart attempts_total = %d, want 3", got)
	}
}

// TestAttemptCap_CapReachedHoldsAndRaisesQuestion: at the cap the task is held,
// one alert and one question reach the operator, and nothing starts it again,
// however often a session retries it.
func TestAttemptCap_CapReachedHoldsAndRaisesQuestion(t *testing.T) {
	r := newCapRig(t, t.TempDir(), noBackoff(4))
	r.addPRD(t)
	r.run(t)
	if got := r.task(t); got.AttemptCapHeld {
		t.Fatalf("held at the cap after 3 of 4 attempts")
	}

	r.supervisorRetry(t) // the fourth attempt runs and fails
	got := r.task(t)
	if n := len(r.spawns.workers()); n != 4 {
		t.Fatalf("spawns = %d, want exactly the cap of 4", n)
	}
	if got.Status != TaskReviewNeeded || !got.AttemptCapHeld || got.AttemptsTotal != 4 {
		t.Fatalf("task status %q attempt_cap_held %v attempts_total %d, want held at the cap with 4", got.Status, got.AttemptCapHeld, got.AttemptsTotal)
	}
	if got.FailureReason == nil || got.FailureReason.Source != attemptCapHoldSource {
		t.Fatalf("FailureReason = %+v, want source %q", got.FailureReason, attemptCapHoldSource)
	}
	if n := len(alertsTitled(&r.alerts, attemptCapAlertTitle)); n != 1 {
		t.Fatalf("cap alerts = %d, want 1", n)
	}
	open := r.store.List(QuestionFilter{State: QuestionOpen, Asker: attemptCapQuestionAsker, PRDID: r.prd.ID, Limit: 10})
	if len(open) != 1 || open[0].TaskID != got.ID {
		t.Fatalf("open cap questions = %+v, want one for the task", open)
	}
	if prd, _ := r.m.Store().GetPRD(r.prd.ID); decisionsByKind(prd)[attemptCapHoldDecisionKind].Kind == "" {
		t.Fatalf("no %s decision written", attemptCapHoldDecisionKind)
	}

	// A session retries again and again: no attempt starts, and the review
	// retry says why.
	for i := 0; i < 3; i++ {
		_, err := r.api.ResolveTaskReview(r.prd.ID, got.ID, ReviewResolution{Action: reviewActionRetry}, "doer")
		if err == nil || !strings.Contains(err.Error(), "attempt cap") {
			t.Fatalf("review retry at the cap: err = %v, want a refusal naming the attempt cap", err)
		}
	}
	if _, err := r.api.ResetTaskWithOptions(r.prd.ID, got.ID, "doer", true, false); err == nil {
		t.Fatalf("a session's force reset of a task held at the cap must be refused")
	}
	if n := len(r.spawns.workers()); n != 4 {
		t.Fatalf("spawns after retries at the cap = %d, want still 4", n)
	}
}

// TestAttemptCap_OperatorForceResetClears: only an operator's force reset
// clears the count and the hold; it is recorded on the Automaton, and the task
// runs again.
func TestAttemptCap_OperatorForceResetClears(t *testing.T) {
	r := newCapRig(t, t.TempDir(), noBackoff(4))
	r.addPRD(t)
	r.run(t)
	r.supervisorRetry(t)
	id := r.task(t).ID
	if !r.task(t).AttemptCapHeld {
		t.Fatalf("setup: task not held at the cap")
	}

	// A plain (not force) reset by the operator does not clear it either.
	if _, err := r.api.ResetTaskWithOptions(r.prd.ID, id, "operator", false, false); err == nil {
		t.Fatalf("a non-force reset of a task held at the cap must be refused")
	}
	if _, err := r.api.ResetTaskWithOptions(r.prd.ID, id, "operator", true, false); err != nil {
		t.Fatalf("operator force reset: %v", err)
	}
	got := r.task(t)
	if got.AttemptsTotal != 0 || got.AttemptCapHeld || got.AttemptNotBefore != nil {
		t.Fatalf("after the operator force reset: attempts_total %d held %v not_before %v, want all clear", got.AttemptsTotal, got.AttemptCapHeld, got.AttemptNotBefore)
	}
	prd, _ := r.m.Store().GetPRD(r.prd.ID)
	d := decisionsByKind(prd)[attemptCapClearedDecisionKind]
	if d.Actor != "operator" || !strings.Contains(d.Note, "4") {
		t.Fatalf("clear decision = %+v, want actor operator and the cleared count 4 in the note", d)
	}
	if open := r.store.List(QuestionFilter{State: QuestionOpen, Asker: attemptCapQuestionAsker, PRDID: r.prd.ID, Limit: 10}); len(open) != 0 {
		t.Fatalf("the cap question is still open after the clear: %+v", open)
	}
	before := len(r.spawns.workers())
	if err := r.api.Run(r.prd.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	pollEscalationSettled(t, r.m, r.prd.ID, 10*time.Second)
	waitExecutorGone(t, r.api, r.prd.ID, 10*time.Second)
	if n := len(r.spawns.workers()) - before; n != 3 {
		t.Fatalf("attempts after the clear = %d, want a fresh ladder of 3", n)
	}
}

// TestAttemptCap_AnswerForceResetByOperatorOnly: the question's force-reset
// answer clears the count when the operator gives it; a session's answer does
// not.
func TestAttemptCap_AnswerForceResetByOperatorOnly(t *testing.T) {
	r := newCapRig(t, t.TempDir(), noBackoff(4))
	r.addPRD(t)
	r.run(t)
	r.supervisorRetry(t)
	id := r.task(t).ID
	open := r.store.List(QuestionFilter{State: QuestionOpen, Asker: attemptCapQuestionAsker, PRDID: r.prd.ID, Limit: 10})
	if len(open) != 1 {
		t.Fatalf("setup: want one cap question, got %d", len(open))
	}
	change := func(actor string) QuestionChange {
		return QuestionChange{Kind: QuestionChangeAnswered, Question: open[0],
			Answer: &Answer{ChosenOptionID: attemptCapActionForceReset, Actor: actor}}
	}
	if !r.api.resolveAttemptCapAnswer(change("session:abc")) {
		t.Fatalf("a cap answer was not recognised")
	}
	if got := r.task(t); got.AttemptsTotal != 4 || !got.AttemptCapHeld {
		t.Fatalf("a session's answer cleared the cap: attempts_total %d held %v", got.AttemptsTotal, got.AttemptCapHeld)
	}
	r.api.resolveAttemptCapAnswer(change("operator"))
	if got := r.task(t); got.AttemptsTotal != 0 || got.AttemptCapHeld || got.ID != id {
		t.Fatalf("the operator's answer did not clear: attempts_total %d held %v", got.AttemptsTotal, got.AttemptCapHeld)
	}
}

// TestAttemptCap_BackoffGrows: after the free attempts each attempt waits the
// next configured delay, and the wait is recorded on the task.
func TestAttemptCap_BackoffGrows(t *testing.T) {
	var mu sync.Mutex
	var waits []time.Duration
	old := attemptCapSleep
	attemptCapSleep = func(_ context.Context, d time.Duration) error {
		mu.Lock()
		defer mu.Unlock()
		waits = append(waits, d)
		return nil
	}
	t.Cleanup(func() { attemptCapSleep = old })

	r := newCapRig(t, t.TempDir(), config.AttemptCapConfig{MaxTotal: 6, FreeAttempts: 1, BackoffMinutes: []int{5, 15, 45}})
	clk := newFakeAttemptClock()
	r.m.attemptClock = clk.Now
	r.addPRD(t)
	r.run(t) // three attempts: the first free, the second after 5 minutes, the third after 15

	mu.Lock()
	defer mu.Unlock()
	if len(waits) != 2 || waits[0] != 5*time.Minute || waits[1] != 15*time.Minute {
		t.Fatalf("waits = %v, want [5m0s 15m0s]", waits)
	}
	got := r.task(t)
	if got.AttemptNotBefore == nil {
		t.Fatalf("no attempt_not_before recorded after the last failed attempt")
	}
}

// TestAttemptCap_DisabledChangesNothing: with the cap off, a task keeps the
// behaviour it had: the ladder alone limits it.
func TestAttemptCap_DisabledChangesNothing(t *testing.T) {
	off := false
	r := newCapRig(t, t.TempDir(), config.AttemptCapConfig{Enabled: &off, MaxTotal: 2})
	r.addPRD(t)
	r.run(t)
	if n := len(r.spawns.workers()); n != 3 {
		t.Fatalf("spawns = %d, want the ladder's 3 with the cap off", n)
	}
	if got := r.task(t); got.AttemptCapHeld {
		t.Fatalf("held at the cap with the cap off")
	}
}

// TestAttemptCap_InfraRetryDoesNotCount: a spawn that fails on infrastructure
// and is retried costs the task no attempt.
func TestAttemptCap_InfraRetryDoesNotCount(t *testing.T) {
	old := infraSleep
	infraSleep = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { infraSleep = old })

	r := newCapRig(t, t.TempDir(), noBackoff(6))
	r.addPRD(t)
	var mu sync.Mutex
	calls := 0
	spawn := func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n <= 2 {
			return SpawnResult{}, errors.New("tmux: server exited unexpectedly")
		}
		return r.spawns.spawn(ctx, req)
	}
	r.api.SetExecutors(spawn, func(context.Context, *PRD, *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "tests pass"}, nil
	})
	if err := r.api.Run(r.prd.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	pollEscalationSettled(t, r.m, r.prd.ID, 10*time.Second)
	waitExecutorGone(t, r.api, r.prd.ID, 10*time.Second)
	if got := r.task(t); got.AttemptsTotal != 1 {
		t.Fatalf("attempts_total = %d after two infrastructure retries and one real attempt, want 1", got.AttemptsTotal)
	}
}

// recordBackoffWaits replaces the back-off sleep with a recorder for one test
// and returns a reader of the waits recorded so far.
func recordBackoffWaits(t *testing.T) func() []time.Duration {
	t.Helper()
	var mu sync.Mutex
	var waits []time.Duration
	old := attemptCapSleep
	attemptCapSleep = func(_ context.Context, d time.Duration) error {
		mu.Lock()
		defer mu.Unlock()
		waits = append(waits, d)
		return nil
	}
	t.Cleanup(func() { attemptCapSleep = old })
	return func() []time.Duration {
		mu.Lock()
		defer mu.Unlock()
		return append([]time.Duration(nil), waits...)
	}
}

// retryAs is a review retry by actor, then waits until the task settles.
func (r *capRig) retryAs(t *testing.T, actor string) {
	t.Helper()
	if _, err := r.api.ResolveTaskReview(r.prd.ID, r.task(t).ID, ReviewResolution{Action: reviewActionRetry}, actor); err != nil {
		t.Fatalf("review retry as %s: %v", actor, err)
	}
	pollEscalationSettled(t, r.m, r.prd.ID, 10*time.Second)
	waitExecutorGone(t, r.api, r.prd.ID, 10*time.Second)
}

// TestAttemptCap_OperatorRetryStartsAtOnce: the operator's own retry inside the
// back-off window starts the next attempt at once. The waits that follow inside
// the new ladder are automatic retries and still apply, and the retry still
// counts toward the cap.
func TestAttemptCap_OperatorRetryStartsAtOnce(t *testing.T) {
	waitsSoFar := recordBackoffWaits(t)
	r := newCapRig(t, t.TempDir(), config.AttemptCapConfig{MaxTotal: 12, FreeAttempts: 1, BackoffMinutes: []int{5, 15, 45}})
	r.m.attemptClock = newFakeAttemptClock().Now
	r.addPRD(t)
	r.run(t) // attempts 1 to 3: waits of 5 and 15 minutes, then 45 minutes owed before attempt 4
	if got := waitsSoFar(); len(got) != 2 {
		t.Fatalf("setup: waits after the first ladder = %v, want two", got)
	}
	if r.task(t).AttemptNotBefore == nil {
		t.Fatalf("setup: no back-off owed after the first ladder")
	}

	r.retryAs(t, "operator") // attempts 4 to 6
	got := waitsSoFar()[2:]
	if len(got) != 2 || got[0] != 45*time.Minute || got[1] != 45*time.Minute {
		t.Fatalf("waits during the operator's ladder = %v, want [45m0s 45m0s]: attempt 4 starts at once, attempts 5 and 6 are automatic retries", got)
	}
	if n := r.task(t).AttemptsTotal; n != 6 {
		t.Fatalf("attempts_total = %d after the operator retry, want 6: the retry still counts", n)
	}
}

// TestAttemptCap_SessionRetryStillWaits: a retry by a doer or supervisor
// session, and the automatic retries of its ladder, all wait their back-off.
func TestAttemptCap_SessionRetryStillWaits(t *testing.T) {
	waitsSoFar := recordBackoffWaits(t)
	r := newCapRig(t, t.TempDir(), config.AttemptCapConfig{MaxTotal: 12, FreeAttempts: 1, BackoffMinutes: []int{5, 15, 45}})
	r.m.attemptClock = newFakeAttemptClock().Now
	r.addPRD(t)
	r.run(t)

	r.retryAs(t, "doer")
	got := waitsSoFar()[2:]
	if len(got) != 3 || got[0] != 45*time.Minute {
		t.Fatalf("waits during the session's ladder = %v, want three waits of 45m0s: attempt 4 waits too", got)
	}
}

// TestAttemptCap_OperatorRetryAtCapStillHeld: the operator's plain retry gets no
// way round the cap: at the cap it is refused and only the force reset clears.
func TestAttemptCap_OperatorRetryAtCapStillHeld(t *testing.T) {
	r := newCapRig(t, t.TempDir(), noBackoff(4))
	r.addPRD(t)
	r.run(t)
	r.supervisorRetry(t) // the fourth attempt: held at the cap
	_, err := r.api.ResolveTaskReview(r.prd.ID, r.task(t).ID, ReviewResolution{Action: reviewActionRetry}, "operator")
	if err == nil || !strings.Contains(err.Error(), "attempt cap") {
		t.Fatalf("operator review retry at the cap: err = %v, want a refusal naming the attempt cap", err)
	}
}
