// held_question_answer.go — the answer arm of the held-question pair.
//
// held_question_pass.go RAISES a daemon question when a PRD (automaton) or
// task, or a pending member in an Automaton, sits in a held state, and
// WITHDRAWS it once that state clears. This file is what happens in between,
// on the other side of the question: the operator answers that raised
// question, and its answer is routed back through the Manager's existing
// action paths. QuestionTick (question_tick.go) branches on the question's
// source and calls here instead of resuming a worker session — a held
// question is about the automation, not about a running task, so there is no
// session to continue.
//
// The mapping (the options each held state offers, held_question_table.go):
//
//	blocked             resume        -> Manager.ResumeParked (a boot-parked
//	                                        automaton; re-admits it)
//	                        retry / change the spec / cancel
//	                                    -> Manager.ResolveTaskReview on the task
//	                                       held for review (the block is that
//	                                       review hold)
//	revisions asked     resume        -> Manager.Approve (the operator's review
//	                                    is done)
//	                        change the spec -> Manager.RequestRevision with the
//	                                        operator's spec
//	review held         retry / retry stronger / change the spec / cancel
//	                                    -> Manager.ResolveTaskReview, retry
//	                                        stronger raising the resolved effort
//	                                        one step
//	awaiting decision   resume        -> releases the task's park (the decision
//	                                    hold is released, or the parked worker
//	                                    task is re-admitted and its session
//	                                    prompted to continue with the decision),
//	                                    same guard as a worker-question resume
//	                        cancel        -> Manager.CancelTask
//	pending insert      approve insert -> Manager.ApproveInsertedMember (operator)
//	                        reject insert -> record only: there is no daemon path
//	                                        for dropping a pending member, so it
//	                                        stays pending
//
// hold is the option for every one of them, and it means "record the answer,
// leave the state exactly as the operator left it" — nothing is run.
//
// Every routed answer is recorded exactly once: an operator_answer entry on
// the PRD's decisions (linked to the question by id and to the answer by id,
// which the decision log already knows how to join) plus an audit event. The
// entry doubles as the idempotence marker — a second pass over the same
// answer sees it and does not re-run the action. An answer to a state that
// has already moved is refused, named, recorded with its own entry, and
// leaves everything as it found it.
//
// This file never raises or withdraws a question: the pass owns the question
// lifecycle and withdraws the open held question the moment the held state
// clears, which after an applied answer is how the outcome reaches the
// operator's timeline.

package autonomous

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// heldAnswerActor names who the recorded answer is: the option the operator
// chose. The daemon only routes it.
const heldAnswerActor = "operator"

// heldAnswerDecisionKind is the PRD decision entry that marks one answer
// processed. It is linked to the daemon question and to the answer by id,
// and it is also the idempotence marker for ProcessHeldAnswer.
const heldAnswerDecisionKind = "operator_answer"

// heldAnswerAudit is the audit action for every answer the processor handles,
// whatever its outcome.
const heldAnswerAuditAction = "held_question_answered"

// heldAnswerNoteMax bounds the recorded note.
const heldAnswerNoteMax = 300

// Routing outcomes, for the audit event and the recorded note.
const (
	heldResultApplied  = "applied"
	heldResultRecorded = "recorded" // the hold option: nothing ran, state kept
	heldResultRefused  = "refused"  // the state had cleared, or the path refused
)

// isHeldQuestion reports whether q is a question the held-question pass
// raised: the pass writes the session tag, and it names its source.
func isHeldQuestion(q *Question) bool {
	return q != nil && (q.SessionID == heldQuestionSessionID || q.Source == QuestionSourceHeld)
}

// heldQuestionIdentity recovers the held item and the held state a raised
// question was for, straight off the question's dedupe key — the same
// reconstruction the pass uses when it re-renders options after a
// re-render. A question that does not carry one of the five
// held:<key>:<state> hashes is not a held question.
func heldQuestionIdentity(q *Question) (key, state string, ok bool) {
	if q == nil || q.SessionID != heldQuestionSessionID || q.SourceLineSHA256 == "" {
		return "", "", false
	}
	key, ok = heldQuestionKeyFromQuestion(q)
	if !ok {
		return "", "", false
	}
	for _, st := range HeldStates {
		if heldQuestionLineHash("held:"+key+":"+string(st)) == q.SourceLineSHA256 {
			return key, string(st), true
		}
	}
	return "", "", false
}

// heldAnswerNote builds the free part of a manager call's note from the
// answer itself.
func heldAnswerNote(ans *Answer) string {
	note := "via held question " + ans.QuestionID
	if qt := strings.TrimSpace(ans.Quote); qt != "" {
		note += ": " + qt
	}
	if len(note) > 200 {
		note = note[:200]
	}
	return note
}

// ProcessHeldAnswer handles the operator's answer to a raised held question:
// it maps the chosen option (or the free text, which carries no option and
// is recorded without a state change) to the automation or task,
// re-verifies the held state still holds, applies the manager path named in
// the file header above, and records the answer once on the PRD's decisions
// and in the audit log.
//
// It is safe to call repeatedly for the same (question, answer): the second
// call is recognized through the recorded entry and does not re-run the
// action. It returns an error only when the question is not a raised held
// question at all — a routing mistake upstream, not a routing outcome; every
// applied or refused outcome is recorded, not returned.
func (m *Manager) ProcessHeldAnswer(ctx context.Context, q Question, ans Answer) error {
	key, state, ok := heldQuestionIdentity(&q)
	if !ok {
		return fmt.Errorf("question %s is not a raised held question", q.ID)
	}
	prdID := q.PRDID
	if prdID != "" {
		if p, ok := m.store.GetPRD(prdID); ok && heldAnswerAlreadyRecorded(p, q.ID, ans.ID) {
			// Already routed once: the first call recorded it, so the action
			// stands and nothing new is written. The tick re-asks every cycle
			// while the question is still answered, so this path must be
			// quiet — pending inserts included (their question names the
			// member-PRD, so the entry lands on that PRD's decisions).
			return nil
		}
	}
	result, detail := m.routeHeldAnswer(ctx, q, &ans, key, state)
	m.recordHeldAnswer(prdID, key, state, q, &ans, result, detail)
	return nil
}

// heldAnswerAlreadyRecorded reports whether prd already carries the
// operator_answer entry for this (question, answer). It is the idempotence
// marker; see the package documentation.
func heldAnswerAlreadyRecorded(prd *PRD, questionID, answerID string) bool {
	for i := range prd.Decisions {
		d := &prd.Decisions[i]
		if d.Kind == heldAnswerDecisionKind && d.QuestionID == questionID && d.AnswerID == answerID {
			return true
		}
	}
	return false
}

// routeHeldAnswer dispatches the answer to the held state it was raised
// for. Every branch returns an outcome and a one-line detail; none of them
// records — that is the caller's single job, applied and refused alike.
func (m *Manager) routeHeldAnswer(ctx context.Context, q Question, ans *Answer, key, state string) (result, detail string) {
	if ans.ChosenOptionID == "" {
		// The answer carried no option — free text, or a surface that does not
		// offer options. It is recorded with its text and nothing is run.
		return heldResultRecorded, "the answer carries no option (free text); the held state is left as it stands"
	}
	switch HeldState(state) {
	case HeldBlocked:
		return m.routeHeldBlocked(q, ans)
	case HeldRevisionRequested:
		return m.routeHeldRevisions(q, ans)
	case HeldReviewHeld:
		if key == "" {
			return heldResultRefused, "no task can be recovered from this held question"
		}
		return m.routeHeldReview(q, ans, key)
	case HeldAwaitingDecision:
		if key == "" {
			return heldResultRefused, "no task can be recovered from this held question"
		}
		return m.routeHeldAwaiting(ctx, q, ans, key)
	case HeldPendingInsert:
		if key == "" {
			return heldResultRefused, "no named member can be recovered from this held question"
		}
		return m.routeHeldPendingInsert(q, ans, key)
	default:
		return heldResultRefused, "no answer path for held state " + state
	}
}

// routeHeldBlocked handles a blocked automaton. A block by a review-hold
// resolves through the review paths on the held task (retry, the revised
// spec, cancel); a boot-park resolves through the boot-resume path. A block
// for any other reason has no daemon resume path: the operator's choice is
// recorded and named, and the state is left as it stands.
func (m *Manager) routeHeldBlocked(q Question, ans *Answer) (result, detail string) {
	if ans.ChosenOptionID == HeldOptionHold {
		return heldResultRecorded, "the automaton stays blocked"
	}
	prdID := q.PRDID
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return heldResultRefused, "the automaton " + prdID + " is not in the store"
	}
	if prd.Status != PRDBlocked {
		return heldResultRefused, "the state cleared: the automaton is " + string(prd.Status) + " now, blocked no more"
	}
	held := m.reviewHeldTaskID(prd)
	switch ans.ChosenOptionID {
	case HeldOptionResume:
		if held != "" {
			return heldResultRefused,
				"the block is a review-hold on task " + held + "; resume it with retry, a revised spec, or cancel"
		}
		if !isBootParked(prd) {
			reason := strings.TrimSpace(prd.BlockedReason)
			if reason == "" {
				reason = "no recorded reason"
			}
			return heldResultRefused,
				"the block is neither a review-hold nor a boot-park (" + reason + "); there is no daemon resume path"
		}
		if _, err := m.ResumeParked(prdID, heldAnswerActor, heldAnswerNote(ans)); err != nil {
			return heldResultRefused, "resume refused: " + err.Error()
		}
		return heldResultApplied, "the automaton was resumed from the boot-park"
	case HeldOptionRetry, HeldOptionChangeSpec, HeldOptionCancel:
		if held == "" {
			return heldResultRefused, "no task is held in review; the automaton is blocked for another reason"
		}
		return m.routeHeldReview(q, ans, held)
	default:
		return heldResultRefused, "option " + ans.ChosenOptionID + " has no path for a blocked automaton"
	}
}

// routeHeldReview handles a task held for review — the state a task enters
// when the review hold fires — through the review paths. retry stronger
// passes the operator's retry plus the resolved effort bumped one step, so
// the retry path applies both without a separate stronger-retry primitive.
func (m *Manager) routeHeldReview(q Question, ans *Answer, taskID string) (result, detail string) {
	if ans.ChosenOptionID == HeldOptionHold {
		return heldResultRecorded, "the task stays held for review"
	}
	t, ok := m.store.GetTask(taskID)
	if !ok {
		return heldResultRefused, "task " + taskID + " is not in the store"
	}
	if t.Status != TaskReviewNeeded {
		return heldResultRefused, "the state cleared: task " + taskID + " is " + string(t.Status) + " now, review-hold no more"
	}
	opt := ans.ChosenOptionID
	if opt == HeldOptionCancel {
		note := "cancelled via held question " + q.ID
		if qt := strings.TrimSpace(ans.Quote); qt != "" {
			note += ": " + qt
		}
		if _, err := m.ResolveTaskReview(q.PRDID, taskID, ReviewResolution{Action: reviewActionCancel, Note: note}, heldAnswerActor); err != nil {
			return heldResultRefused, "the review path refused: " + err.Error()
		}
		return heldResultApplied, "task " + taskID + " was cancelled after the operator's review"
	}

	p, ok := m.store.GetPRD(q.PRDID)
	if !ok {
		return heldResultRefused, "the automaton " + q.PRDID + " is not in the store"
	}

	res := ReviewResolution{Action: reviewActionRetry, Note: heldAnswerNote(ans)}
	raisedEffort := ""
	switch opt {
	case HeldOptionRetry:
	case HeldOptionRetryStronger:
		cur := resolveTaskEffort(t, findStory(p, t.StoryID), p, m.AutomationForPRD(p))
		next, stronger := heldStrongerEffort(cur)
		if !stronger {
			res.Note += "; the task's effort is already the strongest recognized"
		} else {
			res.Effort = next
			raisedEffort = string(next)
		}
	case HeldOptionChangeSpec:
		if strings.TrimSpace(ans.Quote) == "" {
			return heldResultRefused, "a revised spec was chosen but the answer text carried none"
		}
		res.Spec = ans.Quote
	default:
		return heldResultRefused, "option " + opt + " has no path for a task held in review"
	}
	if _, err := m.ResolveTaskReview(q.PRDID, taskID, res, heldAnswerActor); err != nil {
		return heldResultRefused, "the review path refused: " + err.Error()
	}
	switch opt {
	case HeldOptionChangeSpec:
		return heldResultApplied, "task " + taskID + " was requeued with the operator's revised spec"
	case HeldOptionRetryStronger:
		if raisedEffort != "" {
			return heldResultApplied, fmt.Sprintf("task %s was requeued with the effort raised to %s", taskID, raisedEffort)
		}
		return heldResultApplied, "task " + taskID + " was requeued; no stronger effort exists so it is a plain retry"
	default:
		return heldResultApplied, "task " + taskID + " was requeued for a retry"
	}
}

// heldEffortRung is the recognized effort rungs in roughly ascending
// strength. The aliases (quick, normal, thorough) are the same session
// ladder the executor resolves, so one rung up is a real strength step.
var heldEffortRung = []Effort{"", EffortQuick, EffortLow, EffortNormal, EffortMedium, EffortHigh, EffortThorough, EffortMax}

// heldStrongerEffort returns the rung one step above cur. It reports
// false when cur is already the strongest recognized rung, and treats an
// unrecognized or empty value as the floor (so one step up is a meaningful
// jump, not a sideways one).
func heldStrongerEffort(cur Effort) (Effort, bool) {
	for i, e := range heldEffortRung {
		if e == cur {
			if i+1 < len(heldEffortRung) {
				return heldEffortRung[i+1], true
			}
			return "", false
		}
	}
	return EffortHigh, true
}

// routeHeldRevisions handles a PRD waiting on its revision. Resume is the
// operator's approval — the review is done — and the revised spec is a
// request-revision with the operator's words as the note.
func (m *Manager) routeHeldRevisions(q Question, ans *Answer) (result, detail string) {
	if ans.ChosenOptionID == HeldOptionHold {
		return heldResultRecorded, "the automaton stays waiting for its revision"
	}
	prdID := q.PRDID
	p, ok := m.store.GetPRD(prdID)
	if !ok {
		return heldResultRefused, "the automaton " + prdID + " is not in the store"
	}
	if p.Status != PRDRevisionsAsked {
		return heldResultRefused, "the state cleared: the automaton is " + string(p.Status) + " now, waiting no more"
	}
	switch ans.ChosenOptionID {
	case HeldOptionResume:
		if _, err := m.Approve(prdID, heldAnswerActor, heldAnswerNote(ans)); err != nil {
			return heldResultRefused, "approve refused: " + err.Error()
		}
		return heldResultApplied, "the automaton was approved"
	case HeldOptionChangeSpec:
		if strings.TrimSpace(ans.Quote) == "" {
			return heldResultRefused, "a revised spec was chosen but the answer text carried none"
		}
		note := "operator's spec (held question " + q.ID + "): " + ans.Quote
		if _, err := m.RequestRevision(prdID, heldAnswerActor, note); err != nil {
			return heldResultRefused, "request-revision refused: " + err.Error()
		}
		return heldResultApplied, "a revision was requested with the operator's spec"
	default:
		return heldResultRefused, "option " + ans.ChosenOptionID + " has no path for a PRD waiting on its revision"
	}
}

// routeHeldAwaiting handles a task parked waiting on its decision. Resume
// releases the park — a gate-hold through the release path, a worker-park
// through the re-admit-and-prompt path with the same guard as a
// worker-question resume. Cancel routes through the task cancel path.
func (m *Manager) routeHeldAwaiting(ctx context.Context, q Question, ans *Answer, taskID string) (result, detail string) {
	if ans.ChosenOptionID == HeldOptionHold {
		return heldResultRecorded, "the task stays parked waiting on its decision"
	}
	t, ok := m.store.GetTask(taskID)
	if !ok {
		return heldResultRefused, "task " + taskID + " is not in the store"
	}
	parked := t.Status == TaskWaitingDecision || (t.Status == TaskWaitingCapacity && IsAwaitDecisionWaitReason(t.WaitReason))
	switch ans.ChosenOptionID {
	case HeldOptionResume:
		if !parked {
			return heldResultRefused, "the state cleared: task " + taskID + " is " + string(t.Status) + " now, parked no more"
		}
		p, ok := m.store.GetPRD(q.PRDID)
		if !ok {
			return heldResultRefused, "the automaton " + q.PRDID + " is not in the store"
		}
		if t.Status == TaskWaitingDecision {
			m.releaseDecisionHold(p, t)
			return heldResultApplied,
				"the decision-hold was released and the task requeued; if the decision it names is still open it will be re-held"
		}
		if err := m.resumeHeldAwaitingTask(ctx, p, t, q, ans); err != nil {
			return heldResultRefused, "the park could not be released: " + err.Error()
		}
		return heldResultApplied, "the park was released and the task re-admitted; its session was told to continue with the decision"
	case HeldOptionCancel:
		reason := "cancelled via held question " + q.ID
		if qt := strings.TrimSpace(ans.Quote); qt != "" {
			reason += ": " + qt
		}
		if _, err := m.CancelTask(q.PRDID, taskID, heldAnswerActor, reason); err != nil {
			return heldResultRefused, "cancel refused: " + err.Error()
		}
		return heldResultApplied, "task " + taskID + " was cancelled after the operator's review"
	default:
		return heldResultRefused, "option " + ans.ChosenOptionID + " has no path for a task parked on a decision"
	}
}

// resumeHeldAwaitingTask re-admits a task parked under a worker decision
// after the operator answered the daemon's held question about the park:
// it records the resume under the same guard as a worker-question resume,
// re-admits the task, re-claims a tree lock the park released, and prompts
// the session to continue with the decision. A failed re-admit rolls the
// record back so a later pass can try again.
func (m *Manager) resumeHeldAwaitingTask(ctx context.Context, p *PRD, t *Task, q Question, ans *Answer) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	rec := &DecisionResume{QuestionID: q.ID, AnswerID: ans.ID, CurrentAnswerID: ans.ID, ResumedAt: time.Now().UTC()}
	var claimed bool
	var prev *DecisionResume
	m.store.MutateTask(t.ID, func(c *Task) {
		if c.Status != TaskWaitingCapacity || !IsAwaitDecisionWaitReason(c.WaitReason) {
			return
		}
		if c.DecisionResume != nil && c.DecisionResume.QuestionID == q.ID {
			return
		}
		prev = c.DecisionResume
		c.DecisionResume = rec
		claimed = true
	})
	if !claimed {
		return fmt.Errorf("task %s is no longer parked under a decision", t.ID)
	}
	box := m.treeLockBoxLocked(t.ID)
	releasedTree := box != nil && *box == nil
	prefix := AwaitDecisionWaitReasonFor(AwaitDecisionQuestionID(t.WaitReason))
	if prefix == "" {
		prefix = AwaitDecisionWaitReasonFor(q.ID)
	}
	if err := m.awaitReAdmitAs(ctx, p, t, prefix, t.ComputeNode); err != nil {
		if !m.taskCancelled(t.ID) {
			m.store.MutateTask(t.ID, func(c *Task) {
				if c.DecisionResume == rec {
					c.DecisionResume = prev
				}
			})
		}
		return err
	}
	m.clearWaiting(t)
	if m.taskCancelled(t.ID) {
		return nil
	}
	m.reclaimTreeLock(p, t, releasedTree, box)
	sessionID := t.SessionID
	if cur, ok := m.store.TaskSnapshot(t.ID); ok && cur.SessionID != "" {
		sessionID = cur.SessionID
	}
	if fn := m.sessionContinueLineFnLocked(); fn != nil && sessionID != "" {
		if err := fn(sessionID, BuildDecisionResumeLine(q, ans)); err != nil {
			log.Printf("[autonomous] %s: held-decision resume prompt to session %s failed: %v", t.ID, sessionID, err)
		}
	}
	m.emitDecisionResumeAudit(p, t.ID, rec)
	return nil
}

// routeHeldPendingInsert handles a pending member insert. Approve goes
// through the operator-only insert path. Reject-insert and hold are
// recorded only: there is no daemon path for dropping a pending member, so
// the insert stays pending either way and the note says so.
func (m *Manager) routeHeldPendingInsert(q Question, ans *Answer, pendingID string) (result, detail string) {
	auto, pi := m.findHeldPendingInsert(pendingID)
	if auto == nil || pi == nil {
		return heldResultRefused, "insert " + pendingID + " is no longer pending; the state cleared"
	}
	switch ans.ChosenOptionID {
	case HeldOptionApproveInsert:
		if _, err := m.ApproveInsertedMember(auto.ID, pendingID, heldAnswerActor, auto.Version); err != nil {
			return heldResultRefused, "approve-refused: " + err.Error()
		}
		return heldResultApplied, "insert " + pendingID + " on automaton " + auto.ID + " was approved and merged in"
	case HeldOptionRejectInsert:
		return heldResultRecorded, "the insert was not approved: there is no daemon path to drop a pending member, so it stays pending"
	case HeldOptionHold:
		return heldResultRecorded, "insert " + pendingID + " on automaton " + auto.ID + " stays pending"
	default:
		return heldResultRefused, "option " + ans.ChosenOptionID + " has no path for a pending insert"
	}
}

// findHeldPendingInsert returns the automation holding the named pending
// insert, or nil when no automation holds one — the state having cleared.
func (m *Manager) findHeldPendingInsert(pendingID string) (*Automation, *PendingInsert) {
	autos := m.automations.List()
	for _, a := range autos {
		if pi := findPending(a, pendingID); pi != nil {
			return a, pi
		}
	}
	return nil, nil
}

// recordHeldAnswer writes the single record a routed answer gets — applied
// and refused alike — and its audit event: an operator_answer entry on the
// PRD's decisions, named after the state, the option, the outcome and the
// detail, and linked to the daemon question and the answer by id. It keeps
// the automation out of the PRD's decision log; an automation-level
// outcome (a pending insert) is audit-only.
func (m *Manager) recordHeldAnswer(prdID, key, state string, q Question, ans *Answer, result, detail string) {
	m.controlAudit(heldAnswerAuditAction, map[string]any{
		"prd_id":      prdID,
		"key":         key,
		"state":       state,
		"question_id": q.ID,
		"answer_id":   ans.ID,
		"option":      ans.ChosenOptionID,
		"result":      result,
		"detail":      detail,
		"actor":       heldAnswerActor,
	})
	if prdID == "" {
		return
	}
	p, ok := m.store.GetPRD(prdID)
	if !ok {
		log.Printf("[autonomous] prd=%s: record held answer %s on %s failed: not in the store", prdID, ans.ID, q.ID)
		return
	}
	note := fmt.Sprintf("held %s: %s (%s) — %s: %s", state, ans.ChosenOptionID, heldOptionText(ans.ChosenOptionID), result, detail)
	if len(note) > heldAnswerNoteMax {
		note = note[:heldAnswerNoteMax]
	}
	now := m.attemptNow()
	m.store.WithLock(func() {
		p.UpdatedAt = now
		p.Decisions = append(p.Decisions, Decision{
			At:            now,
			Kind:          heldAnswerDecisionKind,
			Actor:         heldAnswerActor,
			QuestionID:    q.ID,
			AnswerID:      ans.ID,
			DecisionState: string(QuestionAnswered),
			Note:          note,
		})
	})
	if err := m.store.SavePRD(p); err != nil {
		log.Printf("[autonomous] prd=%s: save held answer %s on %s: %v", prdID, ans.ID, q.ID, err)
	}
}
