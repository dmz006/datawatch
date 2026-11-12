// Operator questions on the Automaton decision log.
//
// Every operator question raised with an Automaton id appears on that
// Automaton's decision log as an operator_decision entry, and its answer as a
// second, operator_answer entry linked by question_id and answer_id. A
// withdrawal or an expiry is another operator_decision entry carrying that
// state. The question store reports each change through its change hook
// (QuestionStore.SetChangeHook); the daemon wires the hook to
// RecordQuestionChange.
package autonomous

import (
	"log"
	"time"
)

// Decision.Kind values written for operator questions.
const (
	operatorDecisionKind = "operator_decision"
	operatorAnswerKind   = "operator_answer"
)

// operatorDecisionNoteMax bounds the question text copied into a decision
// entry's note, in bytes.
const operatorDecisionNoteMax = 200

// RecordQuestionChange appends the decision-log entry for one question
// change to the Automaton the question was raised for. A question without an
// Automaton id, or one whose Automaton does not exist, writes nothing. The
// same change of the same question is recorded once: a repeated call finds
// the entry already on the log and appends nothing. The Automaton is saved
// with the same store pattern holdTaskForReview uses; a failed save is logged
// and does not fail the question change.
func (m *Manager) RecordQuestionChange(c QuestionChange) {
	q := c.Question
	if q.PRDID == "" {
		return
	}
	dec, ok := operatorDecisionFor(c, m.attemptNow())
	if !ok {
		return
	}
	p, ok := m.store.GetPRD(q.PRDID)
	if !ok {
		return
	}
	appended := false
	m.store.WithLock(func() {
		for _, d := range p.Decisions {
			if d.Kind == dec.Kind && d.QuestionID == dec.QuestionID &&
				d.DecisionState == dec.DecisionState && d.AnswerID == dec.AnswerID {
				return
			}
		}
		p.Decisions = append(p.Decisions, dec)
		appended = true
	})
	if !appended {
		return
	}
	if err := m.store.SavePRD(p); err != nil {
		log.Printf("[autonomous] prd=%s save %s decision: %v", p.ID, dec.Kind, err)
	}
}

// operatorDecisionFor builds the decision entry for c. It reports false for a
// change kind that has no entry, or an answered change without its answer.
func operatorDecisionFor(c QuestionChange, now time.Time) (Decision, bool) {
	q := c.Question
	dec := Decision{At: now, Kind: operatorDecisionKind, QuestionID: q.ID}
	switch c.Kind {
	case QuestionChangeRaised:
		dec.Actor = questionAsker(q)
		dec.Note = truncateBytes(q.Text, operatorDecisionNoteMax)
		dec.DecisionState = string(QuestionOpen)
	case QuestionChangeAnswered:
		if c.Answer == nil {
			return Decision{}, false
		}
		dec.Kind = operatorAnswerKind
		dec.Actor = c.Answer.Actor
		dec.Note = c.Answer.ID
		dec.AnswerID = c.Answer.ID
		dec.DecisionState = string(QuestionAnswered)
	case QuestionChangeWithdrawn, QuestionChangeExpired:
		dec.Actor = c.Actor
		if dec.Actor == "" {
			dec.Actor = questionAsker(q)
		}
		dec.Note = truncateBytes(c.Reason, operatorDecisionNoteMax)
		if dec.Note == "" {
			dec.Note = truncateBytes(q.Text, operatorDecisionNoteMax)
		}
		dec.DecisionState = string(QuestionWithdrawn)
		if c.Kind == QuestionChangeExpired {
			dec.DecisionState = string(QuestionExpired)
		}
	default:
		return Decision{}, false
	}
	return dec, true
}

// questionAsker names who raised q: the asker, else the asker kind.
func questionAsker(q Question) string {
	if q.AskedBy != "" {
		return q.AskedBy
	}
	return string(q.AskerKind)
}
