package autonomous

// Delivery of the decision line to a resumed worker session. A task is only
// recorded as having its decision once the send worked.
//
//   - The state is saved before any keystroke; with no save nothing is typed.
//   - A send refused before typing (a prompt on screen) is tried again on each
//     coordinator tick, into the same worker session only.
//   - Any other failure, or a send the daemon did not see finish, cannot be
//     told apart from a delivered line. It is never typed again: the task says
//     review_needed and the operator gets an alert.
//
// Alerts, logs and audit records carry ids, the state and a reason code, never
// the answer's text.

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// Delivery states of the decision line (DecisionResume.Delivery). Empty means
// the record says nothing about delivery: it predates delivery tracking, or no
// prompt hook is wired. Such a record is never acted on.
const (
	// DecisionWaiting is a recorded resume for which nothing was typed yet.
	DecisionWaiting = "waiting"
	// DecisionSending is saved just before the keystrokes.
	DecisionSending = "sending"
	// DecisionSent means the send returned no error.
	DecisionSent = "sent"
	// DecisionNotTyped was refused before typing and is tried again.
	DecisionNotTyped = "not_typed"
	// DecisionReviewNeeded may or may not have arrived, or must no longer be
	// typed. The operator decides; datawatch never types it again.
	DecisionReviewNeeded = "review_needed"
)

// Reason codes (DecisionResume.DeliveryReason).
const (
	deliveryPromptOnScreen = "prompt_on_screen"
	deliverySendError      = "send_error"
	deliveryInterrupted    = "interrupted"
	deliveryNoSession      = "no_session"
	deliverySessionChanged = "session_changed"
	deliveryTaskMovedOn    = "task_moved_on"
)

// open marks a new resume record as not yet typed, for the worker session the
// task has now.
func (d *DecisionResume) open(sessionID string) {
	d.Delivery, d.DeliverySessionID = DecisionWaiting, sessionID
}

// deliveryOpen reports whether the line's delivery is still unfinished.
func (d *DecisionResume) deliveryOpen() bool {
	switch d.Delivery {
	case DecisionWaiting, DecisionSending, DecisionNotTyped:
		return true
	}
	return false
}

// mutateTaskSaved is MutateTask for a change that must be on disk before the
// caller acts on it: f reports whether it changed the task, and when the save
// fails the change is undone and the error returned. It leaves the task's
// updated time to f.
func (s *Store) mutateTaskSaved(id string, f func(*Task) bool) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return false, nil
	}
	before := *t
	if !f(t) {
		return false, nil
	}
	if err := s.persistPRD(t.PRDID); err != nil {
		*t = before
		return false, err
	}
	return true, nil
}

// setDelivery moves the task's open delivery for the question to a new state
// and returns the new record. The record is replaced, not changed in place,
// because task snapshots share it.
func (m *Manager) setDelivery(taskID, questionID string, change func(*DecisionResume)) (*DecisionResume, bool, error) {
	var next *DecisionResume
	ok, err := m.store.mutateTaskSaved(taskID, func(c *Task) bool {
		cur := c.DecisionResume
		if cur == nil || cur.QuestionID != questionID || !cur.deliveryOpen() {
			return false
		}
		n := *cur
		change(&n)
		c.DecisionResume, next = &n, &n
		// Only an end to the delivery counts as activity on the task: a
		// retry that types nothing must not hide a task that is stuck.
		if !n.deliveryOpen() {
			c.UpdatedAt = time.Now()
		}
		return true
	})
	return next, ok, err
}

// continueDelivery picks up an unfinished delivery on a tick. It starts at
// most one goroutine per task and does nothing while one is running.
func (m *Manager) continueDelivery(ctx context.Context, store *QuestionStore, tk Task, answered, withdrawn map[string]Question) {
	dr := tk.DecisionResume
	var ans *Answer
	q, ok := answered[dr.QuestionID]
	if ok {
		cur, found := store.CurrentAnswer(dr.QuestionID)
		if !found {
			return
		}
		ans = &cur
	} else if q, ok = withdrawn[dr.QuestionID]; !ok {
		return
	}
	// The daemon stopped while the task was still queued to resume: run the
	// whole resume again.
	if dr.Delivery == DecisionWaiting && tk.Status == TaskWaitingCapacity &&
		AwaitDecisionQuestionID(tk.WaitReason) == dr.QuestionID {
		m.startQuestionResume(ctx, tk, q, ans)
		return
	}
	if !m.questionTick.begin(tk.ID) {
		return
	}
	go func() {
		defer m.questionTick.end(tk.ID)
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[autonomous] %s: decision delivery panicked: %v", tk.ID, r)
			}
		}()
		if ctx.Err() != nil {
			return
		}
		m.sendDecisionLine(tk.PRDID, tk.ID, q, ans)
	}()
}

// sendDecisionLine types the decision line for the task's recorded resume, at
// most once. The caller holds the task's in-flight slot, so a record found in
// the sending state belongs to a send that never reported back.
func (m *Manager) sendDecisionLine(prdID, taskID string, q Question, ans *Answer) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return
	}
	snap, ok := m.store.TaskSnapshot(taskID)
	if !ok || isTaskTerminal(snap.Status) {
		return
	}
	rec := snap.DecisionResume
	if rec == nil || rec.QuestionID != q.ID || !rec.deliveryOpen() {
		return
	}
	switch {
	case rec.Delivery == DecisionSending:
		m.deliveryNeedsReview(prd, taskID, q.ID, deliveryInterrupted)
		return
	case snap.Status == TaskWaitingCapacity:
		return // queued again; try once it runs
	case snap.Status != TaskInProgress:
		m.deliveryNeedsReview(prd, taskID, q.ID, deliveryTaskMovedOn)
		return
	case rec.DeliverySessionID == "" && snap.SessionID == "":
		m.deliveryNeedsReview(prd, taskID, q.ID, deliveryNoSession)
		return
	case snap.SessionID != rec.DeliverySessionID:
		m.deliveryNeedsReview(prd, taskID, q.ID, deliverySessionChanged)
		return
	}
	fn := m.sessionContinueLineFnLocked()
	if fn == nil {
		return
	}
	first := rec.Delivery == DecisionWaiting
	sessionID := rec.DeliverySessionID
	_, claimed, err := m.setDelivery(taskID, q.ID, func(n *DecisionResume) {
		n.Delivery, n.DeliveryReason = DecisionSending, ""
		n.Withdrawn, n.AnswerID = ans == nil, ""
		if ans != nil {
			n.AnswerID, n.CurrentAnswerID = ans.ID, ans.ID
		}
	})
	if err != nil {
		log.Printf("[autonomous] %s: decision %s not typed: the task could not be saved: %v", taskID, q.ID, err)
		return
	}
	if !claimed {
		return
	}

	sendErr := fn(sessionID, BuildDecisionResumeLine(q, ans))
	refused := session.RefusedBeforeTyping(sendErr)
	if sendErr != nil && (first || !refused) {
		log.Printf("[autonomous] %s: decision resume prompt to session %s failed: %v", taskID, sessionID, sendErr)
	}
	done, changed, err := m.setDelivery(taskID, q.ID, func(n *DecisionResume) {
		switch {
		case sendErr == nil:
			now := time.Now().UTC()
			n.Delivery, n.DeliveredAt = DecisionSent, &now
		case refused:
			n.Delivery, n.DeliveryReason = DecisionNotTyped, deliveryPromptOnScreen
		default:
			n.Delivery, n.DeliveryReason = DecisionReviewNeeded, deliverySendError
		}
	})
	if err != nil {
		// The saved state stays "sending", which a later tick reads as unsure.
		log.Printf("[autonomous] %s: decision %s: the send result could not be saved: %v", taskID, q.ID, err)
		return
	}
	if !changed {
		return
	}
	if refused {
		if first {
			m.deliveryAlert("warn", "Automaton decision not delivered yet", fmt.Sprintf(
				"automaton=%s task=%s question=%s session=%s reason=%s. Nothing was typed because the session shows a prompt or its screen cannot be read. datawatch tries again until it can type.",
				prd.ID, taskID, q.ID, sessionID, deliveryPromptOnScreen))
		}
		return
	}
	if done.Delivery == DecisionReviewNeeded {
		m.alertDeliveryReview(prd.ID, taskID, q.ID, sessionID, deliverySendError)
	}
	m.emitDecisionResumeAudit(prd, taskID, done)
	recordWorkerQuestion(WorkerQuestionOutcomeResumed)
	log.Printf("[autonomous] %s: resumed after decision %s (answer %q, withdrawn=%v, delivery=%s); no retry counted",
		taskID, q.ID, done.AnswerID, done.Withdrawn, done.Delivery)
}

// deliveryNeedsReview ends an open delivery as one the operator must look at.
func (m *Manager) deliveryNeedsReview(prd *PRD, taskID, questionID, reason string) {
	done, changed, err := m.setDelivery(taskID, questionID, func(n *DecisionResume) {
		n.Delivery, n.DeliveryReason = DecisionReviewNeeded, reason
	})
	if err != nil {
		log.Printf("[autonomous] %s: decision %s: review state could not be saved: %v", taskID, questionID, err)
		return
	}
	if !changed {
		return
	}
	log.Printf("[autonomous] %s: decision %s needs review (%s); it will not be typed again", taskID, questionID, reason)
	m.alertDeliveryReview(prd.ID, taskID, questionID, done.DeliverySessionID, reason)
	m.emitDecisionResumeAudit(prd, taskID, done)
}

func (m *Manager) alertDeliveryReview(prdID, taskID, questionID, sessionID, reason string) {
	m.deliveryAlert("error", "Automaton decision: review needed", fmt.Sprintf(
		"review needed: automaton=%s task=%s question=%s session=%s reason=%s. datawatch cannot tell that this worker got the decision, or must not type it now, and will not type it again. Check the session and send the decision yourself if it is missing.",
		prdID, taskID, questionID, sessionID, reason))
}

func (m *Manager) deliveryAlert(level, title, body string) {
	if sink := m.AlertSink(); sink != nil {
		sink(level, title, body)
	}
}
