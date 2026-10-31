// Infrastructure retry exhaustion: what happens when a task has used up its
// infrastructure retries.
//
// The policy is config.InfraRetryConfig.ExhaustedAction. "count" books the
// failure as one normal failed attempt, so the task takes the same path any
// failed attempt takes. "hold", the default, parks the task in
// TaskReviewNeeded without counting a failure, raises one alert, and, when
// the operator question channel is on, asks the operator whether to retry the
// task or cancel it. The held task is resolved the way any held task is, by
// ResolveTaskReview; an answer to the question does exactly that.
package autonomous

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

const (
	// infraRetryHoldSource is the FailureReason.Source of a task held after
	// its infrastructure retries ran out.
	infraRetryHoldSource = "infra-retry-hold"
	// infraRetryExhaustedDecisionKind is the Decision.Kind written beside
	// review_needed when the retries ran out.
	infraRetryExhaustedDecisionKind = "infra_retry_exhausted"
	// infraHoldQuestionAsker is the asked_by of the operator question raised
	// for a held task; it is also how the question is found again.
	infraHoldQuestionAsker = "infra-retry-hold"
	// infraHoldQuestionLabel is the question's label.
	infraHoldQuestionLabel = "decision"
	// infraHoldWithdrawActor is the actor of a withdrawal no operator made.
	infraHoldWithdrawActor = "autonomous"
)

// isInfraText reports whether the text of a failed attempt names an
// infrastructure failure.
func isInfraText(text string) bool {
	_, ok := ClassifyInfraText(text)
	return ok
}

// infraRetriesUsedUp reports whether err is a transient infrastructure failure
// that the policy retries and the task has already used all its tries on: the
// feature is on, the class is listed, and the task's consecutive retry count
// has reached max_tries.
func (m *Manager) infraRetriesUsedUp(t *Task, err error) bool {
	cfg := m.infraRetryConfig()
	if !cfg.EffectiveEnabled() {
		return false
	}
	class, ok := ClassifyInfraFailure(err)
	if !ok || !cfg.HasCategory(class) {
		return false
	}
	var used int
	m.store.WithLock(func() { used = t.InfraRetryCount })
	return used >= cfg.EffectiveMaxTries()
}

// infraRetriesExhausted applies the exhausted action to a task whose
// infrastructure retries are used up. With the "count" action it records one
// normal failed attempt, categorised by the failure class, and returns
// handled=false so the caller's existing failure path continues. With the
// "hold" action it parks the task for operator review (holdAfterInfraRetries)
// and returns handled=true, so the caller stops working on the task.
func (m *Manager) infraRetriesExhausted(ctx context.Context, prd *PRD, t *Task, run attemptRun, err error) (handled bool, rerr error) {
	cfg := m.infraRetryConfig()
	if cfg.EffectiveExhaustedAction() != config.ExhaustedActionCount {
		return true, m.holdAfterInfraRetries(ctx, prd, t, run, err)
	}
	class, _ := ClassifyInfraFailure(err)
	m.recordFailedAttempt(ctx, prd, t, run, attemptFailure{
		Outcome:  attemptOutcomeForError(err),
		Category: class,
		Summary:  boundInfraReason(err.Error()),
	})
	return false, nil
}

// holdAfterInfraRetries parks t in TaskReviewNeeded because err is an
// infrastructure failure and the retries are used up. The failed attempt is
// recorded without counting toward the task's failures, so the failure
// escalation budget is untouched. The task gets a FailureReason of category
// infra with Source infra-retry-hold and Retryable false, a ReviewSummary that
// names the infrastructure cause and lists each retry, a review_needed and an
// infra_retry_exhausted decision, and a learning. Then one alert is raised and,
// when the operator question channel is on, one question. It returns the error
// from saving the task; the other side effects are best effort.
func (m *Manager) holdAfterInfraRetries(ctx context.Context, prd *PRD, t *Task, run attemptRun, err error) error {
	class, _ := ClassifyInfraFailure(err)
	cause := boundInfraReason(err.Error())
	m.recordFailedAttempt(ctx, prd, t, run, attemptFailure{
		Outcome: attemptOutcomeForError(err), Category: infraRetryFailureCategory, Summary: cause, Requeue: true,
	})
	var retries int
	m.store.WithLock(func() { retries = t.InfraRetryCount })

	_, perr := m.parkTaskForReview(prd, t, reviewParking{
		reason: FailureReason{
			Category: infraRetryFailureCategory, Source: infraRetryHoldSource,
			Detail: boundInfraReason(class + ": " + cause),
		},
		describe: func(t *Task) (string, string) {
			return fmt.Sprintf("held for operator review after %d infrastructure retries (%s)", retries, class),
				fmt.Sprintf("task %s held for operator review after %d infrastructure retries: %s", t.ID, retries, class)
		},
		report: func(t *Task, now time.Time) *ReviewSummary {
			return buildInfraReviewSummary(t, now, class, cause)
		},
		extraDecisions: []Decision{{
			Kind:            infraRetryExhaustedDecisionKind,
			Actor:           "autonomous",
			FailureCategory: infraRetryFailureCategory,
			Note: fmt.Sprintf("task %s used up %d infrastructure retries (%s: %s); the failure was not counted",
				t.ID, retries, class, cause),
		}},
	})
	if perr != nil {
		return perr
	}
	m.raiseInfraHoldAlert(prd, t, class, cause, retries)
	m.raiseInfraHoldQuestion(prd, t, class, cause, retries)
	return nil
}

// buildInfraReviewSummary renders the operator's report for t held after its
// infrastructure retries: the infrastructure cause, the time the retries took,
// each retry record, then every recorded attempt. It reads t without locking,
// so callers that share t with running goroutines call it under the store lock.
func buildInfraReviewSummary(t *Task, now time.Time, class, cause string) *ReviewSummary {
	var b strings.Builder
	retries := t.InfraRetryCount
	elapsed := time.Duration(0)
	if first := len(t.InfraRetries) - retries; retries > 0 && first >= 0 {
		elapsed = now.Sub(t.InfraRetries[first].At).Round(time.Second)
	}
	fmt.Fprintf(&b, "The Automaton task %s (%s) was held after %d infrastructure retries over %s.\n",
		t.ID, t.Title, retries, elapsed)
	fmt.Fprintf(&b, "Infrastructure cause: %s: %s\n", class, cause)
	b.WriteString("The failure was not counted against the task's failed attempts.\n\n")
	if len(t.InfraRetries) == 0 {
		b.WriteString("No infrastructure retries were recorded.\n")
	} else {
		b.WriteString("Infrastructure retries:\n")
	}
	for _, r := range t.InfraRetries {
		fmt.Fprintf(&b, "- retry %d (%s) at %s, waited %d s: %s\n",
			r.Number, r.Class, r.At.Format("15:04:05"), r.DelaySeconds, r.Reason)
	}
	b.WriteString("\n")
	return finishReviewSummary(&b, t, now)
}

// raiseInfraHoldAlert sends the one operator alert for a task held after its
// infrastructure retries, through the sink raiseReviewAlert uses. Nothing is
// sent when no alert sink is wired.
func (m *Manager) raiseInfraHoldAlert(prd *PRD, t *Task, class, cause string, retries int) {
	m.mu.Lock()
	alert := m.alertSink
	m.mu.Unlock()
	if alert == nil {
		return
	}
	body := fmt.Sprintf(
		"Automaton: %s (%s)\nTask: %s (%s)\nInfrastructure failure: %s\nCause: %s\nInfrastructure retries used: %d\n"+
			"Ways to act: retry the task now, which starts it with a fresh set of retries, "+
			"or cancel it. Answer the operator question for the task, or resolve the task review.",
		prd.ID, prd.Title, t.ID, t.Title, class, cause, retries)
	alert("error", "Automaton task held after infrastructure retries", body)
}

// operatorQuestionChannel returns the question store wired into the Manager
// and whether the operator question channel (autonomous.questions.enabled) is
// on. A store that is not wired reads as off.
func (m *Manager) operatorQuestionChannel() (*QuestionStore, bool) {
	m.mu.Lock()
	store, cfgFn := m.decisionStore, m.decisionGateCfg
	m.mu.Unlock()
	if store == nil || cfgFn == nil {
		return nil, false
	}
	return store, cfgFn().Enabled
}

// infraHoldQuestionKey is the dedupe key of the question for one hold: a hash
// of the Automaton, the task, the failure class and the retry count, so the
// same hold read twice raises one question while it is open.
func infraHoldQuestionKey(prdID, taskID, class string, retries int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d", prdID, taskID, class, retries)))
	return hex.EncodeToString(sum[:])
}

// raiseInfraHoldQuestion raises the one operator question for a held task: it
// starts with the infrastructure cause and offers retry or cancel. Nothing is
// raised when autonomous.infra_retry.raise_question is off, no question store
// is wired, or the operator question channel is disabled; the alert and the
// review summary carry the cause then. A question already open for the same
// hold is not raised again. A failure is logged and never changes the hold.
func (m *Manager) raiseInfraHoldQuestion(prd *PRD, t *Task, class, cause string, retries int) {
	if !m.infraRetryConfig().EffectiveRaiseQuestion() {
		return
	}
	store, enabled := m.operatorQuestionChannel()
	if !enabled {
		return
	}
	cause = strings.TrimRight(strings.Join(strings.Fields(cause), " "), ".")
	q := Question{
		Label:     infraHoldQuestionLabel,
		AskedBy:   infraHoldQuestionAsker,
		AskerKind: QuestionAskerSystem,
		Context:   QuestionContextTask,
		PRDID:     prd.ID,
		TaskID:    t.ID,
		Text: fmt.Sprintf("Task %s in Automaton %s was held after %d infrastructure retries: %s: %s. Retry it now or cancel it?",
			t.Title, automatonLabel(prd), retries, class, cause),
		Options: []QuestionOption{
			{ID: reviewActionRetry, Label: "Retry the task now",
				Tradeoffs: "Runs the task again with a fresh set of infrastructure retries; it is held again if the cause persists."},
			{ID: reviewActionCancel, Label: "Cancel the task",
				Tradeoffs: "Cancels the task; tasks that depend on it do not run."},
		},
		Default:          reviewActionRetry,
		Priority:         QuestionPriorityHigh,
		Blocking:         true,
		SourceLineSHA256: infraHoldQuestionKey(prd.ID, t.ID, class, retries),
	}
	if _, _, err := store.RaiseUnlessOpenSourceLine(q); err != nil {
		log.Printf("[autonomous] prd=%s task=%s: raise infrastructure hold question: %v", prd.ID, t.ID, err)
	}
}

// automatonLabel names an Automaton in running text: its title, or its id when
// it has none.
func automatonLabel(prd *PRD) string {
	if strings.TrimSpace(prd.Title) != "" {
		return prd.Title
	}
	return prd.ID
}

// withdrawInfraHoldQuestions withdraws the open infrastructure hold questions
// of a task, with reason. A task resolved some other way than by answering its
// question leaves no question waiting for an answer that no longer matters.
// It does nothing when no question store is wired or no such question is open.
func (m *Manager) withdrawInfraHoldQuestions(prdID, taskID, actor, reason string) {
	m.mu.Lock()
	store := m.decisionStore
	m.mu.Unlock()
	if store == nil {
		return
	}
	if actor == "" {
		actor = infraHoldWithdrawActor
	}
	open := store.List(QuestionFilter{State: QuestionOpen, Asker: infraHoldQuestionAsker, PRDID: prdID, Limit: 1000})
	for _, q := range open {
		if q.TaskID != taskID {
			continue
		}
		if _, err := store.Withdraw(q.ID, actor, reason); err != nil && !errors.Is(err, ErrQuestionNotOpen) {
			log.Printf("[autonomous] prd=%s task=%s: withdraw infrastructure hold question %s: %v", prdID, taskID, q.ID, err)
		}
	}
}

// infraHoldResolution reads an answered infrastructure hold question as the
// operator's decision on the held task. ok is false for any other change: not
// an answer, not a question of this kind, or an answer that names neither the
// retry nor the cancel option.
func infraHoldResolution(c QuestionChange) (prdID, taskID string, req ReviewResolution, actor string, ok bool) {
	q := c.Question
	if c.Kind != QuestionChangeAnswered || c.Answer == nil ||
		q.AskedBy != infraHoldQuestionAsker || q.AskerKind != QuestionAskerSystem ||
		q.PRDID == "" || q.TaskID == "" {
		return "", "", ReviewResolution{}, "", false
	}
	switch c.Answer.ChosenOptionID {
	case reviewActionRetry, reviewActionCancel:
	default:
		return "", "", ReviewResolution{}, "", false
	}
	actor = c.Answer.Actor
	if actor == "" {
		actor = reviewDecisionActor
	}
	req = ReviewResolution{Action: c.Answer.ChosenOptionID, Note: "answered operator question " + q.ID}
	return q.PRDID, q.TaskID, req, actor, true
}

// resolveInfraHoldAnswer resolves the held task an answered infrastructure
// hold question is about, with the action the answer chose, through
// ResolveTaskReview so a live executor is woken or the run relaunched. It
// reports whether the change was such an answer. A task that is no longer held
// is left as it is.
func (a *API) resolveInfraHoldAnswer(c QuestionChange) bool {
	prdID, taskID, req, actor, ok := infraHoldResolution(c)
	if !ok {
		return false
	}
	if _, err := a.ResolveTaskReview(prdID, taskID, req, actor); err != nil && !errors.Is(err, ErrTaskNotInReview) {
		log.Printf("[autonomous] prd=%s task=%s: %s after question %s answered: %v", prdID, taskID, req.Action, c.Question.ID, err)
	}
	return true
}
