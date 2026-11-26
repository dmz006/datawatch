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
	deliveryReplaced       = "replaced"
	deliveryQuestionGone   = "question_missing"
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
	if s.taskSaveHook != nil {
		s.taskSaveHook(t)
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
		// retry that types nothing must not hide a task that is stuck. At
		// that end the question is no longer owed.
		if !n.deliveryOpen() {
			c.UpdatedAt = time.Now()
			c.OwedDecisions = withoutOwed(c.OwedDecisions, n.QuestionID)
		}
		return true
	})
	return next, ok, err
}

// parkedOnAnotherQuestion reports whether the task is parked on a question
// other than the one its delivery record is for.
func parkedOnAnotherQuestion(tk Task, recorded string) bool {
	if tk.Status != TaskWaitingCapacity {
		return false
	}
	qid := AwaitDecisionQuestionID(tk.WaitReason)
	return qid != "" && qid != recorded
}

// closeReplacedDelivery ends the task's open delivery as review needed before
// a resume for another question takes its place, so a line that will never be
// typed is not dropped silently. It reports whether the resume may go on;
// false means that state could not be saved. The caller holds the task's
// in-flight slot.
func (m *Manager) closeReplacedDelivery(prd *PRD, tk Task, next string) bool {
	old := tk.DecisionResume
	if old == nil || old.QuestionID == next || !old.deliveryOpen() {
		return true
	}
	m.deliveryNeedsReview(prd, tk.ID, old.QuestionID, deliveryReplaced)
	cur, ok := m.store.TaskSnapshot(tk.ID)
	return ok && (cur.DecisionResume == nil || cur.DecisionResume.QuestionID != old.QuestionID || !cur.DecisionResume.deliveryOpen())
}

// takeOverUnfinishedLine is asked before the task is parked on next while a
// decision line is not finished (waiting, sending or not typed). The line
// keeps the task when next was already open when it was recorded, while it is
// being sent, and while any resume or send of the task is at work. Otherwise
// next was asked later and takes over: the old line is listed first. It
// reports whether the park may go on.
func (m *Manager) takeOverUnfinishedLine(prd *PRD, cur Task, next string) bool {
	dr := cur.DecisionResume
	if dr == nil || !dr.deliveryOpen() {
		return true
	}
	if next == dr.QuestionID || dr.Delivery == DecisionSending {
		return false
	}
	for _, id := range dr.OpenAtResume {
		if id == next {
			return false
		}
	}
	return m.listUnfinishedLine(prd, cur.ID, dr.QuestionID, deliveryReplaced)
}

// listUnfinishedLine ends the task's open delivery for the question as review
// needed, unless a resume or send of the task is at work. It reports whether
// that delivery is no longer open.
func (m *Manager) listUnfinishedLine(prd *PRD, taskID, questionID, reason string) bool {
	if !m.questionTick.begin(taskID) {
		return false
	}
	defer m.questionTick.end(taskID)
	m.deliveryNeedsReview(prd, taskID, questionID, reason)
	cur, ok := m.store.TaskSnapshot(taskID)
	return ok && (cur.DecisionResume == nil || cur.DecisionResume.QuestionID != questionID || !cur.DecisionResume.deliveryOpen())
}

// otherOpenQuestionIDs lists the task's open blocking questions other than
// the one being resumed.
func (m *Manager) otherOpenQuestionIDs(taskID, resumed string) []string {
	var ids []string
	for _, q := range m.openBlockingQuestionsForTask(taskID) {
		if q.ID != resumed {
			ids = append(ids, q.ID)
		}
	}
	return ids
}

// continueDelivery picks up an unfinished delivery on a tick. It starts at
// most one goroutine per task and does nothing while one is running. The
// question is read by id: it need not be among the newest decided ones.
func (m *Manager) continueDelivery(ctx context.Context, store *QuestionStore, tk Task) {
	dr := tk.DecisionResume
	var ans *Answer
	q, ok := store.Get(dr.QuestionID)
	if ok && q.State == QuestionAnswered {
		cur, found := store.CurrentAnswer(dr.QuestionID)
		if found {
			ans = &cur
		}
		ok = found
	}
	if !ok {
		// There is no decision left to type: say so instead of waiting.
		if prd, found := m.store.GetPRD(tk.PRDID); found {
			m.listUnfinishedLine(prd, tk.ID, dr.QuestionID, deliveryQuestionGone)
		}
		return
	}
	if q.State == QuestionOpen {
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
	// The notice of a failed send is saved before the result is.
	notice := func(bool) {}
	switch {
	case refused && first:
		notice = m.prepareNotice("warn", "Automaton decision not delivered yet", fmt.Sprintf(
			"automaton=%s task=%s question=%s session=%s reason=%s. Nothing was typed because the session shows a prompt or its screen cannot be read. datawatch tries again while the task stays with this worker session and no later question takes over; retries pause while the Automaton is held or the autonomous loop is off. If it stops trying, you get a 'review needed' alert for this question.",
			prd.ID, taskID, q.ID, sessionID, deliveryPromptOnScreen))
	case sendErr != nil && !refused:
		notice = m.prepareDeliveryReview(prd.ID, taskID, q.ID, sessionID, deliverySendError)
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
		notice(false)
		log.Printf("[autonomous] %s: decision %s: the send result could not be saved: %v", taskID, q.ID, err)
		return
	}
	notice(changed)
	if !changed || refused {
		return
	}
	m.emitDecisionResumeAudit(prd, taskID, done)
	recordWorkerQuestion(WorkerQuestionOutcomeResumed)
	log.Printf("[autonomous] %s: resumed after decision %s (answer %q, withdrawn=%v, delivery=%s); no retry counted",
		taskID, q.ID, done.AnswerID, done.Withdrawn, done.Delivery)
}

// deliveryNeedsReview ends an open delivery as one the operator must look at.
func (m *Manager) deliveryNeedsReview(prd *PRD, taskID, questionID, reason string) {
	notice := func(bool) {}
	if snap, ok := m.store.TaskSnapshot(taskID); ok {
		if dr := snap.DecisionResume; dr != nil && dr.QuestionID == questionID && dr.deliveryOpen() {
			notice = m.prepareDeliveryReview(prd.ID, taskID, questionID, dr.DeliverySessionID, reason)
		}
	}
	done, changed, err := m.setDelivery(taskID, questionID, func(n *DecisionResume) {
		n.Delivery, n.DeliveryReason = DecisionReviewNeeded, reason
	})
	if err != nil {
		notice(false)
		log.Printf("[autonomous] %s: decision %s: review state could not be saved: %v", taskID, questionID, err)
		return
	}
	notice(changed)
	if !changed {
		return
	}
	log.Printf("[autonomous] %s: decision %s needs review (%s); it will not be typed again", taskID, questionID, reason)
	m.emitDecisionResumeAudit(prd, taskID, done)
}

// prepareDeliveryReview saves the review notice of one decision
// (prepareNotice).
func (m *Manager) prepareDeliveryReview(prdID, taskID, questionID, sessionID, reason string) func(bool) {
	return m.prepareNotice("error", "Automaton decision: review needed", fmt.Sprintf(
		"review needed: automaton=%s task=%s question=%s session=%s reason=%s. datawatch cannot tell that this worker got the decision, or must not type it now, and will not type it again. Check the session and send the decision yourself if it is missing.",
		prdID, taskID, questionID, sessionID, reason))
}
