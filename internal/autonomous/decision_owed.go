// Every blocking question a task's worker asks is owed one decision line. A
// task shows one park and one delivery record at a time, so with a second
// question open a decision could be left behind unseen. The task therefore
// keeps what it still owes, from the moment a question is raised until its
// line is sent or the operator is told it needs review:
//
//   - A task is not re-parked while the question it is parked on has a decision
//     that is not typed yet.
//   - A decided question the task is not parked on is typed once the task
//     runs: the coordinator tick parks the task on it and resumes it. While
//     the task is parked on another open question it waits, and the operator
//     is told once.
//   - When its worker can no longer get it, it is listed with a "review
//     needed" alert.
//
// The daemon's own questions about a task (asker kind system, held questions
// among them) have their own handlers and are not tracked here.

package autonomous

import (
	"context"
	"fmt"
	"log"
)

// OwedDecision is one blocking question of a task whose decision line has not
// reached an end state.
type OwedDecision struct {
	QuestionID string `json:"question_id"`
	// SessionID is the one worker session the line is meant for.
	SessionID string `json:"session_id,omitempty"`
	// WaitingNoticed is set once the operator was told the decision waits for
	// another question.
	WaitingNoticed bool `json:"waiting_noticed,omitempty"`
}

// Reason codes of the notice that a decision waits: the task is parked on
// another open question, or an earlier decision line is not typed yet.
const (
	deliveryOtherQuestionOpen = "other_question_open"
	deliveryEarlierUnfinished = "earlier_line_unfinished"
)

// owedQuestion reports whether the question is one a task's worker waits for.
func owedQuestion(q *Question) bool {
	return q.Blocking && q.TaskID != "" && q.AskerKind != QuestionAskerSystem && !isHeldQuestion(q)
}

// hasLiveAttempt reports whether a task in this status can have a worker that
// waits for a decision.
func hasLiveAttempt(s TaskStatus) bool {
	switch s {
	case TaskInProgress, TaskVerifying, TaskRunningTests, TaskWaitingCapacity, TaskWaitingDecision:
		return true
	}
	return false
}

func owedIndex(list []OwedDecision, questionID string) int {
	for i := range list {
		if list[i].QuestionID == questionID {
			return i
		}
	}
	return -1
}

// withoutOwed returns the list without the question, in a new array: task
// snapshots share the old one.
func withoutOwed(list []OwedDecision, questionID string) []OwedDecision {
	i := owedIndex(list, questionID)
	if i < 0 {
		return list
	}
	return append(append([]OwedDecision(nil), list[:i]...), list[i+1:]...)
}

// noteRaisedQuestion is the question store's report of a new question, so one
// decided before any poll or tick saw it open is still owed.
func (m *Manager) noteRaisedQuestion(q Question) {
	if owedQuestion(&q) {
		m.noteOwedDecisions(q.TaskID, "", []Question{q})
	}
}

// noteOwedDecisions adds the task's open questions to what it owes, for the
// worker session it has now. parkedOn is the question the task is, or is
// about to be, parked on. Any other question asked by a different session (a
// verifier, or an earlier attempt's worker) is owed to the session that asked
// and never moved to this one, so its decision is listed, not typed. The same
// holds for a session's question recorded while the task has no live attempt:
// it can arrive after its worker's task moved on.
func (m *Manager) noteOwedDecisions(taskID, parkedOn string, open []Question) {
	if len(open) == 0 {
		return
	}
	if _, err := m.store.mutateTaskSaved(taskID, func(c *Task) bool {
		live := hasLiveAttempt(c.Status) && c.SessionID != ""
		next, changed := c.OwedDecisions, false
		for i := range open {
			q := &open[i]
			askedBySession := q.AskerKind == QuestionAskerSession && q.SessionID != ""
			if q.TaskID != c.ID || !owedQuestion(q) || (!live && !askedBySession) {
				continue
			}
			to, foreign := c.SessionID, !live || (q.ID != parkedOn && q.SessionID != "" && !sameSessionID(q.SessionID, c.SessionID))
			if foreign {
				to = q.SessionID
			}
			at := owedIndex(next, q.ID)
			if at >= 0 && (foreign || next[at].SessionID == to) {
				continue
			}
			next, changed = append([]OwedDecision(nil), next...), true
			if at < 0 {
				next = append(next, OwedDecision{QuestionID: q.ID, SessionID: to})
			} else {
				next[at].SessionID = to
			}
		}
		if changed {
			c.OwedDecisions = next
		}
		return changed
	}); err != nil {
		log.Printf("[autonomous] %s: the questions this task waits for could not be saved: %v", taskID, err)
	}
}

// openBlockingQuestionsForTask returns the task's open blocking questions.
func (m *Manager) openBlockingQuestionsForTask(taskID string) []Question {
	m.mu.Lock()
	store := m.decisionStore
	m.mu.Unlock()
	if store == nil || taskID == "" {
		return nil
	}
	var out []Question
	for _, q := range store.List(QuestionFilter{State: QuestionOpen, Limit: maxQuestionListLimit}) {
		if q.TaskID == taskID && q.Blocking {
			out = append(out, q)
		}
	}
	return out
}

// parkedQuestionID is the question the task is parked on now. A running task
// can still carry the park text of an earlier attempt; that is not a park.
func parkedQuestionID(t Task) string {
	if t.Status != TaskWaitingCapacity && t.Status != TaskWaitingDecision {
		return ""
	}
	return AwaitDecisionQuestionID(t.WaitReason)
}

// parkedDecisionUntyped reports whether the task is parked with a line that
// is still to be typed: the question it is parked on is answered, withdrawn or
// expired with no line recorded beyond waiting, or a recorded resume is still
// waiting for capacity. Parking on another question would drop it.
func (m *Manager) parkedDecisionUntyped(t Task) bool {
	qid := parkedQuestionID(t)
	if qid == "" {
		return false
	}
	dr := t.DecisionResume
	if dr != nil && dr.Delivery == DecisionWaiting {
		return true
	}
	m.mu.Lock()
	store := m.decisionStore
	m.mu.Unlock()
	if store == nil {
		return false
	}
	q, ok := store.Get(qid)
	return ok && (q.State == QuestionAnswered || q.State == QuestionWithdrawn || q.State == QuestionExpired) &&
		(dr == nil || dr.QuestionID != qid)
}

// decidedOwed returns the task's owed questions that are answered or
// withdrawn, oldest first. An expired or unknown one is dropped: it has no
// decision to type.
func (m *Manager) decidedOwed(store *QuestionStore, t Task) (owed []OwedDecision, questions []Question) {
	for _, e := range t.OwedDecisions {
		q, ok := store.Get(e.QuestionID)
		switch {
		case !ok || q.State == QuestionExpired:
			if _, err := m.store.mutateTaskSaved(t.ID, func(c *Task) bool {
				c.OwedDecisions = withoutOwed(c.OwedDecisions, e.QuestionID)
				return true
			}); err != nil {
				log.Printf("[autonomous] %s: owed question %s could not be dropped: %v", t.ID, e.QuestionID, err)
			}
		case q.State == QuestionAnswered || q.State == QuestionWithdrawn:
			owed, questions = append(owed, e), append(questions, q)
		}
	}
	return owed, questions
}

// settleOwedDecisions handles the decided questions a task still owes while it
// has no line in delivery and is not parked on a decided question. It starts
// at most one resume.
func (m *Manager) settleOwedDecisions(ctx context.Context, store *QuestionStore, taskID string) {
	snap, ok := m.store.TaskSnapshot(taskID)
	if !ok || len(snap.OwedDecisions) == 0 {
		return
	}
	owed, questions := m.decidedOwed(store, snap)
	for i, e := range owed {
		if dr := snap.DecisionResume; dr != nil && dr.QuestionID == e.QuestionID && dr.deliveryOpen() {
			continue // its own delivery is under way
		}
		waiting := snap.Status == TaskWaitingCapacity || snap.Status == TaskWaitingDecision
		switch {
		case !hasLiveAttempt(snap.Status):
			m.listOwedDecision(snap.PRDID, snap.ID, e, questions[i], deliveryTaskMovedOn)
		case e.SessionID != snap.SessionID:
			m.listOwedDecision(snap.PRDID, snap.ID, e, questions[i], deliverySessionChanged)
		case waiting && !parkedOnExpired(store, snap):
			// It is typed once the task runs again.
			m.noticeOwedWaiting(store, snap, e)
		case m.workerFinished(snap):
			m.listOwedDecision(snap.PRDID, snap.ID, e, questions[i], deliveryTaskMovedOn)
		default:
			m.resumeOwedDecision(ctx, store, snap, questions[i])
			return
		}
	}
}

// parkedOnExpired reports whether the task is parked on a question that
// expired: no decision will come for it, so another one takes its place.
func parkedOnExpired(store *QuestionStore, t Task) bool {
	qid := AwaitDecisionQuestionID(t.WaitReason)
	if qid == "" {
		return false
	}
	q, ok := store.Get(qid)
	return !ok || q.State == QuestionExpired
}

// workerFinished reports whether the task's worker can no longer take a line:
// its session ended, or, once the task is being verified, a verifier session
// is working on it or the worker's output carries the completion marker.
func (m *Manager) workerFinished(t Task) bool {
	m.mu.Lock()
	aliveFn, outputFn := m.sessionAliveFn, m.sessionOutputFn
	m.mu.Unlock()
	if aliveFn != nil && !aliveFn(t.SessionID) {
		return true
	}
	if t.Status == TaskInProgress {
		return false
	}
	// The task keeps a verifier's session id after that verification ended.
	if t.VerifierSessionID != "" && (aliveFn == nil || aliveFn(t.VerifierSessionID)) {
		return true
	}
	if outputFn == nil {
		return false
	}
	out, ok := outputFn(t.SessionID)
	return ok && outputHasCompletionMarker(out)
}

// resumeOwedDecision parks the running task on a decided question it owes and
// resumes it, the same way as a question it was parked on while open.
func (m *Manager) resumeOwedDecision(ctx context.Context, store *QuestionStore, snap Task, q Question) {
	var ans *Answer
	if q.State == QuestionAnswered {
		cur, ok := store.CurrentAnswer(q.ID)
		if !ok {
			return
		}
		ans = &cur
	}
	if !m.questionTick.begin(snap.ID) {
		return
	}
	prd, okPRD := m.store.GetPRD(snap.PRDID)
	t, okTask := m.store.GetTask(snap.ID)
	m.questionTick.parkMu.Lock()
	if now, ok := m.store.TaskSnapshot(snap.ID); okPRD && okTask && ok && now.Status == snap.Status &&
		now.SessionID == snap.SessionID && now.WaitReason == snap.WaitReason &&
		(now.DecisionResume == nil || !now.DecisionResume.deliveryOpen()) {
		m.parkOnDecision(prd, t, q.ID)
	}
	m.questionTick.parkMu.Unlock()
	m.questionTick.end(snap.ID)
	if parked, ok := m.store.TaskSnapshot(snap.ID); ok && parked.Status == TaskWaitingCapacity &&
		AwaitDecisionQuestionID(parked.WaitReason) == q.ID {
		m.startQuestionResume(ctx, parked, q, ans)
	}
}

// noticeOwedWaiting tells the operator, once, that a decision waits because
// the task is parked on another question that is still open.
func (m *Manager) noticeOwedWaiting(store *QuestionStore, snap Task, e OwedDecision) {
	parkedOn := AwaitDecisionQuestionID(snap.WaitReason)
	if parkedOn == "" || parkedOn == e.QuestionID {
		return
	}
	if q, ok := store.Get(parkedOn); !ok || q.State != QuestionOpen {
		return
	}
	m.noticeOwedOnce(snap, e, deliveryOtherQuestionOpen, fmt.Sprintf(
		"The task also waits for question %s, which is still open. This decision is typed after that one is decided or expires, or listed as 'review needed' if the worker can no longer get it. Answer or withdraw that question.", parkedOn))
}

// noticeOwedBehindLine tells the operator, once for each, which other
// decisions wait behind the task's line that could not be typed.
func (m *Manager) noticeOwedBehindLine(store *QuestionStore, taskID, earlier string) {
	snap, ok := m.store.TaskSnapshot(taskID)
	if !ok {
		return
	}
	owed, _ := m.decidedOwed(store, snap)
	for _, e := range owed {
		if e.QuestionID != earlier {
			m.noticeOwedOnce(snap, e, deliveryEarlierUnfinished, fmt.Sprintf(
				"The line for question %s is not typed yet. This decision is typed after it, or listed as 'review needed' if the worker can no longer get it.", earlier))
		}
	}
}

func (m *Manager) noticeOwedOnce(snap Task, e OwedDecision, reason, why string) {
	if e.WaitingNoticed {
		return
	}
	notice := m.prepareNotice("warn", "Automaton decision not delivered yet", fmt.Sprintf(
		"automaton=%s task=%s question=%s session=%s reason=%s. %s",
		snap.PRDID, snap.ID, e.QuestionID, e.SessionID, reason, why))
	noted, err := m.store.mutateTaskSaved(snap.ID, func(c *Task) bool {
		at := owedIndex(c.OwedDecisions, e.QuestionID)
		if at < 0 || c.OwedDecisions[at].WaitingNoticed {
			return false
		}
		next := append([]OwedDecision(nil), c.OwedDecisions...)
		next[at].WaitingNoticed = true
		c.OwedDecisions = next
		return true
	})
	notice(err == nil && noted)
}

// listOwedDecision gives up on typing an owed decision and lists it for the
// operator, once. A withdrawn question is only dropped: nobody decided
// anything.
func (m *Manager) listOwedDecision(prdID, taskID string, e OwedDecision, q Question, reason string) {
	if !m.questionTick.begin(taskID) {
		return // a resume or a send is running; look again next tick
	}
	defer m.questionTick.end(taskID)
	notice := func(bool) {}
	if q.State != QuestionWithdrawn {
		notice = m.prepareDeliveryReview(prdID, taskID, e.QuestionID, e.SessionID, reason)
	}
	removed, err := m.store.mutateTaskSaved(taskID, func(c *Task) bool {
		if owedIndex(c.OwedDecisions, e.QuestionID) < 0 {
			return false
		}
		c.OwedDecisions = withoutOwed(c.OwedDecisions, e.QuestionID)
		return true
	})
	if err != nil {
		notice(false)
		log.Printf("[autonomous] %s: decision %s: review state could not be saved: %v", taskID, e.QuestionID, err)
		return
	}
	notice(removed)
	if !removed || q.State == QuestionWithdrawn {
		return
	}
	log.Printf("[autonomous] %s: decision %s needs review (%s); it was not typed", taskID, e.QuestionID, reason)
	if prd, ok := m.store.GetPRD(prdID); ok {
		m.emitDecisionResumeAudit(prd, taskID, &DecisionResume{QuestionID: e.QuestionID, Delivery: DecisionReviewNeeded})
	}
}

// listUndeliveredDecisions is the tick's pass over a task that will not run
// again (it ended, or its Automaton is closed): an unfinished delivery and
// every decided question it still owes are listed. Nothing is typed.
func (m *Manager) listUndeliveredDecisions(store *QuestionStore, taskID string) {
	snap, ok := m.store.TaskSnapshot(taskID)
	if !ok {
		return
	}
	if dr := snap.DecisionResume; dr != nil && dr.deliveryOpen() && m.questionTick.begin(taskID) {
		if prd, ok := m.store.GetPRD(snap.PRDID); ok {
			m.deliveryNeedsReview(prd, taskID, dr.QuestionID, deliveryTaskMovedOn)
		}
		m.questionTick.end(taskID)
		snap, ok = m.store.TaskSnapshot(taskID)
		if !ok {
			return
		}
	}
	owed, questions := m.decidedOwed(store, snap)
	for i, e := range owed {
		if dr := snap.DecisionResume; dr != nil && dr.QuestionID == e.QuestionID && dr.deliveryOpen() {
			continue
		}
		m.listOwedDecision(snap.PRDID, taskID, e, questions[i], deliveryTaskMovedOn)
	}
}
