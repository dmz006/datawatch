package autonomous

// Infrastructure retry exhaustion: a task that has used up its infrastructure
// retries is held for operator review, with one alert and one operator
// question that names the infrastructure cause. The spawn is a fake that fails
// with a transient error, infraSleep is a recorder, and the alert sink and the
// question store are fakes, so no test sleeps or reaches a real service.

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

const (
	holdTestCause = "tmux: server exited unexpectedly"
	holdTestClass = config.InfraRetryClassSpawnError
)

// alertsTitled returns the alerts the log holds with the given title.
func alertsTitled(l *holdAlertLog, title string) []holdAlert {
	var out []holdAlert
	for _, a := range l.all() {
		if a.title == title {
			out = append(out, a)
		}
	}
	return out
}

const infraHoldAlertTitle = "Automaton task held after infrastructure retries"

// alwaysFailSpawn fails every spawn with a transient infrastructure error.
func alwaysFailSpawn(_ context.Context, _ SpawnRequest, _ int) (SpawnResult, error) {
	return SpawnResult{}, errors.New(holdTestCause)
}

// wireInfraHoldRig wires the fake alert sink and, when withQuestions is set, a
// question store with the operator question channel enabled. The change hook
// records the question in the decision log and resolves the held task the way
// an answer does, through the Manager (no run is relaunched).
func wireInfraHoldRig(t *testing.T, r *infraExecRun, withQuestions bool) (*holdAlertLog, *QuestionStore) {
	t.Helper()
	alerts := &holdAlertLog{}
	r.m.SetAlertSink(alerts.sink)
	if !withQuestions {
		return alerts, nil
	}
	store, err := NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	store.SetChangeHook(func(c QuestionChange) {
		r.m.RecordQuestionChange(c)
		if prdID, taskID, req, actor, ok := infraHoldResolution(c); ok {
			if _, err := r.m.ResolveTaskReview(prdID, taskID, req, actor); err != nil {
				t.Errorf("ResolveTaskReview from the answer: %v", err)
			}
		}
	})
	r.m.SetDecisionGate(store, func() config.AutonomousQuestionsConfig {
		return config.AutonomousQuestionsConfig{Enabled: true}
	})
	return alerts, store
}

// useInfraCountAction switches the exhausted action to "count", so a task that
// meets a persistent infrastructure failure takes the failed path the tests of
// that path expect, instead of being held for review.
func useInfraCountAction(m *Manager) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.InfraRetry.ExhaustedAction = config.ExhaustedActionCount
}

// heldTask returns the live Automaton and its only task.
func heldTask(t *testing.T, r *infraExecRun) (*PRD, *Task) {
	t.Helper()
	prd, ok := r.m.Store().GetPRD(r.prd.ID)
	if !ok {
		t.Fatalf("Automaton %s not found", r.prd.ID)
	}
	return prd, &prd.Story[0].Tasks[0]
}

func decisionsByKind(p *PRD) map[string]Decision {
	out := map[string]Decision{}
	for _, d := range p.Decisions {
		out[d.Kind] = d
	}
	return out
}

func TestInfraRetry_RepeatedFailureHolds(t *testing.T) {
	r := newInfraExecRun(t, nil, nil)
	alerts, _ := wireInfraHoldRig(t, r, false)
	got, task := r.run(t, alwaysFailSpawn, passingVerify)

	maxTries := config.InfraRetryDefaultMaxTries
	if task.Status != TaskReviewNeeded {
		t.Fatalf("task status = %q (error %q), want review_needed", task.Status, task.Error)
	}
	if task.EscalationFailures != 0 {
		t.Errorf("EscalationFailures = %d, want 0: the hold counts no failure", task.EscalationFailures)
	}
	if task.RetryCount != 0 || got.RetryCeilingTotal != 0 || len(got.RetryCountsByCategory) != 0 {
		t.Errorf("budget consumed: retry count %d, ceiling %d, categories %v",
			task.RetryCount, got.RetryCeilingTotal, got.RetryCountsByCategory)
	}
	if got.Status != PRDBlocked || !strings.Contains(got.BlockedReason, task.ID) {
		t.Errorf("Automaton status %q reason %q, want blocked for review of task %s", got.Status, got.BlockedReason, task.ID)
	}
	if s := r.slept(); len(s) != maxTries {
		t.Errorf("sleeps = %v, want %d delays", s, maxTries)
	}
	if len(task.InfraRetries) != maxTries || task.InfraRetryCount != maxTries {
		t.Errorf("infra retries = %d, count = %d, want %d", len(task.InfraRetries), task.InfraRetryCount, maxTries)
	}
	if r.spawned() != maxTries+1 {
		t.Errorf("spawns = %d, want %d (the first try and each retry)", r.spawned(), maxTries+1)
	}

	fr := task.FailureReason
	if fr == nil || fr.Category != "infra" || fr.Source != infraRetryHoldSource || fr.Retryable {
		t.Fatalf("FailureReason = %+v, want infra from %s, not retryable", fr, infraRetryHoldSource)
	}
	if !strings.Contains(fr.Detail, holdTestClass) || !strings.Contains(fr.Detail, holdTestCause) {
		t.Errorf("FailureReason detail %q lacks the class or the cause", fr.Detail)
	}
	if !strings.Contains(task.Error, "infrastructure retries") {
		t.Errorf("Error line = %q", task.Error)
	}

	rs := task.ReviewSummary
	if rs == nil {
		t.Fatal("no ReviewSummary")
	}
	for _, want := range []string{
		"Automaton task " + task.ID, "was held after 3 infrastructure retries over ",
		"Infrastructure cause: " + holdTestClass + ": ", holdTestCause,
		"retry 1 (spawn_error)", "retry 2 (spawn_error)", "retry 3 (spawn_error)",
	} {
		if !strings.Contains(rs.Text, want) {
			t.Errorf("review summary lacks %q:\n%s", want, rs.Text)
		}
	}
	if strings.Contains(rs.Text, "PRD") {
		t.Errorf("review summary says PRD:\n%s", rs.Text)
	}

	kinds := decisionsByKind(got)
	for _, k := range []string{reviewNeededDecisionKind, infraRetryExhaustedDecisionKind} {
		if _, ok := kinds[k]; !ok {
			t.Errorf("no %s decision in %+v", k, got.Decisions)
		}
	}
	if len(task.Attempts) != maxTries+1 {
		t.Errorf("attempts = %d, want %d recorded, none counted", len(task.Attempts), maxTries+1)
	}

	held := alertsTitled(alerts, infraHoldAlertTitle)
	if len(held) != 1 {
		t.Fatalf("hold alerts = %d, want 1 (all: %+v)", len(held), alerts.all())
	}
	for _, want := range []string{r.prd.ID, task.ID, holdTestClass, holdTestCause} {
		if !strings.Contains(held[0].body, want) {
			t.Errorf("alert body lacks %q:\n%s", want, held[0].body)
		}
	}
}

func TestInfraRetry_HoldRaisesQuestionNamingCause(t *testing.T) {
	r := newInfraExecRun(t, nil, nil)
	_, store := wireInfraHoldRig(t, r, true)
	r.run(t, alwaysFailSpawn, passingVerify)

	open := store.List(QuestionFilter{State: QuestionOpen})
	if len(open) != 1 {
		t.Fatalf("open questions = %d, want 1: %+v", len(open), open)
	}
	q := open[0]
	prd, task := heldTask(t, r)
	if !strings.HasPrefix(q.Text, "Task "+task.Title+" in Automaton "+automatonLabel(prd)+" was held after 3 infrastructure retries: ") ||
		!strings.Contains(q.Text, holdTestClass+": ") || !strings.Contains(q.Text, holdTestCause) ||
		!strings.HasSuffix(q.Text, "Retry it now or cancel it?") {
		t.Errorf("question text = %q", q.Text)
	}
	if q.AskerKind != QuestionAskerSystem || q.PRDID != prd.ID || q.TaskID != task.ID ||
		q.Priority != QuestionPriorityHigh || !q.Blocking || q.Default != "retry" {
		t.Errorf("question = %+v", q)
	}
	if len(q.Options) != 2 || q.Options[0].ID != "retry" || q.Options[1].ID != "cancel" {
		t.Errorf("options = %+v, want retry and cancel", q.Options)
	}

	// The same hold read again raises no second question.
	err := &InfraSpawnError{Cause: errors.New(holdTestCause)}
	if perr := r.m.holdAfterInfraRetries(context.Background(), prd, task, attemptRun{}, err); perr != nil {
		t.Fatalf("second hold: %v", perr)
	}
	if n := len(store.List(QuestionFilter{})); n != 1 {
		t.Fatalf("questions after a second hold = %d, want 1", n)
	}

	// Answering retry resolves the task back to pending.
	if _, err := store.Answer(q.ID, Answer{ChosenOptionID: "retry", Actor: "operator", Surface: "test"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	prd, task = heldTask(t, r)
	if normTaskStatus(task.Status) != TaskPending {
		t.Fatalf("task status = %q, want pending", task.Status)
	}
	if task.ReviewSummary != nil || task.FailureReason != nil || task.InfraRetryCount != 0 {
		t.Errorf("hold not cleared: summary %v, reason %+v, retry count %d",
			task.ReviewSummary != nil, task.FailureReason, task.InfraRetryCount)
	}
	if prd.Status != PRDRunning {
		t.Errorf("Automaton status = %q, want running again", prd.Status)
	}
	if st, _ := store.Get(q.ID); st.State != QuestionAnswered {
		t.Errorf("question state = %q, want answered", st.State)
	}
	if !strings.Contains(task.PriorAttemptHint, "operator has asked for another attempt") {
		t.Errorf("retry hint = %q", task.PriorAttemptHint)
	}
}

func TestInfraRetry_HoldWithoutQuestionStoreAlertsOnly(t *testing.T) {
	r := newInfraExecRun(t, nil, nil)
	alerts, _ := wireInfraHoldRig(t, r, false)
	got, task := r.run(t, alwaysFailSpawn, passingVerify)

	if task.Status != TaskReviewNeeded || got.Status != PRDBlocked {
		t.Fatalf("task %q, Automaton %q, want review_needed and blocked", task.Status, got.Status)
	}
	held := alertsTitled(alerts, infraHoldAlertTitle)
	if len(held) != 1 {
		t.Fatalf("hold alerts = %d, want 1", len(held))
	}
	for _, want := range []string{holdTestClass, holdTestCause} {
		if !strings.Contains(held[0].body, want) {
			t.Errorf("alert body lacks %q:\n%s", want, held[0].body)
		}
		if task.ReviewSummary == nil || !strings.Contains(task.ReviewSummary.Text, want) {
			t.Errorf("review summary lacks %q", want)
		}
	}
}

// A question store that is wired but whose channel is off, and a policy that
// turns the question off, raise nothing; the alert and the summary remain.
func TestInfraRetry_HoldQuestionSwitchedOff(t *testing.T) {
	cases := []struct {
		name   string
		adjust func(*config.InfraRetryConfig)
		wire   func(r *infraExecRun, store *QuestionStore)
	}{
		{"channel disabled", nil, func(r *infraExecRun, store *QuestionStore) {
			r.m.SetDecisionGate(store, func() config.AutonomousQuestionsConfig { return config.AutonomousQuestionsConfig{} })
		}},
		{"raise_question false", func(c *config.InfraRetryConfig) {
			off := false
			c.RaiseQuestion = &off
		}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newInfraExecRun(t, tc.adjust, nil)
			alerts, store := wireInfraHoldRig(t, r, true)
			if tc.wire != nil {
				tc.wire(r, store)
			}
			_, task := r.run(t, alwaysFailSpawn, passingVerify)
			if task.Status != TaskReviewNeeded {
				t.Fatalf("task status = %q, want review_needed", task.Status)
			}
			if n := len(store.List(QuestionFilter{})); n != 0 {
				t.Errorf("questions = %d, want none", n)
			}
			if n := len(alertsTitled(alerts, infraHoldAlertTitle)); n != 1 {
				t.Errorf("hold alerts = %d, want 1", n)
			}
		})
	}
}

// Resolving the held task some other way withdraws the question that is still
// open, with a reason.
func TestInfraRetry_HoldResolvedElsewhereWithdrawsQuestion(t *testing.T) {
	for _, action := range []string{reviewActionRetry, reviewActionCancel} {
		t.Run(action, func(t *testing.T) {
			r := newInfraExecRun(t, nil, nil)
			_, store := wireInfraHoldRig(t, r, true)
			r.run(t, alwaysFailSpawn, passingVerify)
			open := store.List(QuestionFilter{State: QuestionOpen})
			if len(open) != 1 {
				t.Fatalf("open questions = %d, want 1", len(open))
			}
			prd, task := heldTask(t, r)
			if _, err := r.m.ResolveTaskReview(prd.ID, task.ID, ReviewResolution{Action: action}, "tester"); err != nil {
				t.Fatalf("ResolveTaskReview: %v", err)
			}
			q, _ := store.Get(open[0].ID)
			if q.State != QuestionWithdrawn {
				t.Fatalf("question state = %q, want withdrawn", q.State)
			}
			if n := len(store.List(QuestionFilter{State: QuestionOpen})); n != 0 {
				t.Errorf("open questions = %d, want 0", n)
			}
		})
	}
}

// An answer through the API resolves the held task and relaunches the run.
func TestInfraRetry_AnswerThroughAPIRelaunchesRun(t *testing.T) {
	r := newInfraExecRun(t, nil, nil)
	_, store := wireInfraHoldRig(t, r, true)
	store.SetChangeHook(func(c QuestionChange) {
		r.m.RecordQuestionChange(c)
		r.api.OnQuestionChange(c)
	})
	var mu sync.Mutex
	healed := false
	spawn := func(ctx context.Context, req SpawnRequest, n int) (SpawnResult, error) {
		mu.Lock()
		defer mu.Unlock()
		if healed {
			return SpawnResult{}, nil
		}
		return alwaysFailSpawn(ctx, req, n)
	}
	r.run(t, spawn, passingVerify)
	open := store.List(QuestionFilter{State: QuestionOpen})
	if len(open) != 1 {
		t.Fatalf("open questions = %d, want 1", len(open))
	}
	mu.Lock()
	healed = true
	mu.Unlock()
	if _, err := store.Answer(open[0].ID, Answer{ChosenOptionID: "retry", Actor: "operator"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	got := waitForPRDTerminal(t, r.m, r.prd.ID)
	if got.Status != PRDCompleted || got.Story[0].Tasks[0].Status != TaskCompleted {
		t.Fatalf("Automaton %q, task %q, want both completed", got.Status, got.Story[0].Tasks[0].Status)
	}
}

func TestInfraRetry_ExhaustedCountAction(t *testing.T) {
	r := newInfraExecRun(t, func(c *config.InfraRetryConfig) { c.ExhaustedAction = config.ExhaustedActionCount }, nil)
	alerts, store := wireInfraHoldRig(t, r, true)
	got, task := r.run(t, alwaysFailSpawn, passingVerify)

	if task.Status == TaskReviewNeeded || got.Status == PRDBlocked {
		t.Fatalf("task %q, Automaton %q: the count action must not hold", task.Status, got.Status)
	}
	if task.Status != TaskFailed {
		t.Errorf("task status = %q, want failed by the existing path", task.Status)
	}
	if task.EscalationFailures != 1 {
		t.Errorf("EscalationFailures = %d, want 1 normal failure counted", task.EscalationFailures)
	}
	if task.FailureReason != nil && task.FailureReason.Source == infraRetryHoldSource {
		t.Errorf("FailureReason = %+v, want no hold", task.FailureReason)
	}
	if n := len(alertsTitled(alerts, infraHoldAlertTitle)); n != 0 {
		t.Errorf("hold alerts = %d, want 0", n)
	}
	if n := len(store.List(QuestionFilter{})); n != 0 {
		t.Errorf("questions = %d, want 0", n)
	}
	if _, ok := decisionsByKind(got)[infraRetryExhaustedDecisionKind]; ok {
		t.Error("an infra_retry_exhausted decision was written for the count action")
	}
	if s := r.slept(); len(s) != config.InfraRetryDefaultMaxTries {
		t.Errorf("sleeps = %v, want %d delays", s, config.InfraRetryDefaultMaxTries)
	}
}
