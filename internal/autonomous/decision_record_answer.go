package autonomous

// Applying the answer of a decision record question.
//
// When the question of a decision record is answered, the DecisionRecords
// service
//
//  1. stores the answer on the record (DecisionRecordStore.RecordAnswer): the
//     answer id, the chosen option, the quote exactly as the operator wrote
//     it, the actor, the surface and the answered time, and the record
//     becomes answered;
//  2. releases any task held by the decision gate on that question, by
//     waking the executor that waits for it (the same wake an answer to any
//     other question sends, API.OnQuestionChange);
//  3. hands over to applyAnswerEffects, the hook point for the document line
//     rewrite and the follow-up steps.
//
// Two paths trigger it so nothing is missed. The prompt path is the question
// store change hook (ChainQuestionHook adds the service after whatever hook
// the daemon already installed; SetChangeHook holds one function, so it is
// chained, never replaced). The recovery path is Reconcile: an open record
// whose question is answered but whose record has no answer yet gets it
// applied on the next poll. Applying the same answer twice changes nothing
// and runs the effects once.
//
// The effects run after the service lock is released, so a follow-up that
// takes a while never blocks Create, Update or Reconcile.

import (
	"errors"
	"fmt"
	"log"
)

// decisionAnswerApplied is one answer that was just stored on a record and
// whose release and effects still have to run.
type decisionAnswerApplied struct {
	rec DecisionRecord
	q   Question
	a   Answer
}

// SetAnswerRelease sets the function that wakes whatever holds a task on the
// answered question. The daemon passes API.OnQuestionChange, which wakes the
// live executor (or starts the run) so the task begins without waiting for
// its next poll. A nil function leaves the executor to its own poll.
func (s *DecisionRecords) SetAnswerRelease(fn func(QuestionChange)) {
	s.effectsMu.Lock()
	s.release = fn
	s.effectsMu.Unlock()
}

// ChainQuestionHook installs the service on the question store change hook
// after the hook that is already there: the existing hook runs first, then
// the service. A hook set with SetChangeHook after this call replaces the
// chain, so call it once the daemon's own hook is in place.
func (s *DecisionRecords) ChainQuestionHook() {
	qs := s.questions
	if qs == nil {
		return
	}
	qs.mu.Lock()
	prev := qs.changeHook
	qs.changeHook = func(c QuestionChange) {
		if prev != nil {
			prev(c)
		}
		s.OnQuestionChange(c)
	}
	qs.mu.Unlock()
}

// OnQuestionChange is the prompt trigger. It applies the answer of a decision
// record question to its record and ignores every other change. It does
// nothing while a switch the decision records need is off; Reconcile applies
// the answer once the switches are on.
func (s *DecisionRecords) OnQuestionChange(c QuestionChange) {
	if c.Kind != QuestionChangeAnswered || c.Question.ID == "" {
		return
	}
	if s.available() != nil {
		return
	}
	rec, ok := s.records.GetByQuestionID(c.Question.ID)
	if !ok && c.Question.DecisionRecordID != "" {
		rec, ok = s.records.Get(c.Question.DecisionRecordID)
	}
	if !ok {
		return // an ordinary question, not one a record owns
	}
	var a Answer
	switch {
	case c.Answer != nil:
		a = *c.Answer
	default:
		cur, found := s.questions.CurrentAnswer(c.Question.ID)
		if !found {
			return
		}
		a = cur
	}
	s.mu.Lock()
	_, err := s.applyAnswerLocked(rec, c.Question, a)
	s.mu.Unlock()
	if err != nil {
		log.Printf("[autonomous] decision record %s: apply answer: %v", rec.ID, err)
	}
	s.drainAnswerEffects()
}

// applyAnswerLocked stores answer a on the record and queues the release and
// the effects. It reports whether anything changed: an answer already stored
// on the record, an answered question that belongs to a withdrawn record,
// changes nothing. The caller holds s.mu and calls drainAnswerEffects after
// releasing it.
func (s *DecisionRecords) applyAnswerLocked(rec DecisionRecord, q Question, a Answer) (bool, error) {
	cur, ok := s.records.Get(rec.ID)
	if !ok {
		return false, fmt.Errorf("decision record %s: %w", rec.ID, ErrDecisionRecordNotFound)
	}
	if cur.State == DecisionRecordWithdrawn {
		return false, nil
	}
	if cur.State == DecisionRecordAnswered && cur.Answer != nil && cur.Answer.AnswerID == a.ID {
		return false, nil
	}
	if cur.QuestionID == "" && q.ID != "" {
		// The record was never linked (a crash after the raise): link it now.
		linked, err := s.records.SetQuestionID(cur.ID, q.ID)
		if err != nil && !errors.Is(err, ErrDecisionRecordClosed) {
			return false, fmt.Errorf("link question %s: %w", q.ID, err)
		}
		if err == nil {
			cur = linked
		}
	}
	stored, err := s.records.RecordAnswer(cur.ID, DecisionAnswer{
		AnswerID:       a.ID,
		ChosenOptionID: a.ChosenOptionID,
		Quote:          a.Quote,
		Actor:          a.Actor,
		Surface:        a.Surface,
		AnsweredAt:     cloneTime(a.CreatedAt),
	})
	if err != nil {
		return false, fmt.Errorf("store answer %s: %w", a.ID, err)
	}
	s.effectsMu.Lock()
	s.pending = append(s.pending, decisionAnswerApplied{rec: stored, q: q, a: a})
	s.effectsMu.Unlock()
	return true, nil
}

// drainAnswerEffects runs the release and the effects of every answer that
// was stored since the last drain. It must be called without s.mu held.
func (s *DecisionRecords) drainAnswerEffects() {
	s.effectsMu.Lock()
	todo := s.pending
	s.pending = nil
	release := s.release
	s.effectsMu.Unlock()
	for _, p := range todo {
		if release != nil {
			answered := p.a
			release(QuestionChange{Kind: QuestionChangeAnswered, Question: p.q, Answer: &answered})
		}
		s.applyAnswerEffects(p.rec, p.q, p.a)
	}
}

// applyAnswerEffects is the hook point that runs once for each answer stored
// on a record, after the record is updated and the gate is released. The
// document status line rewrite and the follow-up steps hang off it.
func (s *DecisionRecords) applyAnswerEffects(rec DecisionRecord, q Question, a Answer) {
	s.effectsMu.Lock()
	seen := s.effectsSeen
	s.effectsMu.Unlock()
	if seen != nil {
		seen(rec, q, a)
	}
	s.rewriteDocStatusLine(rec, q, a)
	s.runFollowUp(rec, q, a)
}
