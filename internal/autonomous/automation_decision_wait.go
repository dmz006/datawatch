// Decision wait member: an Automation member that asks the operator one
// question and holds only its own branch until the answer arrives.
//
// Entering a decision_wait member raises an operator question through the same
// question store the decision assistant and the held questions use, marks the
// member waiting for the operator (Status running, WaitingForOperator set, the
// cursor held) and does nothing else. The Automation is never set to blocked: a
// sibling branch keeps running, and under all_must_succeed the join simply
// stays pending, because a waiting member counts as a branch still running.
//
// The step itself does no I/O, so the question is raised after the write wins
// (Coordinator.afterAdvance), exactly as member starts are. The question is
// found again, never raised twice, by a dedupe key derived from the Automation
// and the member, and the member records its question's id once it is linked.
// Everything that observes the question funnels into one idempotent resolver
// (Coordinator.syncDecisionWait): the question store's change hook (an answer
// completes the member along on_complete at once), the coordinator tick (a
// missed event is read from the store), and boot reconcile and resume (which
// also re-link an open question, or raise it if the crash fell between the
// write and the raise).
//
// An answer completes the member and the chain moves on. A withdrawn question
// fails the branch with a plain reason; a decision_wait member outside any fork
// has no branch to fail, so the Automation blocks with that reason instead.

package autonomous

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

// Names and texts of the decision wait question and member states.
const (
	// DecisionWaitQuestionAsker is the asker name on every decision wait question.
	DecisionWaitQuestionAsker = "decision-wait"
	// DecisionWaitQuestionLabel is the question label (a default shipped label).
	DecisionWaitQuestionLabel = "decision"

	// Option ids of the decision wait question. Any answer completes the member;
	// the two options differ only in whether the operator means to leave a note.
	DecisionWaitOptionContinue      = "continue"
	DecisionWaitOptionContinueNoted = "continue_noted"

	// DecisionWaitReason is the plain-language reason a member waiting for an
	// operator answer reports in the status data. It is never reported as
	// blocked.
	DecisionWaitReason = "waiting for you"

	decisionWaitDecisionKind = "decision_wait"

	decisionWaitOutcomeAnswered  = "answered"
	decisionWaitOutcomeWithdrawn = "question-withdrawn"

	// decisionWaitNoteMax bounds the answer or question text copied into a
	// Decision note, in runes.
	decisionWaitNoteMax = 160

	// decisionWaitSweepGrace is how old an open decision wait question must be
	// before the sweep may withdraw it as an orphan, so a question raised a
	// moment ago whose member link is still being written is never withdrawn.
	decisionWaitSweepGrace = 2 * time.Minute
)

// memberWaitingForOperator reports whether m is a decision_wait member that
// holds its cursor for an operator answer. A member that has ended is never
// waiting, whatever its stored flag says.
func memberWaitingForOperator(m *AutomationMember) bool {
	return m != nil && m.kind() == MemberKindDecisionWait && m.WaitingForOperator && m.Status == MemberRunning
}

// operatorWaiters returns the IDs of the decision_wait members of a that hold a
// cursor and wait for an operator answer, in member order.
func operatorWaiters(a *Automation) []string {
	var out []string
	for i := range a.Members {
		m := &a.Members[i]
		if memberWaitingForOperator(m) && isLiveCursor(a, m.ID) {
			out = append(out, m.ID)
		}
	}
	return out
}

// decisionWaitKey is the dedupe key of the question for one member: a hash of
// the Automation and the member, so the same member read twice (a retry, a
// restart, a resume) finds one question.
func decisionWaitKey(autoID, memberID string) string {
	sum := sha256.Sum256([]byte(DecisionWaitQuestionAsker + "\x00" + autoID + "\x00" + memberID))
	return hex.EncodeToString(sum[:])
}

// truncateRunes cuts s to n runes, never splitting a multi-byte character.
func truncateRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// buildDecisionWaitQuestion builds the operator question for member mem of a:
// the member's own question text plus one line saying where it comes from and
// that only this branch waits. Both options carry on, so the question never
// reads as a veto it cannot honour.
func buildDecisionWaitQuestion(a *Automation, mem *AutomationMember) Question {
	text := strings.TrimSpace(mem.Question) +
		fmt.Sprintf("\n\n(Automation %q, member %s: this branch waits for your answer; the other branches keep running.)", a.Name, mem.ID)
	return Question{
		Label:     DecisionWaitQuestionLabel,
		AskedBy:   DecisionWaitQuestionAsker,
		AskerKind: QuestionAskerSystem,
		Context:   QuestionContextNone,
		Text:      text,
		Options: []QuestionOption{
			{ID: DecisionWaitOptionContinue, Label: "Continue",
				Tradeoffs: "The branch moves on to its next member."},
			{ID: DecisionWaitOptionContinueNoted, Label: "Continue and note my reply",
				Tradeoffs: "Same effect; your own words are kept with the answer on the Automation's decision log."},
		},
		Default:          DecisionWaitOptionContinue,
		Blocking:         true,
		SourceLineSHA256: decisionWaitKey(a.ID, mem.ID),
	}
}

// ---- the step -------------------------------------------------------------

// enterDecisionWait handles the cursor arriving on a decision_wait member: the
// member starts running in the waiting state and holds its cursor. Nothing is
// started and nothing blocks. The coordinator raises the question after the
// write wins (stepEffects.waits). The Automation is not paused here: enter()
// has already moved the cursor and started nothing for a paused one.
func (st *stepper) enterDecisionWait(next *AutomationMember) {
	t := st.now
	next.Status = MemberRunning
	next.StartedAt = &t
	next.WaitingForSlot = false
	next.WaitingForOperator = true
	next.QuestionID = "" // a fresh wait: the coordinator links the question it raises
	addCursor(st.a, next.ID)
	st.eff.waits = append(st.eff.waits, next.ID)
	st.audit("automation_decision_wait", map[string]any{
		"automation_id": st.a.ID, "member": next.ID, "state": "waiting", "actor": coordinatorActor,
	})
	st.decision("waiting", fmt.Sprintf("member %s is %s: %s", st.name(next), DecisionWaitReason, truncateRunes(next.Question, decisionWaitNoteMax)))
}

// endDecisionWait records the end of a decision_wait member: it clears the
// waiting flag, leaves a decision_wait Decision linking the question and the
// answer, and, when the member ended any way other than its question being
// answered or withdrawn (a skip, a cancelled join branch), asks the coordinator
// to withdraw the question so no answer is left waiting for a member that has
// moved on.
func (st *stepper) endDecisionWait(mem *AutomationMember, end endOutcome) {
	qid := mem.QuestionID
	mem.WaitingForOperator = false
	note := end.note
	if note == "" {
		note = fmt.Sprintf("member %s ended %s", st.name(mem), end.outcome)
	}
	d := Decision{At: st.now, Kind: decisionWaitDecisionKind, Actor: coordinatorActor, Note: note, QuestionID: qid, AnswerID: end.answerID}
	switch end.outcome {
	case decisionWaitOutcomeAnswered:
		d.DecisionState = string(QuestionAnswered)
	case decisionWaitOutcomeWithdrawn:
		d.DecisionState = string(QuestionWithdrawn)
	}
	st.a.Decisions = trimDecisions(append(st.a.Decisions, d))
	if qid != "" && end.outcome != decisionWaitOutcomeAnswered && end.outcome != decisionWaitOutcomeWithdrawn {
		st.eff.settle = append(st.eff.settle, qid)
	}
}

// settleCancelledDecisionWait clears a decision_wait member that a join cancelled
// while it waited, and asks the coordinator to withdraw its question.
func (st *stepper) settleCancelledDecisionWait(b *AutomationMember) {
	if b.kind() != MemberKindDecisionWait {
		return
	}
	b.WaitingForOperator = false
	if b.QuestionID != "" {
		st.eff.settle = append(st.eff.settle, b.QuestionID)
	}
}

// ---- the coordinator ------------------------------------------------------

// decisionWaitStore returns the question store wired into the Manager, whether
// or not the operator question channel is enabled: observing a question needs
// the store, only raising one needs the channel.
func (m *Manager) decisionWaitStore() *QuestionStore {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.decisionStore
}

// coordinatorOrNil returns the started coordinator, or nil.
func (m *Manager) coordinatorOrNil() *Coordinator {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.coordinator
}

// afterDecisionWaits performs the question effects of a won write: it withdraws
// the questions of members that ended without an answer, then raises and links
// the question of every member that began waiting.
func (c *Coordinator) afterDecisionWaits(a *Automation, eff *stepEffects) {
	for _, qid := range eff.settle {
		c.settleDecisionWaitQuestion(qid, "the member that asked it ended without this answer")
	}
	for _, memID := range eff.waits {
		c.ensureDecisionWaitQuestion(a.ID, memID)
	}
}

// settleDecisionWaitQuestion withdraws question qid when it is still open. A
// question already answered or withdrawn, or one the store no longer holds, is
// left alone.
func (c *Coordinator) settleDecisionWaitQuestion(qid, reason string) {
	store := c.m.decisionWaitStore()
	if store == nil || qid == "" {
		return
	}
	q, ok := store.Get(qid)
	if !ok || q.State != QuestionOpen {
		return
	}
	if _, err := store.Withdraw(qid, coordinatorActor, reason); err != nil && !isQuestionNotOpen(err) {
		log.Printf("[autonomous] decision wait: withdraw question %s: %v", qid, err)
	}
}

func isQuestionNotOpen(err error) bool {
	return errors.Is(err, ErrQuestionNotOpen)
}

// findDecisionWaitQuestion returns the question the store holds for dedupe key
// key: an open one first, else the newest of any state. ok is false when the
// store holds none.
func findDecisionWaitQuestion(store *QuestionStore, key string) (Question, bool) {
	var best Question
	found := false
	for _, q := range store.List(QuestionFilter{Asker: DecisionWaitQuestionAsker, Limit: maxQuestionListLimit}) {
		if q.SourceLineSHA256 != key {
			continue
		}
		switch {
		case !found:
			best, found = q, true
		case q.State == QuestionOpen && best.State != QuestionOpen:
			best = q
		case q.State == best.State && q.CreatedAt != nil && best.CreatedAt != nil && q.CreatedAt.After(*best.CreatedAt):
			best = q
		}
	}
	return best, found
}

// ensureDecisionWaitQuestion makes sure the waiting member has its question:
// it finds the one the store holds for the member's key (an answered or
// withdrawn question is linked too, and resolves the member next), raises one
// when there is none, and records the question's id on the member. It is
// idempotent, so afterAdvance, the coordinator tick, resume and boot reconcile
// can all call it: exactly one question is ever raised for a member's wait.
//
// When the operator question channel is not available (no store wired, or
// autonomous.questions.enabled off) the question could never be answered, so
// the member fails with that reason rather than waiting forever unseen.
func (c *Coordinator) ensureDecisionWaitQuestion(autoID, memID string) {
	a, ok := c.m.automations.Get(autoID)
	if !ok || (a.Status != AutomationActive && a.Status != AutomationPaused) {
		return
	}
	mem := automationMember(a, memID)
	if !memberWaitingForOperator(mem) || !isLiveCursor(a, memID) {
		return
	}
	store, enabled := c.m.operatorQuestionChannel()
	if store != nil && mem.QuestionID != "" {
		if _, held := store.Get(mem.QuestionID); held {
			return // linked, and the store still holds it
		}
	}
	if store == nil || !enabled {
		c.failDecisionWait(autoID, memID, "the operator question channel is not available (autonomous.questions is off or no question store is wired), so member "+memID+" cannot ask its question")
		return
	}
	key := decisionWaitKey(a.ID, mem.ID)
	q, found := findDecisionWaitQuestion(store, key)
	raised := false
	if !found {
		var err error
		q, raised, err = store.RaiseUnlessOpenSourceLine(buildDecisionWaitQuestion(a, mem))
		if err != nil {
			log.Printf("[autonomous] automation %s member %s: raise decision wait question: %v", autoID, memID, err)
			return // the tick tries again
		}
	}
	linked, err := c.m.automations.Update(autoID, 0, func(cur *Automation) error {
		m := automationMember(cur, memID)
		if !memberWaitingForOperator(m) {
			return errStaleStep
		}
		if m.QuestionID == q.ID {
			return errStaleStep // already linked: nothing to write
		}
		m.QuestionID = q.ID
		return nil
	})
	if err != nil && !errors.Is(err, errStaleStep) {
		log.Printf("[autonomous] automation %s member %s: link decision wait question %s: %v", autoID, memID, q.ID, err)
		return
	}
	if raised {
		c.audit("automation_decision_wait", map[string]any{
			"automation_id": autoID, "member": memID, "state": "question_raised",
			"question_id": q.ID, "actor": coordinatorActor,
		})
		c.decisionWaitAlert(a, mem, q)
	}
	if linked != nil {
		// The answer or a withdrawal may have landed before the link was written.
		c.syncDecisionWait(autoID, memID)
	}
}

// decisionWaitAlert raises the one operator-question alert for a member that
// began waiting. It says the Automation is waiting for the operator, not that
// it is blocked: the blocked alert is never used for this member.
func (c *Coordinator) decisionWaitAlert(a *Automation, mem *AutomationMember, q Question) {
	c.alert("warn", "Automation waiting for you",
		fmt.Sprintf("Automation %q (%s): member %s is waiting for your answer to question %s: %s\nOnly this branch waits; the Automation is not blocked and its other branches keep running.",
			a.Name, a.ID, mem.ID, q.ID, truncateRunes(mem.Question, decisionWaitNoteMax)))
}

// failDecisionWait ends a waiting member as failed with reason. A member on a
// fork branch fails its branch; any other member blocks the Automation, since
// it has no on_failed edge to take.
func (c *Coordinator) failDecisionWait(autoID, memID, reason string) {
	c.advance(autoID, stepInput{memberID: memID, end: endOutcome{
		memStatus: MemberFailed, outcome: "question-unavailable", direct: reason, note: reason,
	}})
}

// syncDecisionWait reads the state of a waiting member's question and moves the
// member on when it is decided: an answer completes it along on_complete, a
// withdrawn or expired question fails its branch. A question still open, a
// member that is not waiting and a member not linked yet change nothing, so any
// number of callers may run it.
func (c *Coordinator) syncDecisionWait(autoID, memID string) {
	a, ok := c.m.automations.Get(autoID)
	if !ok || (a.Status != AutomationActive && a.Status != AutomationPaused) {
		return
	}
	mem := automationMember(a, memID)
	if !memberWaitingForOperator(mem) || !isLiveCursor(a, memID) || mem.QuestionID == "" {
		return
	}
	store := c.m.decisionWaitStore()
	if store == nil {
		return
	}
	q, ok := store.Get(mem.QuestionID)
	if !ok {
		return // ensureDecisionWaitQuestion re-raises a question the store lost
	}
	switch q.State {
	case QuestionAnswered:
		ans, _ := store.CurrentAnswer(q.ID)
		note := fmt.Sprintf("operator answered question %s", q.ID)
		if text := truncateRunes(ans.Quote, decisionWaitNoteMax); text != "" {
			note += ": " + text
		} else if ans.ChosenOptionID != "" {
			note += ": " + ans.ChosenOptionID
		}
		c.advance(autoID, stepInput{memberID: memID, end: endOutcome{
			edge: edgeOnComplete, memStatus: MemberCompleted, outcome: decisionWaitOutcomeAnswered,
			success: true, note: note, answerID: ans.ID,
		}})
	case QuestionWithdrawn, QuestionExpired:
		reason := "withdrawn"
		if q.State == QuestionExpired {
			reason = "expired"
		}
		if meta := store.withdrawnMeta(q.ID); meta.reason != "" {
			reason += " (" + meta.reason + ")"
		}
		consequence := "the Automation blocks"
		if joinOf(a, memID) != nil {
			consequence = "its branch fails"
		}
		msg := fmt.Sprintf("operator question %s for member %s was %s without an answer; %s", q.ID, memID, reason, consequence)
		c.advance(autoID, stepInput{memberID: memID, end: endOutcome{
			memStatus: MemberFailed, outcome: decisionWaitOutcomeWithdrawn, note: msg, direct: msg,
		}})
	}
}

// withdrawnMeta returns the actor and reason a question was withdrawn with.
func (s *QuestionStore) withdrawnMeta(id string) questionWithdrawMeta {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withdrawMeta[id]
}

// reconcileDecisionWait brings one waiting member in line with its question:
// the question is found, linked or raised, and a decided one resolves the
// member. Boot reconcile, resume and the coordinator tick all use it.
func (c *Coordinator) reconcileDecisionWait(autoID, memID string) {
	c.ensureDecisionWaitQuestion(autoID, memID)
	c.syncDecisionWait(autoID, memID)
}

// decisionWaitPass is the coordinator tick's pass over decision wait members:
// every waiting member is reconciled with its question (so an answer or a
// withdrawal the change hook missed is read from the store), then orphaned
// questions are swept. It starts no goroutine and is safe to run any number of
// times.
func (c *Coordinator) decisionWaitPass() {
	if c == nil || c.m == nil || c.m.automations == nil {
		return
	}
	for _, a := range c.m.automations.List() {
		if a.Status != AutomationActive && a.Status != AutomationPaused {
			continue
		}
		for _, id := range liveCursorIDs(a) {
			if mem := automationMember(a, id); memberWaitingForOperator(mem) {
				c.reconcileDecisionWait(a.ID, id)
			}
		}
	}
	c.sweepDecisionWaitQuestions()
}

// sweepDecisionWaitQuestions withdraws open decision wait questions that no
// waiting member links any more: the member was removed, skipped, cancelled with
// its Automation, or its join fired without it. A question younger than
// decisionWaitSweepGrace is left alone while its link is being written.
func (c *Coordinator) sweepDecisionWaitQuestions() {
	store := c.m.decisionWaitStore()
	if store == nil {
		return
	}
	open := store.List(QuestionFilter{State: QuestionOpen, Asker: DecisionWaitQuestionAsker, Limit: maxQuestionListLimit})
	if len(open) == 0 {
		return
	}
	linked := make(map[string]bool)
	for _, a := range c.m.automations.List() {
		for i := range a.Members {
			if m := &a.Members[i]; memberWaitingForOperator(m) && m.QuestionID != "" {
				linked[m.QuestionID] = true
			}
		}
	}
	now := c.now()
	for _, q := range open {
		if linked[q.ID] || q.AskerKind != QuestionAskerSystem {
			continue
		}
		if q.CreatedAt != nil && now.Sub(*q.CreatedAt) < decisionWaitSweepGrace {
			continue
		}
		c.settleDecisionWaitQuestion(q.ID, "no member is waiting for this question any more")
	}
}

// onDecisionWaitChange is the question store's route to a waiting member: an
// answered, withdrawn or expired decision wait question resolves the member
// that links it at once (see syncDecisionWait). It reports whether the change
// was a decision wait question, so the caller does not hand it to the other
// handlers. A change for a member not linked yet is left to the link step and
// the coordinator tick, which read the question's state from the store.
func (m *Manager) onDecisionWaitChange(ch QuestionChange) bool {
	q := ch.Question
	if q.AskedBy != DecisionWaitQuestionAsker || q.AskerKind != QuestionAskerSystem {
		return false
	}
	switch ch.Kind {
	case QuestionChangeAnswered, QuestionChangeWithdrawn, QuestionChangeExpired:
	default:
		return true
	}
	m.mu.Lock()
	c := m.coordinator
	m.mu.Unlock()
	if c == nil || m.automations == nil {
		return true
	}
	for _, a := range m.automations.List() {
		for i := range a.Members {
			if mem := &a.Members[i]; memberWaitingForOperator(mem) && mem.QuestionID == q.ID {
				c.syncDecisionWait(a.ID, mem.ID)
			}
		}
	}
	return true
}
