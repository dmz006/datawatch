package mergepipeline

import (
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// fakeAnswerReader is an in-memory ReleaseAnswerReader for testing
// ReleaseDecisionAnswer without a store file.
type fakeAnswerReader struct {
	question *autonomous.Question
	answers  []autonomous.AnswerView
}

func (f *fakeAnswerReader) Get(id string) (autonomous.Question, bool) {
	if f.question == nil || f.question.ID != id {
		return autonomous.Question{}, false
	}
	return *f.question, true
}

func (f *fakeAnswerReader) Answers(questionID string) ([]autonomous.AnswerView, bool) {
	if f.question == nil || f.question.ID != questionID {
		return nil, false
	}
	return f.answers, true
}

func decisionQuestion(state autonomous.QuestionState) *autonomous.Question {
	return &autonomous.Question{
		ID:      "q-1",
		AskedBy: ReleaseQuestionAsker,
		State:   state,
		Options: []autonomous.QuestionOption{
			{ID: releaseOptionShip, Label: "yes (ship it)"},
			{ID: releaseOptionHold, Label: "no (do not ship)"},
		},
	}
}

func choseOption(id string) autonomous.AnswerView {
	return autonomous.AnswerView{Answer: autonomous.Answer{ID: "a-" + id, ChosenOptionID: id}}
}

func TestReleaseDecisionAnswerNilReaderAndEmptyID(t *testing.T) {
	if yes, answered := ReleaseDecisionAnswer(nil, "q-1"); yes || answered {
		t.Fatalf("nil reader: got yes=%v answered=%v; want false, false", yes, answered)
	}
	if yes, answered := ReleaseDecisionAnswer(&fakeAnswerReader{}, ""); yes || answered {
		t.Fatalf("empty id: got yes=%v answered=%v; want false, false", yes, answered)
	}
}

func TestReleaseDecisionAnswerMissingQuestion(t *testing.T) {
	if yes, answered := ReleaseDecisionAnswer(&fakeAnswerReader{}, "q-missing"); yes || answered {
		t.Fatalf("missing: got yes=%v answered=%v; want false, false", yes, answered)
	}
}

func TestReleaseDecisionAnswerUnanswered(t *testing.T) {
	f := &fakeAnswerReader{question: decisionQuestion(autonomous.QuestionOpen)}
	if yes, answered := ReleaseDecisionAnswer(f, "q-1"); yes || answered {
		t.Fatalf("unanswered: got yes=%v answered=%v; want false, false", yes, answered)
	}
}

func TestReleaseDecisionAnswerWithdrawnCountsAsUnanswered(t *testing.T) {
	// A withdrawn answer is not a decision of record, even though one was
	// written before the withdrawal.
	f := &fakeAnswerReader{
		question: decisionQuestion(autonomous.QuestionWithdrawn),
		answers:  []autonomous.AnswerView{choseOption(releaseOptionShip)},
	}
	if yes, answered := ReleaseDecisionAnswer(f, "q-1"); yes || answered {
		t.Fatalf("withdrawn: got yes=%v answered=%v; want false, false", yes, answered)
	}
}

func TestReleaseDecisionAnswerYes(t *testing.T) {
	f := &fakeAnswerReader{
		question: decisionQuestion(autonomous.QuestionAnswered),
		answers:  []autonomous.AnswerView{choseOption(releaseOptionShip)},
	}
	if yes, answered := ReleaseDecisionAnswer(f, "q-1"); !yes || !answered {
		t.Fatalf("yes: got yes=%v answered=%v; want true, true", yes, answered)
	}
}

func TestReleaseDecisionAnswerNo(t *testing.T) {
	f := &fakeAnswerReader{
		question: decisionQuestion(autonomous.QuestionAnswered),
		answers:  []autonomous.AnswerView{choseOption(releaseOptionHold)},
	}
	if yes, answered := ReleaseDecisionAnswer(f, "q-1"); yes || !answered {
		t.Fatalf("no: got yes=%v answered=%v; want false, true", yes, answered)
	}
}

func TestReleaseDecisionAnswerSupersededCountsOnlyCurrent(t *testing.T) {
	// yes, then a changed mind to no: the yes is superseded and must not
	// count.
	yes := choseOption(releaseOptionShip)
	yes.Superseded = true
	yes.SupersededBy = "a-no"
	no := choseOption(releaseOptionHold)
	f := &fakeAnswerReader{
		question: decisionQuestion(autonomous.QuestionAnswered),
		answers:  []autonomous.AnswerView{yes, no},
	}
	if got, answered := ReleaseDecisionAnswer(f, "q-1"); got || !answered {
		t.Fatalf("superseded yes: got yes=%v answered=%v; want false, true", got, answered)
	}

	// and back to yes: the held answer is superseded and the yes counts.
	yes2 := choseOption(releaseOptionShip)
	no2 := choseOption(releaseOptionHold)
	no2.Superseded = true
	no2.SupersededBy = yes2.ID
	f2 := &fakeAnswerReader{
		question: decisionQuestion(autonomous.QuestionAnswered),
		answers:  []autonomous.AnswerView{no2, yes2},
	}
	if got, answered := ReleaseDecisionAnswer(f2, "q-1"); !got || !answered {
		t.Fatalf("superseded no: got yes=%v answered=%v; want true, true", got, answered)
	}
}

func TestReleaseDecisionAnswerDefensiveEdges(t *testing.T) {
	// An answered question with a lost answer list (the store cannot really
	// do this) is not a yes.
	f := &fakeAnswerReader{question: decisionQuestion(autonomous.QuestionAnswered)}
	if yes, answered := ReleaseDecisionAnswer(f, "q-1"); yes || answered {
		t.Fatalf("no answers: got yes=%v answered=%v; want false, false", yes, answered)
	}
	// Every stored answer superseded (likewise impossible through the store)
	// still counts as answered, but not as yes.
	g := &fakeAnswerReader{
		question: decisionQuestion(autonomous.QuestionAnswered),
		answers:  []autonomous.AnswerView{{Answer: autonomous.Answer{ID: "a-1"}, Superseded: true, SupersededBy: "a-2"}},
	}
	if yes, answered := ReleaseDecisionAnswer(g, "q-1"); yes || !answered {
		t.Fatalf("all superseded: got yes=%v answered=%v; want false, true", yes, answered)
	}
}

func TestReleaseDecisionAnswerAnsweredWithoutOptionsIsNotYes(t *testing.T) {
	// A free-text answer names no option, so it is not a yes.
	f := &fakeAnswerReader{
		question: decisionQuestion(autonomous.QuestionAnswered),
		answers:  []autonomous.AnswerView{{Answer: autonomous.Answer{ID: "a-q", Quote: "ship it, I suppose"}}},
	}
	if yes, answered := ReleaseDecisionAnswer(f, "q-1"); yes || !answered {
		t.Fatalf("free text: got yes=%v answered=%v; want false, true", yes, answered)
	}
}

// The real *autonomous.QuestionStore satisfies the interface and gives back
// the result the gate expects end to end.
func TestReleaseDecisionAnswerAgainstRealStore(t *testing.T) {
	s := newQuestions(t)
	id, raised, err := RaiseReleaseDecision(s, samplePlan())
	if err != nil || !raised || id == "" {
		t.Fatalf("raise: id=%q raised=%v err=%v", id, raised, err)
	}
	if yes, answered := ReleaseDecisionAnswer(s, id); yes || answered {
		t.Fatalf("before answer: got yes=%v answered=%v; want false, false", yes, answered)
	}
	a, err := s.Answer(id, autonomous.Answer{Actor: "operator", ChosenOptionID: releaseOptionShip})
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if yes, answered := ReleaseDecisionAnswer(s, id); !yes || !answered {
		t.Fatalf("after yes: got yes=%v answered=%v; want true, true", yes, answered)
	}
	// A changed mind supersedes, and so does the decision.
	a2, err := s.Answer(id, autonomous.Answer{Actor: "operator", ChosenOptionID: releaseOptionHold, Supersedes: a.ID})
	if err != nil {
		t.Fatalf("answer no: %v", err)
	}
	if yes, answered := ReleaseDecisionAnswer(s, id); yes || !answered {
		t.Fatalf("after no: got yes=%v answered=%v; want false, true", yes, answered)
	}
	if _, err := s.Answer(id, autonomous.Answer{Actor: "operator", ChosenOptionID: releaseOptionShip, Supersedes: a2.ID}); err != nil {
		t.Fatalf("answer yes again: %v", err)
	}
	if yes, answered := ReleaseDecisionAnswer(s, id); !yes || !answered {
		t.Fatalf("after yes again: got yes=%v answered=%v; want true, true", yes, answered)
	}
	if yes, answered := ReleaseDecisionAnswer(s, "q-does-not-exist"); yes || answered {
		t.Fatalf("unknown id: got yes=%v answered=%v; want false, false", yes, answered)
	}
}
