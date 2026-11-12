package autonomous

import (
	"path/filepath"
	"strings"
	"testing"
)

// newOperatorDecisionRig builds a Manager with one Automaton and a question
// store whose change hook feeds the Manager, as the daemon wires it.
func newOperatorDecisionRig(t *testing.T) (*Manager, *QuestionStore, string) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("test spec", t.TempDir(), "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	s, err := NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	s.SetChangeHook(m.RecordQuestionChange)
	return m, s, prd.ID
}

// prdQuestion returns a valid question linked to the Automaton prdID.
func prdQuestion(prdID, text string) Question {
	q := sampleQuestion(text)
	q.Context = QuestionContextPrd
	q.PRDID = prdID
	q.AskedBy = "supervisor-1"
	q.AskerKind = QuestionAskerSupervisor
	return q
}

// operatorDecisions returns the operator_decision and operator_answer
// entries on the Automaton's log, in order.
func operatorDecisions(t *testing.T, m *Manager, prdID string) []Decision {
	t.Helper()
	p, ok := m.Store().GetPRD(prdID)
	if !ok {
		t.Fatalf("Automaton %s not found", prdID)
	}
	var out []Decision
	for _, d := range p.Decisions {
		if d.Kind == operatorDecisionKind || d.Kind == operatorAnswerKind {
			out = append(out, d)
		}
	}
	return out
}

func TestOperatorDecisionRaiseWritesEntry(t *testing.T) {
	m, s, prdID := newOperatorDecisionRig(t)
	long := strings.Repeat("x", 300)

	q, err := s.Raise(prdQuestion(prdID, long))
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}

	got := operatorDecisions(t, m, prdID)
	if len(got) != 1 {
		t.Fatalf("got %d entries; want 1: %+v", len(got), got)
	}
	d := got[0]
	if d.Kind != "operator_decision" || d.DecisionState != "open" || d.QuestionID != q.ID {
		t.Errorf("entry = %+v; want operator_decision, open, question %s", d, q.ID)
	}
	if d.Actor != "supervisor-1" {
		t.Errorf("actor = %q; want supervisor-1", d.Actor)
	}
	if len(d.Note) != operatorDecisionNoteMax {
		t.Errorf("note is %d bytes; want it cut to %d", len(d.Note), operatorDecisionNoteMax)
	}
	if d.AnswerID != "" {
		t.Errorf("answer_id = %q; want empty on a raise", d.AnswerID)
	}
}

func TestOperatorDecisionAnswerWritesLinkedEntry(t *testing.T) {
	m, s, prdID := newOperatorDecisionRig(t)
	q, err := s.Raise(prdQuestion(prdID, "Ship it?"))
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}

	a, err := s.Answer(q.ID, Answer{Actor: "operator", Surface: "pwa", ChosenOptionID: "first"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}

	got := operatorDecisions(t, m, prdID)
	if len(got) != 2 {
		t.Fatalf("got %d entries; want 2: %+v", len(got), got)
	}
	d := got[1]
	if d.Kind != "operator_answer" || d.DecisionState != "answered" {
		t.Errorf("entry = %+v; want operator_answer, answered", d)
	}
	if d.QuestionID != q.ID || d.AnswerID != a.ID || d.Note != a.ID {
		t.Errorf("entry links question %q answer %q note %q; want %q, %q, %q",
			d.QuestionID, d.AnswerID, d.Note, q.ID, a.ID, a.ID)
	}
	if d.Actor != "operator" {
		t.Errorf("actor = %q; want operator", d.Actor)
	}
}

func TestOperatorDecisionNoAutomatonWritesNothing(t *testing.T) {
	m, s, prdID := newOperatorDecisionRig(t)
	q := sampleQuestion("A question about nothing in particular?")

	raised, err := s.Raise(q)
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	if _, err := s.Answer(raised.ID, Answer{Actor: "operator", Surface: "pwa", ChosenOptionID: "first"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}

	if got := operatorDecisions(t, m, prdID); len(got) != 0 {
		t.Errorf("got %d entries on the Automaton; want none: %+v", len(got), got)
	}
}

func TestOperatorDecisionRepeatDoesNotDuplicate(t *testing.T) {
	m, s, prdID := newOperatorDecisionRig(t)
	q, err := s.Raise(prdQuestion(prdID, "Ship it?"))
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	a, err := s.Answer(q.ID, Answer{Actor: "operator", Surface: "pwa", ChosenOptionID: "first"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}

	m.RecordQuestionChange(QuestionChange{Kind: QuestionChangeRaised, Question: q})
	m.RecordQuestionChange(QuestionChange{Kind: QuestionChangeAnswered, Question: q, Answer: &a})

	if got := operatorDecisions(t, m, prdID); len(got) != 2 {
		t.Errorf("got %d entries after repeats; want 2: %+v", len(got), got)
	}
}

func TestOperatorDecisionWithdrawnWritesEntry(t *testing.T) {
	m, s, prdID := newOperatorDecisionRig(t)
	q, err := s.Raise(prdQuestion(prdID, "Ship it?"))
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}

	if _, err := s.Withdraw(q.ID, "operator", "no longer needed"); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}

	got := operatorDecisions(t, m, prdID)
	if len(got) != 2 {
		t.Fatalf("got %d entries; want 2: %+v", len(got), got)
	}
	d := got[1]
	if d.Kind != "operator_decision" || d.DecisionState != "withdrawn" || d.QuestionID != q.ID {
		t.Errorf("entry = %+v; want operator_decision, withdrawn, question %s", d, q.ID)
	}
	if d.Actor != "operator" || d.Note != "no longer needed" {
		t.Errorf("actor %q note %q; want operator, the reason", d.Actor, d.Note)
	}
}

func TestOperatorDecisionExpiredWritesEntry(t *testing.T) {
	m, s, prdID := newOperatorDecisionRig(t)
	q, err := s.Raise(prdQuestion(prdID, "Ship it?"))
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}

	if _, err := s.Expire(q.ID, "skipped"); err != nil {
		t.Fatalf("Expire: %v", err)
	}

	got := operatorDecisions(t, m, prdID)
	if len(got) != 2 || got[1].DecisionState != "expired" {
		t.Errorf("entries = %+v; want the second to be expired", got)
	}
}
