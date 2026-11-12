// held_question_answer.go — the answer arm of the held-question pair.
//
// held_question_pass.go RAISES a daemon question when a PRD (automaton) or
// task, or a pending member in an Automaton, sits in a held state, and
// WITHDRAWS it once that state clears while the question is still open.
// This file is what happens in between, on the other side of the question:
// the operator answers that raised question, and its answer is routed back
// through the Manager's existing action paths. QuestionTick (question_tick.go)
// branches on the question's source and calls here instead of resuming a
// worker session — a held question is about the automation, not about a
// running task, so there is no session to continue.
//
// The mapping (the options each held state offers, held_question_table.go):
//
//	blocked             resume            -> Manager.ResumeParked (the block
//	                                          is a boot-park; re-admits the
//	                                          automaton)
//	                      retry           -> Manager.ResolveTaskReview on the
//	                                          task held for review (the block
//	                                          is that review hold)
//	                      change the spec -> Manager.ResolveTaskReview with the
//	                                          operator's revised spec
//	revisions asked     resume            -> Manager.Approve (the operator's
//	                                          review is done)
//	                      change the spec -> Manager.RequestRevision with the
//	                                          operator's spec
//	review held         retry / retry stronger / change the spec / cancel
//	                                    -> Manager.ResolveTaskReview, retry
//	                                        stronger raising the resolved
//	                                        effort one step
//	awaiting decision   resume            -> releases the task's park (the
//	                                          decision hold is released, or the
//	                                          parked worker task is re-admitted
//	                                          and its session is prompted to
//	                                          continue), same guard as a
//	                                          worker-question resume
//	                      cancel          -> Manager.CancelTask
//	pending insert      approve insert    -> Manager.ApproveInsertedMember (the
//	                                          operator-only insert path)
//	                      reject insert   -> record only, with a note that a
//	                                          pending member cannot be dropped
//	                                          by the daemon and so stays
//	                                          pending
//
// hold is the option for every one of them, and it means "record the answer,
// leave the state exactly as the operator left it" — nothing runs.
//
// Before any option is applied the processor re-checks that the item is
// still in the state the question was raised for: an answer that names a
// state the item left is REFUSED, the question is WITHDRAWN with a reason
// that names the state it was raised for and the state the item is in now,
// a result-stale decision entry records the refusal with the full answer,
// and nothing runs. The caller gets ErrHeldStateChanged, and the refusal is
// recorded either way so the timeline shows what happened.
//
// Every answer is recorded exactly once (idempotent by answer id): one
// operator_answer entry on the PRD's decisions carrying the operator's
// quoted words, the answer actor, the surface it came from, the chosen
// option and the applied / recorded / refused / stale result — plus an
// audit event. The entry is the idempotence marker: a second pass over the
// same answer sees it and does not re-run the action, re-record the entry,
// or re-withdraw the question.
//
// An answer that names no option (free text) is recorded, its words noted,
// and treated as hold: nothing runs. An answer never acts on an unanswered
// question — only QuestionStore.Answer records, and the processor reads.
//
// The pass continues to own the lifecycle of OPEN held questions,
// withdrawing one the moment its state clears; the processor closes the
// already-ANSWERED questions it has to refuse, which the pass leaves alone.

package autonomous

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

// ErrHeldStateChanged is returned by ProcessHeldAnswer when the operator's
// answer names a state the item is no longer in — the hold the question was
// raised for has cleared or moved. The refusal runs nothing and withdraws
// the question with a reason naming the old and the current state; the
// caller can recognize the outcome with errors.Is.
var ErrHeldStateChanged = errors.New("held state changed before the answer arrived")

// heldAnswerActor is the fallback actor for a routed answer: this path only
// ever carries operator answers, and one that does not name an actor is
// attributed to the operator.
const heldAnswerActor = "operator"

// heldAnswerDecisionKind is the PRD decision entry that marks one answer
// processed. It is linked to the daemon question and to the answer by id,
// and it is also the idempotence marker for ProcessHeldAnswer.
const heldAnswerDecisionKind = "operator_answer"

// heldAnswerAudit is the audit action for every answer the processor
// handles, whatever its outcome.
const heldAnswerAuditAction = "held_question_answered"

// heldAnswerNoteMax bounds the recorded note.
const heldAnswerNoteMax = 300

// Routing outcomes, for the audit event and the recorded note.
const (
	heldResultApplied  = "applied"  // the option ran through its existing path
	heldResultRecorded = "recorded" // the hold option (or free text): nothing ran, state kept
	heldResultRefused  = "refused"  // the option does not apply to this state/path, or the path refused
	heldResultStale    = "stale"    // the state moved: refused, the question was withdrawn, nothing ran
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

// heldAnswerActorOf attributes a routed answer: the actor its surface
// recorded, falling back to the operator for one that named none.
func heldAnswerActorOf(ans *Answer) string {
	if a := strings.TrimSpace(ans.Actor); a != "" {
		return a
	}
	return heldAnswerActor
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

// heldStaleReason builds the refusal text for an answer whose state moved:
// it always names the state the question was raised for and the state the
// item is in now. It is the reason the question is withdrawn with, and the
// one embedded in the returned ErrHeldStateChanged.
func heldStaleReason(state, current string) string {
	return "the held state the question was raised for changed: raised for " + state + ", but " + current
}

// --- the action paths the processor routes through -------------------------

// heldAnswerPaths are the existing action paths a routed held-question
// answer runs through, as the processor sees them. Each is the Manager's
// real path by default (ResolveTaskReview for the review holds, Approve and
// RequestRevision for the revision hold, ResumeParked for the boot-park,
// CancelTask for the cancels, ApproveInsertedMember for the pending insert,
// the decision-gate release for the awaiting hold) and they are swappable
// for tests, which fake the review, control and resume paths behind them
// while the held state itself stays real in the store.
type heldAnswerPaths struct {
	resolveReview   func(prdID, taskID string, res ReviewResolution, actor string) (*PRD, error)
	approve         func(prdID, actor, note string) (*PRD, error)
	requestRevision func(prdID, actor, note string) (*PRD, error)
	resumeParked    func(prdID, actor, note string) (*PRD, error)
	cancelTask      func(prdID, taskID, actor, reason string) (*PRD, error)
	approveInsert   func(automationID, pendingID, actor string, expectedVersion int64) (*Automation, error)
	releaseAwaiting func(ctx context.Context, p *PRD, t *Task, q Question, ans *Answer) error
	withdraw        func(questionID, actor, reason string) error
}

// heldAnswerDispatch reads the path table under the lock, or nil for "all
// default paths".
func (m *Manager) heldAnswerDispatch() *heldAnswerPaths {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.heldAnswerPaths
}

// dispatchResolveReview routes a review-hold resolution through the
// existing Manager path or its test stand-in.
func (m *Manager) dispatchResolveReview(prdID, taskID string, res ReviewResolution, actor string) (*PRD, error) {
	if h := m.heldAnswerDispatch(); h != nil && h.resolveReview != nil {
		return h.resolveReview(prdID, taskID, res, actor)
	}
	return m.ResolveTaskReview(prdID, taskID, res, actor)
}

// dispatchApprove routes a revision-hold approval (resume) through the
// existing Manager.Approve path or its test stand-in.
func (m *Manager) dispatchApprove(prdID, actor, note string) (*PRD, error) {
	if h := m.heldAnswerDispatch(); h != nil && h.approve != nil {
		return h.approve(prdID, actor, note)
	}
	return m.Approve(prdID, actor, note)
}

// dispatchRequestRevision routes a revision-hold spec change through the
// existing Manager.RequestRevision path or its test stand-in.
func (m *Manager) dispatchRequestRevision(prdID, actor, note string) (*PRD, error) {
	if h := m.heldAnswerDispatch(); h != nil && h.requestRevision != nil {
		return h.requestRevision(prdID, actor, note)
	}
	return m.RequestRevision(prdID, actor, note)
}

// dispatchResumeParked routes a boot-park block's resume through the
// existing Manager.ResumeParked path or its test stand-in.
func (m *Manager) dispatchResumeParked(prdID, actor, note string) (*PRD, error) {
	if h := m.heldAnswerDispatch(); h != nil && h.resumeParked != nil {
		return h.resumeParked(prdID, actor, note)
	}
	return m.ResumeParked(prdID, actor, note)
}

// dispatchCancelTask routes a cancel through the existing Manager.CancelTask
// path or its test stand-in.
func (m *Manager) dispatchCancelTask(prdID, taskID, actor, reason string) (*PRD, error) {
	if h := m.heldAnswerDispatch(); h != nil && h.cancelTask != nil {
		return h.cancelTask(prdID, taskID, actor, reason)
	}
	return m.CancelTask(prdID, taskID, actor, reason)
}

// dispatchApproveInsert routes a pending-insert approval through the
// existing operator-only Manager.ApproveInsertedMember path or its test
// stand-in.
func (m *Manager) dispatchApproveInsert(automationID, pendingID, actor string, expectedVersion int64) (*Automation, error) {
	if h := m.heldAnswerDispatch(); h != nil && h.approveInsert != nil {
		return h.approveInsert(automationID, pendingID, actor, expectedVersion)
	}
	return m.ApproveInsertedMember(automationID, pendingID, actor, expectedVersion)
}

// dispatchReleaseAwaiting routes an awaiting-decision resume through the
// existing release paths (the decision hold, or the parked worker task) or
// its test stand-in.
func (m *Manager) dispatchReleaseAwaiting(ctx context.Context, p *PRD, t *Task, q Question, ans *Answer) error {
	if h := m.heldAnswerDispatch(); h != nil && h.releaseAwaiting != nil {
		return h.releaseAwaiting(ctx, p, t, q, ans)
	}
	if t.Status == TaskWaitingDecision {
		m.releaseDecisionHold(p, t)
		return nil
	}
	return m.resumeHeldAwaitingTask(ctx, p, t, q, ans)
}

// dispatchWithdraw closes an already-answered held question the processor
// must refuse because its state moved. Default: the question store's
// withdraw, audited like the pass's own withdraws.
func (m *Manager) dispatchWithdraw(questionID, actor, reason string) error {
	if h := m.heldAnswerDispatch(); h != nil && h.withdraw != nil {
		return h.withdraw(questionID, actor, reason)
	}
	m.mu.Lock()
	store := m.decisionStore
	m.mu.Unlock()
	if store == nil {
		return fmt.Errorf("no question store to withdraw from")
	}
	if _, err := store.Withdraw(questionID, actor, reason); err != nil {
		return err
	}
	m.controlAudit("held_question_withdrawn", map[string]any{
		"question_id": questionID,
		"actor":       actor,
		"reason":      reason,
	})
	return nil
}

// ProcessHeldAnswer handles the operator's answer to a raised held question:
// it maps the chosen option (or the free text, which carries no option and
// is recorded without a state change) to the automation or task,
// re-verifies the held state still holds, applies the manager path named in
// the file header, records the answer once on the PRD's decisions and in
// the audit log, and — when the state moved — withdraws the question with
// the refusal reason and reports ErrHeldStateChanged.
//
// It is safe to call repeatedly for the same (question, answer): the second
// call is recognized through the recorded entry and does not re-run the
// action, re-record the entry, or re-withdraw. It returns a plain error
// only when the question is not a raised held question at all — a routing
// mistake upstream, not a routing outcome; every applied, recorded, refused
// or stale outcome is recorded. A stale outcome additionally returns ErrHeldStateChanged
// so the caller can tell a moved state from a recorded hold.
func (m *Manager) ProcessHeldAnswer(ctx context.Context, q Question, ans Answer) error {
	key, state, ok := heldQuestionIdentity(&q)
	if !ok {
		return fmt.Errorf("question %s is not a raised held question", q.ID)
	}
	prdID := q.PRDID
	if prdID != "" {
		if p, ok := m.store.GetPRD(prdID); ok && heldAnswerAlreadyRecorded(p, q.ID, ans.ID) {
			// Already routed once: the first call recorded it (and withdrew
			// the question when it refused), so the action stands and nothing
			// new is written. The tick re-asks every cycle while the question
			// is still answered, so this path must be quiet — pending inserts
			// included (their question names the member-PRD, so the entry
			// lands on that PRD's decisions).
			return nil
		}
	}
	result, detail := m.routeHeldAnswer(ctx, q, &ans, key, state)
	m.recordHeldAnswer(prdID, key, state, q, &ans, result, detail)
	if result == heldResultStale {
		// The refusal is recorded above; the question itself is closed now,
		// with the same reason, so the operator sees the move. An already
		// closed question makes the withdraw a recorded error, not a panic.
		if err := m.dispatchWithdraw(q.ID, heldAnswerActorOf(&ans), detail); err != nil {
			log.Printf("[autonomous] %s: withdrawing the held question for a stale answer failed: %v", q.ID, err)
		}
		return fmt.Errorf("%w: %s", ErrHeldStateChanged, detail)
	}
	return nil
}

// heldAnswerAlreadyRecorded reports whether prd already carries the
// ROUTED operator_answer entry for this (question, answer). It is the
// idempotence marker; see the package documentation.
//
// It deliberately does NOT match the submit-time operator_answer entry the
// daemon records on the PRD's decisions when the answer is submitted
// (QuestionStore's change hook → RecordQuestionChange): that entry is a
// plain log of the submission and carries no Result. Only recordHeldAnswer
// sets Result, so Result is what distinguishes "an answer was routed" from
// "an answer was submitted". Matching the hook's entry too would make the
// probe hold every first routing — the action would never run in a wired
// daemon.
func heldAnswerAlreadyRecorded(prd *PRD, questionID, answerID string) bool {
	if prd == nil {
		return false
	}
	for i := range prd.Decisions {
		d := &prd.Decisions[i]
		if d.Kind == heldAnswerDecisionKind && d.QuestionID == questionID && d.AnswerID == answerID && d.Result != "" {
			return true
		}
	}
	return false
}

// routeHeldAnswer dispatches the answer to the held state it was raised
// for. Every branch returns an outcome and a one-line detail; none of them
// records — that is the caller's single job, applied, recorded, refused and
// stale alike.
func (m *Manager) routeHeldAnswer(ctx context.Context, q Question, ans *Answer, key, state string) (result, detail string) {
	if ans.ChosenOptionID == "" {
		// The answer carried no option — free text, or a surface that does
		// not offer options. Its words are recorded with it and nothing runs,
		// exactly like hold.
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

// routeHeldBlocked handles a blocked automaton. The block re-checks first:
// a block that cleared is stale, refused and withdrawn, nothing runs. A
// block by a review-hold resolves through the review paths on the held task
// (retry, the revised spec, cancel); a boot-park resolves through the
// boot-resume path. A block for any other reason has no daemon resume path:
// the operator's choice is refused and named, and the state is left as it
// stands.
func (m *Manager) routeHeldBlocked(q Question, ans *Answer) (result, detail string) {
	if ans.ChosenOptionID == HeldOptionHold {
		// Hold runs nothing and never refuses: the state stays as the
		// operator left it, whatever else is true.
		return heldResultRecorded, "the automaton stays blocked"
	}
	prdID := q.PRDID
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return heldResultRefused, "the automaton " + prdID + " is not in the store"
	}
	if prd.Status != PRDBlocked {
		return heldResultStale, heldStaleReason(string(HeldBlocked), "the automaton is "+string(prd.Status)+" now")
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
		if _, err := m.dispatchResumeParked(prdID, heldAnswerActorOf(ans), heldAnswerNote(ans)); err != nil {
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

// routeHeldReview routes one review-hold answer through the existing
// Manager.ResolveTaskReview path. The task re-checks first: one that left
// the review hold is stale, refused and withdrawn, nothing runs. A change
// of the task's spec is carried in the answer's own wording: the operator's
// words are the spec.
func (m *Manager) routeHeldReview(q Question, ans *Answer, taskID string) (result, detail string) {
	if ans.ChosenOptionID == HeldOptionHold {
		return heldResultRecorded, "the task stays held for review"
	}
	t, ok := m.store.GetTask(taskID)
	if !ok {
		return heldResultRefused, "task " + taskID + " is not in the store"
	}
	if t.Status != TaskReviewNeeded {
		return heldResultStale, heldStaleReason(string(HeldReviewHeld), "task "+taskID+" is "+string(t.Status)+" now")
	}
	p, ok := m.store.GetPRD(q.PRDID)
	if !ok {
		return heldResultRefused, "the automaton " + q.PRDID + " is not in the store"
	}
	actor := heldAnswerActorOf(ans)
	note := heldAnswerNote(ans)
	switch ans.ChosenOptionID {
	case HeldOptionCancel:
		if _, err := m.dispatchResolveReview(q.PRDID, taskID, ReviewResolution{Action: reviewActionCancel, Note: note}, actor); err != nil {
			return heldResultRefused, "resolve-refused: " + err.Error()
		}
		return heldResultApplied, "task " + taskID + " was cancelled out of the review hold"
	case HeldOptionRetry:
		if _, err := m.dispatchResolveReview(q.PRDID, taskID, ReviewResolution{Action: reviewActionRetry, Note: note}, actor); err != nil {
			return heldResultRefused, "resolve-refused: " + err.Error()
		}
		return heldResultApplied, "task " + taskID + " was released from the review hold: retry"
	case HeldOptionRetryStronger:
		res := ReviewResolution{Action: reviewActionRetry, Note: note}
		if e := heldStrongerEffort(resolveTaskEffort(t, findStory(p, t.StoryID), p, m.AutomationForPRD(p))); e != "" {
			res.Effort = e
		}
		if _, err := m.dispatchResolveReview(q.PRDID, taskID, res, actor); err != nil {
			return heldResultRefused, "resolve-refused: " + err.Error()
		}
		return heldResultApplied, "task " + taskID + " was released from the review hold: retry stronger"
	case HeldOptionChangeSpec:
		spec := strings.TrimSpace(ans.Quote)
		if spec == "" {
			return heldResultRefused, "the answer carries no spec text to replace the task's; the task stays held"
		}
		if _, err := m.dispatchResolveReview(q.PRDID, taskID, ReviewResolution{Action: reviewActionRetry, Note: note, Spec: spec}, actor); err != nil {
			return heldResultRefused, "resolve-refused: " + err.Error()
		}
		return heldResultApplied, "task " + taskID + " was released from the review hold: retry with the revised spec"
	default:
		return heldResultRefused, "option " + ans.ChosenOptionID + " has no path for a review-hold"
	}
}

// routeHeldRevisions routes one revision-requested answer through the
// existing Manager approve / request-revision paths. The PRD re-checks
// first: one that left the revision hold is stale, refused and withdrawn,
// nothing runs. Resume — approve, the operator's review being done — and
// change the spec, the revised spec arriving in the answer's own wording,
// are applied; nothing applies a spec change without one.
func (m *Manager) routeHeldRevisions(q Question, ans *Answer) (result, detail string) {
	if ans.ChosenOptionID == HeldOptionHold {
		return heldResultRecorded, "the revision hold stays"
	}
	p, ok := m.store.GetPRD(q.PRDID)
	if !ok {
		return heldResultRefused, "the automaton " + q.PRDID + " is not in the store"
	}
	if p.Status != PRDRevisionsAsked {
		return heldResultStale, heldStaleReason(string(HeldRevisionRequested), "the automaton is "+string(p.Status)+" now")
	}
	actor := heldAnswerActorOf(ans)
	note := heldAnswerNote(ans)
	switch ans.ChosenOptionID {
	case HeldOptionResume:
		if _, err := m.dispatchApprove(q.PRDID, actor, note); err != nil {
			return heldResultRefused, "approve refused: " + err.Error()
		}
		return heldResultApplied, "the revision hold was approved out"
	case HeldOptionChangeSpec:
		spec := strings.TrimSpace(ans.Quote)
		if spec == "" {
			return heldResultRefused, "the answer carries no spec text to request a revision with; the hold stays"
		}
		if _, err := m.dispatchRequestRevision(q.PRDID, actor, spec); err != nil {
			return heldResultRefused, "request-revision refused: " + err.Error()
		}
		return heldResultApplied, "a revision was requested with the operator's spec"
	default:
		return heldResultRefused, "option " + ans.ChosenOptionID + " has no path for a revision-requested automaton"
	}
}

// routeHeldAwaiting routes one awaiting-decision answer: resume runs the
// existing release path for the task's park (nothing else is done — the
// release is the whole action), and cancel runs the existing cancel task
// path. The task re-checks first: one that left the decision park is stale,
// refused and withdrawn, nothing runs.
func (m *Manager) routeHeldAwaiting(ctx context.Context, q Question, ans *Answer, taskID string) (result, detail string) {
	if ans.ChosenOptionID == HeldOptionHold {
		return heldResultRecorded, "the task stays parked under a decision"
	}
	t, ok := m.store.GetTask(taskID)
	if !ok {
		return heldResultRefused, "task " + taskID + " is not in the store"
	}
	parked := (t.Status == TaskWaitingDecision || t.Status == TaskWaitingCapacity) && IsAwaitDecisionWaitReason(t.WaitReason)
	if !parked {
		return heldResultStale, heldStaleReason(string(HeldAwaitingDecision), "task "+taskID+" is "+string(t.Status)+" now")
	}
	p, ok := m.store.GetPRD(q.PRDID)
	if !ok {
		return heldResultRefused, "the automaton " + q.PRDID + " is not in the store"
	}
	switch ans.ChosenOptionID {
	case HeldOptionResume:
		if err := m.dispatchReleaseAwaiting(ctx, p, t, q, ans); err != nil {
			return heldResultRefused, "release refused: " + err.Error()
		}
		return heldResultApplied, "the task's decision park was released"
	case HeldOptionCancel:
		if _, err := m.dispatchCancelTask(q.PRDID, taskID, heldAnswerActorOf(ans), "task "+taskID+" cancelled via held question "+q.ID); err != nil {
			return heldResultRefused, "cancel-refused: " + err.Error()
		}
		return heldResultApplied, "task " + taskID + " was cancelled out of its decision hold"
	default:
		return heldResultRefused, "option " + ans.ChosenOptionID + " has no path for an awaiting-decision hold"
	}
}

// routeHeldPendingInsert handles a pending member insert. Approve goes
// through the existing operator-only insert path, with the actor the answer
// named. The pending insert re-checks first: one that is no longer pending
// is stale, refused and withdrawn, nothing runs. Reject-insert and hold are
// recorded only: there is no daemon path for dropping a pending member, so
// the insert stays pending either way and the note says so.
func (m *Manager) routeHeldPendingInsert(q Question, ans *Answer, pendingID string) (result, detail string) {
	autos := m.automations.List()
	if len(autos) > 1 {
		return heldResultRefused, "multiple automations exist; the pending insert cannot be resolved against them"
	}
	if len(autos) == 0 || findPending(autos[0], pendingID) == nil {
		return heldResultStale, heldStaleReason(string(HeldPendingInsert), "insert "+pendingID+" is no longer pending (approved or removed)")
	}
	auto := autos[0]
	switch ans.ChosenOptionID {
	case HeldOptionApproveInsert:
		if _, err := m.dispatchApproveInsert(auto.ID, pendingID, heldAnswerActorOf(ans), auto.Version); err != nil {
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

// heldStrongerEffort raises a task's resolved effort one rung for a
// retry-stronger. From the floor (no effort resolved) it returns the first
// rung — one rung up from an unset worker is Quick. From the top rung there
// is nothing stronger, so it returns "" and the resolution keeps its top.
func heldStrongerEffort(current Effort) Effort {
	ladder := []Effort{EffortQuick, EffortLow, EffortMedium, EffortHigh, EffortMax}
	switch current {
	case "":
		return ladder[0]
	case EffortQuick:
		return ladder[1]
	case EffortLow:
		return ladder[2]
	case EffortMedium, EffortNormal:
		return ladder[3]
	case EffortHigh, EffortThorough:
		return ladder[4]
	}
	return ""
}

// resumeHeldAwaitingTask releases a parked worker task through its existing
// path: it claims the release record, re-admits the task, and prompts the
// parked session to continue with the decision — the same guard as a
// worker-question resume. It is the awaiting-decision default under the
// release dispatch.
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

// recordHeldAnswer writes the single record a routed answer gets — applied,
// recorded, refused and stale alike — and its audit event: one
// operator_answer entry on the PRD's decisions carrying the operator's
// quoted words, the answer actor, the surface it came from, the chosen
// option, the outcome and its detail, and linked to the daemon question
// and the answer by id. It is both the record and the idempotence marker.
// A stale entry additionally names the withdrawn state, the way the
// question itself was closed.
func (m *Manager) recordHeldAnswer(prdID, key, state string, q Question, ans *Answer, result, detail string) {
	actor := heldAnswerActorOf(ans)
	m.controlAudit(heldAnswerAuditAction, map[string]any{
		"prd_id":      prdID,
		"key":         key,
		"state":       state,
		"question_id": q.ID,
		"answer_id":   ans.ID,
		"actor":       actor,
		"quote":       ans.Quote,
		"surface":     ans.Surface,
		"option":      ans.ChosenOptionID,
		"result":      result,
		"detail":      detail,
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
	decisionState := string(QuestionAnswered)
	if result == heldResultStale {
		// The question was closed by a withdrawal carrying the refusal
		// reason — the record names that the way the store does.
		decisionState = string(QuestionWithdrawn)
	}
	now := m.attemptNow()
	m.store.WithLock(func() {
		p.UpdatedAt = now
		p.Decisions = append(p.Decisions, Decision{
			At:            now,
			Kind:          heldAnswerDecisionKind,
			Actor:         actor,
			Note:          note,
			Quote:         ans.Quote,
			Surface:       ans.Surface,
			Option:        ans.ChosenOptionID,
			Result:        result,
			QuestionID:    q.ID,
			AnswerID:      ans.ID,
			DecisionState: decisionState,
		})
	})
	if err := m.store.SavePRD(p); err != nil {
		log.Printf("[autonomous] prd=%s: save held answer %s on %s: %v", prdID, ans.ID, q.ID, err)
	}
}
