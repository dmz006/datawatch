// Attempt cap — the daemon-owned total attempt count, back-off and hold.
//
// The failure escalation ladder counts failed attempts since the last
// operator action, and a reset, a review retry or a cancel-and-requeue starts
// it over. A model that cancels and retries a task can therefore loop for as
// long as it likes. The attempt cap closes that: every launch of a worker for
// a task adds one to Task.AttemptsTotal, a stored count that no reset, cancel,
// review retry or restart clears. The first free_attempts attempts start at
// once; each later attempt waits a growing delay after the previous one ended.
// When the count reaches the cap the task is held for the operator with an
// alert and a question, and nothing starts it again. Only an operator's force
// reset clears the count, and the clear is written to the Automaton's decision
// log. The policy is config.AttemptCapConfig (autonomous.attempt_cap) with a
// per-project override of the cap (projects.<name>.policy.attempt_cap).
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
	// attemptCapHoldSource is the FailureReason.Source of a task held at the cap.
	attemptCapHoldSource = "attempt-cap-hold"
	// attemptCapHoldDecisionKind is the Decision.Kind written when a task is
	// held at the cap.
	attemptCapHoldDecisionKind = "attempt_cap_hold"
	// attemptCapClearedDecisionKind is the Decision.Kind written when the
	// operator's force reset clears the count.
	attemptCapClearedDecisionKind = "attempt_cap_cleared"
	// attemptCapAnswerIgnoredDecisionKind is the Decision.Kind written when a
	// force-reset answer to the cap question came from a caller that is not the
	// operator, so nothing was cleared.
	attemptCapAnswerIgnoredDecisionKind = "attempt_cap_answer_ignored"
	// attemptCapQuestionAsker is the asked_by of the question raised at the cap.
	attemptCapQuestionAsker = "attempt-cap-hold"
	// attemptCapActionForceReset is the question option that clears the count.
	attemptCapActionForceReset = "force_reset"
	// attemptCapAlertTitle is the title of the one alert raised at the cap.
	attemptCapAlertTitle = "Automaton task reached its attempt cap"
	// attemptCapCategory is the failure category of a task held at the cap.
	attemptCapCategory = "policy"
)

// attemptCapSleep waits d or until ctx is done, returning ctx.Err() in that
// case. Tests replace it so none of them sleeps.
var attemptCapSleep = func(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// isOperatorActor reports whether actor names the operator: the one caller
// whose force reset may clear the attempt count. A doer, a supervisor, a
// housekeeping repair and a session all carry another actor name.
func isOperatorActor(actor string) bool { return actor == operatorActor }

// IsOperatorActor is isOperatorActor for the surfaces: REST, MCP and the
// command line decide the actor of a reset before they call the manager.
func IsOperatorActor(actor string) bool { return isOperatorActor(actor) }

// attemptCapConfig returns the attempt cap policy in effect for prd: the
// daemon block, with the cap itself replaced by the project's override when
// the Automaton's project sets one. maxTotal is the cap in effect.
func (m *Manager) attemptCapConfig(prd *PRD) (cfg config.AttemptCapConfig, maxTotal int) {
	m.mu.Lock()
	cfg = m.cfg.AttemptCap.Clone()
	m.mu.Unlock()
	maxTotal = cfg.EffectiveMaxTotal()
	if prd != nil {
		if pol, ok := m.effectivePolicyOf(prd); ok && policyProjectTier(pol.AttemptCap.Source) && pol.AttemptCap.Value > 0 {
			maxTotal = pol.AttemptCap.Value
		}
	}
	return cfg, maxTotal
}

// attemptCapOn reports whether the cap applies to prd's tasks, and the cap.
func (m *Manager) attemptCapOn(prd *PRD) (on bool, maxTotal int) {
	cfg, maxTotal := m.attemptCapConfig(prd)
	return cfg.EffectiveEnabled(), maxTotal
}

// attemptCapReached reports whether t has used every attempt the cap allows.
func (m *Manager) attemptCapReached(prd *PRD, t *Task) bool {
	on, maxTotal := m.attemptCapOn(prd)
	if !on {
		return false
	}
	var total int
	m.store.WithLock(func() { total = t.AttemptsTotal })
	return total >= maxTotal
}

// noteAttemptStarted adds one to the task's total attempt count and clears the
// back-off wait the attempt has now served. It is called once per launch of a
// worker for the task, never for a relaunch that costs the task no attempt.
func (m *Manager) noteAttemptStarted(prd *PRD, t *Task) {
	m.store.WithLock(func() {
		t.AttemptsTotal++
		t.AttemptNotBefore = nil
	})
	if err := m.store.SaveTask(t); err != nil {
		log.Printf("[autonomous] prd=%s task=%s: save attempt count: %v", prd.ID, t.ID, err)
	}
}

// attemptBackoffNotBefore returns the earliest time the attempt after the one
// that ended at ended may start, or nil when it starts at once. total is the
// count of attempts already started.
func attemptBackoffNotBefore(cfg config.AttemptCapConfig, total int, ended time.Time) *time.Time {
	if !cfg.EffectiveEnabled() {
		return nil
	}
	minutes := cfg.BackoffBefore(total + 1)
	if minutes <= 0 {
		return nil
	}
	at := ended.Add(time.Duration(minutes) * time.Minute)
	return &at
}

// attemptCapGate runs before an attempt starts. It holds the task for the
// operator when the cap is reached (stop true), and otherwise waits out the
// back-off that the previous attempt left. It stops, with the context error
// dropped, when the wait is ended by the executor stopping. A task that is not
// held and has no wait to serve passes straight through.
func (m *Manager) attemptCapGate(ctx context.Context, prd *PRD, t *Task) (stop bool, err error) {
	on, _ := m.attemptCapOn(prd)
	if !on {
		return false, nil
	}
	if m.attemptCapReached(prd, t) {
		m.relocateCheckpoint(prd, t)
		return true, m.holdAtAttemptCap(prd, t, "", "")
	}
	var until *time.Time
	var total int
	m.store.WithLock(func() {
		total = t.AttemptsTotal
		if t.AttemptNotBefore != nil {
			c := *t.AttemptNotBefore
			until = &c
		}
	})
	if until == nil {
		return false, nil
	}
	wait := until.Sub(m.attemptNow())
	if wait <= 0 {
		m.store.WithLock(func() { t.AttemptNotBefore = nil })
		return false, m.store.SaveTask(t)
	}
	reason := fmt.Sprintf("attempt back-off: attempt %d starts at %s", total+1, until.Format("15:04:05"))
	m.store.WithLock(func() { t.WaitReason = reason })
	if serr := m.store.SaveTask(t); serr != nil {
		log.Printf("[autonomous] prd=%s task=%s: save attempt back-off wait: %v", prd.ID, t.ID, serr)
	}
	werr := attemptCapSleep(ctx, wait)
	m.store.WithLock(func() { t.WaitReason = "" })
	if serr := m.store.SaveTask(t); serr != nil {
		log.Printf("[autonomous] prd=%s task=%s: clear attempt back-off wait: %v", prd.ID, t.ID, serr)
	}
	if werr != nil {
		return true, nil
	}
	return false, nil
}

// holdAtAttemptCap parks t in TaskReviewNeeded because it has used every
// attempt the cap allows. The task keeps its attempts and gets a report, a
// FailureReason with Source attempt-cap-hold, AttemptCapHeld set, an
// attempt_cap_hold decision, one alert and, when the operator question channel
// is on, one question. lastCategory and detail describe the last failure and
// may be empty. It returns the error from saving the task.
func (m *Manager) holdAtAttemptCap(prd *PRD, t *Task, lastCategory, detail string) error {
	last, total, maxTotal, err := m.parkAtAttemptCap(prd, t, detail, "")
	if err != nil {
		return err
	}
	m.raiseAttemptCapAlert(prd, t, total, maxTotal, last)
	m.raiseAttemptCapQuestion(prd, t, total, maxTotal)
	return nil
}

// parkAtAttemptCap is the hold of holdAtAttemptCap without the alert and the
// question: it parks t in TaskReviewNeeded with AttemptCapHeld, the report, the
// FailureReason and the attempt_cap_hold decision. detail is the failure detail
// (a default naming the count when empty); decisionNote replaces the decision's
// text when set. It returns the last attempt (or nil), the count and the cap.
func (m *Manager) parkAtAttemptCap(prd *PRD, t *Task, detail, decisionNote string) (last *TaskAttempt, total, maxTotal int, err error) {
	_, maxTotal = m.attemptCapConfig(prd)
	m.store.WithLock(func() { total = t.AttemptsTotal })
	if detail == "" {
		detail = fmt.Sprintf("%d attempts were started, the cap is %d", total, maxTotal)
	}
	if decisionNote == "" {
		decisionNote = fmt.Sprintf("task %s reached the attempt cap (%d of %d); no attempt starts until the operator force-resets it",
			t.ID, total, maxTotal)
	}
	last, err = m.parkTaskForReview(prd, t, reviewParking{
		reason: FailureReason{Category: attemptCapCategory, Detail: detail, Source: attemptCapHoldSource},
		describe: func(t *Task) (string, string) {
			t.AttemptCapHeld = true
			return fmt.Sprintf("held at the attempt cap: %d attempts started (cap %d)", t.AttemptsTotal, maxTotal),
				fmt.Sprintf("task %s held at the attempt cap: %d attempts started (cap %d); only an operator force reset starts it again", t.ID, t.AttemptsTotal, maxTotal)
		},
		report: func(t *Task, now time.Time) *ReviewSummary {
			return buildAttemptCapSummary(t, now, maxTotal)
		},
		extraDecisions: []Decision{{
			Kind:  attemptCapHoldDecisionKind,
			Actor: "autonomous",
			Note:  decisionNote,
		}},
	})
	return last, total, maxTotal, err
}

// buildAttemptCapSummary renders the operator's report for t held at the cap.
// It reads t without locking, so callers share t under the store lock.
func buildAttemptCapSummary(t *Task, now time.Time, maxTotal int) *ReviewSummary {
	var b strings.Builder
	fmt.Fprintf(&b, "Task %s (%s) reached the attempt cap: %d attempts were started and the cap is %d.\n",
		t.ID, t.Title, t.AttemptsTotal, maxTotal)
	b.WriteString("No attempt starts it again, whoever asks, until the operator force-resets the task, which clears the count.\n\n")
	return finishReviewSummary(&b, t, now)
}

// raiseAttemptCapAlert sends the one operator alert for a task held at the
// cap. Nothing is sent when no alert sink is wired.
func (m *Manager) raiseAttemptCapAlert(prd *PRD, t *Task, total, maxTotal int, last *TaskAttempt) {
	m.mu.Lock()
	alert := m.alertSink
	m.mu.Unlock()
	if alert == nil {
		return
	}
	worker := "none recorded"
	if last != nil {
		worker = escalationWorkerLabel(*last)
	}
	body := fmt.Sprintf(
		"Automaton: %s (%s)\nTask: %s (%s)\nAttempts started: %d of %d\nLast worker: %s\n"+
			"Nothing starts the task again until you force-reset it, which clears the count. "+
			"Answer the operator question for the task, or cancel it.",
		prd.ID, prd.Title, t.ID, t.Title, total, maxTotal, worker)
	alert("error", attemptCapAlertTitle, body)
}

// attemptCapQuestionKey is the dedupe key of the question for one hold.
func attemptCapQuestionKey(prdID, taskID string, total int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("attempt-cap\x00%s\x00%s\x00%d", prdID, taskID, total)))
	return hex.EncodeToString(sum[:])
}

// raiseAttemptCapQuestion raises the one operator question for a task held at
// the cap: clear the count and run it again, or cancel it. The default is to
// leave it held. Nothing is raised when no question store is wired or the
// operator question channel is off; the alert and the report carry the facts
// then. A question already open for the same hold is not raised again.
func (m *Manager) raiseAttemptCapQuestion(prd *PRD, t *Task, total, maxTotal int) {
	store, enabled := m.operatorQuestionChannel()
	if !enabled {
		return
	}
	q := Question{
		Label:     infraHoldQuestionLabel,
		AskedBy:   attemptCapQuestionAsker,
		AskerKind: QuestionAskerSystem,
		Context:   QuestionContextTask,
		PRDID:     prd.ID,
		TaskID:    t.ID,
		Text: fmt.Sprintf("Task %s in Automaton %s used %d of %d attempts and is held. Clear the attempt count and run it again, or cancel it?",
			t.Title, automatonLabel(prd), total, maxTotal),
		Options: []QuestionOption{
			{ID: attemptCapActionForceReset, Label: "Clear the count and run it again",
				Tradeoffs: "Force-resets the task as the operator: the attempt count goes back to zero and the task runs a fresh set of attempts."},
			{ID: reviewActionCancel, Label: "Cancel the task",
				Tradeoffs: "Cancels the task; tasks that depend on it do not run."},
		},
		Default:          "hold",
		Priority:         QuestionPriorityHigh,
		Blocking:         true,
		SourceLineSHA256: attemptCapQuestionKey(prd.ID, t.ID, total),
	}
	if _, _, err := store.RaiseUnlessOpenSourceLine(q); err != nil {
		log.Printf("[autonomous] prd=%s task=%s: raise attempt cap question: %v", prd.ID, t.ID, err)
	}
}

// withdrawAttemptCapQuestions withdraws the open attempt cap questions of a
// task, with reason, so no question waits for an answer that no longer matters.
func (m *Manager) withdrawAttemptCapQuestions(prdID, taskID, actor, reason string) {
	m.mu.Lock()
	store := m.decisionStore
	m.mu.Unlock()
	if store == nil {
		return
	}
	if actor == "" {
		actor = infraHoldWithdrawActor
	}
	for _, q := range store.List(QuestionFilter{State: QuestionOpen, Asker: attemptCapQuestionAsker, PRDID: prdID, Limit: 1000}) {
		if q.TaskID != taskID {
			continue
		}
		if _, err := store.Withdraw(q.ID, actor, reason); err != nil && !errors.Is(err, ErrQuestionNotOpen) {
			log.Printf("[autonomous] prd=%s task=%s: withdraw attempt cap question %s: %v", prdID, taskID, q.ID, err)
		}
	}
}

// attemptCapResolution reads an answered attempt cap question as a decision on
// the held task. ok is false for any other change.
func attemptCapResolution(c QuestionChange) (prdID, taskID, option, actor string, ok bool) {
	q := c.Question
	if c.Kind != QuestionChangeAnswered || c.Answer == nil ||
		q.AskedBy != attemptCapQuestionAsker || q.AskerKind != QuestionAskerSystem ||
		q.PRDID == "" || q.TaskID == "" {
		return "", "", "", "", false
	}
	switch c.Answer.ChosenOptionID {
	case attemptCapActionForceReset, reviewActionCancel:
	default:
		return "", "", "", "", false
	}
	actor = c.Answer.Actor
	if actor == "" {
		actor = reviewDecisionActor
	}
	return q.PRDID, q.TaskID, c.Answer.ChosenOptionID, actor, true
}

// resolveAttemptCapAnswer acts on an answered attempt cap question: the
// operator's force-reset answer clears the count and runs the task again; a
// force-reset answer from any other caller clears nothing and is noted on the
// Automaton; a cancel answer cancels the held task. It reports whether the
// change was such an answer.
func (a *API) resolveAttemptCapAnswer(c QuestionChange) bool {
	if a.resolveAttemptCapSeedAnswer(c) {
		return true
	}
	prdID, taskID, option, actor, ok := attemptCapResolution(c)
	if !ok {
		return false
	}
	switch option {
	case reviewActionCancel:
		if _, err := a.ResolveTaskReview(prdID, taskID, ReviewResolution{Action: reviewActionCancel, Note: "answered operator question " + c.Question.ID}, actor); err != nil && !errors.Is(err, ErrTaskNotInReview) {
			log.Printf("[autonomous] prd=%s task=%s: cancel after question %s answered: %v", prdID, taskID, c.Question.ID, err)
		}
	case attemptCapActionForceReset:
		if !isOperatorActor(actor) {
			a.M.noteAttemptCapAnswerIgnored(prdID, taskID, actor, c.Question.ID)
			return true
		}
		if _, err := a.M.ResetTaskWithOptions(prdID, taskID, actor, true, false); err != nil {
			log.Printf("[autonomous] prd=%s task=%s: force reset after question %s answered: %v", prdID, taskID, c.Question.ID, err)
			return true
		}
		_ = a.Run(prdID)
	}
	return true
}

// noteAttemptCapAnswerIgnored records on the Automaton that a force-reset
// answer came from a caller that is not the operator, so the count stays.
func (m *Manager) noteAttemptCapAnswerIgnored(prdID, taskID, actor, questionID string) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return
	}
	m.store.WithLock(func() {
		prd.Decisions = append(prd.Decisions, Decision{
			At: m.attemptNow(), Kind: attemptCapAnswerIgnoredDecisionKind, Actor: actor,
			Note: fmt.Sprintf("task %s: the force-reset answer to question %s came from %q, not the operator; the attempt count is kept", taskID, questionID, actor),
		})
	})
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] prd=%s save attempt cap answer note: %v", prdID, err)
	}
}
