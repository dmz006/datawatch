package mergepipeline

import (
	"errors"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// failingRaiser returns an error from the store path.
type failingRaiser struct{}

func (failingRaiser) RaiseUnlessOpenSourceLine(q autonomous.Question) (autonomous.Question, bool, error) {
	return autonomous.Question{}, false, errors.New("store down")
}

func samplePlan() ReleasePlan {
	return ReleasePlan{
		Project:     "datawatch",
		AutomatonID: "auto-1",
		Size:        "minor",
		Branches:    []string{"feature-a", "feature-b"},
		Version:     "9.1.0",
		Checks: []ReleaseCheck{
			{Name: "unit tests", Passed: true},
			{Name: "lint", Passed: false, Detail: "2 warnings"},
		},
		Steps: []string{"tag", "push", "deploy"},
	}
}

func TestRaiseReleaseDecisionNilRaiserReturnsNoChannel(t *testing.T) {
	id, raised, err := RaiseReleaseDecision(nil, samplePlan())
	if err != ErrNoQuestionChannel || raised || id != "" {
		t.Fatalf("got id=%q raised=%v err=%v; want ErrNoQuestionChannel", id, raised, err)
	}
}

func TestRaiseReleaseDecisionStoreErrorIsWrapped(t *testing.T) {
	_, raised, err := RaiseReleaseDecision(failingRaiser{}, samplePlan())
	if err == nil || raised || !strings.Contains(err.Error(), "store down") {
		t.Fatalf("got raised=%v err=%v; want wrapped store error", raised, err)
	}
}

func TestRaiseReleaseDecisionRaisesOneQuestionAndDedupes(t *testing.T) {
	qs := newQuestions(t)
	id, raised, err := RaiseReleaseDecision(qs, samplePlan())
	if err != nil || !raised || id == "" {
		t.Fatalf("first raise: id=%q raised=%v err=%v", id, raised, err)
	}
	again, raised2, err := RaiseReleaseDecision(qs, samplePlan())
	if err != nil || raised2 || again != id {
		t.Fatalf("second raise: id=%q raised=%v err=%v; want existing %q", again, raised2, err, id)
	}
	open := openQuestionsBy(t, qs, ReleaseQuestionAsker)
	if len(open) != 1 {
		t.Fatalf("open release questions = %d; want 1", len(open))
	}
	q := open[0]
	if q.PRDID != "auto-1" || q.Context != autonomous.QuestionContextPrd {
		t.Errorf("question not linked to Automaton: prd=%q context=%q", q.PRDID, q.Context)
	}
	if q.Default != releaseOptionHold {
		t.Errorf("default = %q; want %q", q.Default, releaseOptionHold)
	}
	if len(q.Options) != 2 || q.Options[0].ID != "yes" || q.Options[1].ID != "no" {
		t.Errorf("options = %+v; want yes and no", q.Options)
	}
}

func TestReleaseSizeChangesTheQuestion(t *testing.T) {
	qs := newQuestions(t)
	first, _, err := RaiseReleaseDecision(qs, samplePlan())
	if err != nil {
		t.Fatal(err)
	}
	other := samplePlan()
	other.Size = "patch"
	second, raised, err := RaiseReleaseDecision(qs, other)
	if err != nil || !raised || second == first {
		t.Fatalf("different size: id=%q raised=%v err=%v; want a new question", second, raised, err)
	}
}

func TestReleaseSourceLineIsStable(t *testing.T) {
	a := releaseSourceLine(samplePlan())
	b := releaseSourceLine(samplePlan())
	if a != b || len(a) != 64 {
		t.Fatalf("source line not stable: %q vs %q", a, b)
	}
	p := samplePlan()
	p.Project = "other"
	if releaseSourceLine(p) == a {
		t.Fatal("different project must give a different source line")
	}
	p = samplePlan()
	p.AutomatonID = "auto-2"
	if releaseSourceLine(p) == a {
		t.Fatal("different Automaton must give a different source line")
	}
	// Version and branches are not part of the identity.
	p = samplePlan()
	p.Version = "9.9.9"
	p.Branches = nil
	if releaseSourceLine(p) != a {
		t.Fatal("version and branches must not change the source line")
	}
}

func TestReleaseQuestionTextDescribesPlanAndEndsWithChoice(t *testing.T) {
	text := releaseQuestionText(samplePlan())
	for _, want := range []string{"datawatch", "auto-1", "minor", "9.1.0", "feature-a, feature-b", "unit tests: passed", "lint: failed (2 warnings)", "tag, then push, then deploy"} {
		if !strings.Contains(text, want) {
			t.Errorf("text missing %q:\n%s", want, text)
		}
	}
	if !strings.HasSuffix(text, releaseQuestionTail) {
		t.Errorf("text does not end with the choice:\n%s", text)
	}
}

func TestReleaseQuestionTextWithoutOptionalParts(t *testing.T) {
	text := releaseQuestionText(ReleasePlan{Project: "p"})
	for _, want := range []string{"It merges no branches.", "No checks were recorded.", "no steps are listed"} {
		if !strings.Contains(text, want) {
			t.Errorf("text missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Automaton") {
		t.Errorf("no Automaton should be named:\n%s", text)
	}
}

func TestReleaseQuestionTextIsShortened(t *testing.T) {
	p := samplePlan()
	for i := 0; i < 400; i++ {
		p.Checks = append(p.Checks, ReleaseCheck{Name: "long check name number", Passed: true, Detail: "detail text here"})
	}
	text := releaseQuestionText(p)
	if !strings.Contains(text, "[shortened]") || !strings.HasSuffix(text, releaseQuestionTail) {
		t.Errorf("long text not shortened with the choice kept at the end")
	}
}

func TestReleaseQuestionNoAutomatonHasNoAutomatonLink(t *testing.T) {
	qs := newQuestions(t)
	p := samplePlan()
	p.AutomatonID = ""
	if _, _, err := RaiseReleaseDecision(qs, p); err != nil {
		t.Fatal(err)
	}
	open := openQuestionsBy(t, qs, ReleaseQuestionAsker)
	if len(open) != 1 || open[0].PRDID != "" || open[0].Context != autonomous.QuestionContextNone {
		t.Fatalf("unexpected question linkage: %+v", open)
	}
}

// openQuestionsBy lists open questions asked by asker.
func openQuestionsBy(t *testing.T, s *autonomous.QuestionStore, asker string) []autonomous.Question {
	t.Helper()
	return s.List(autonomous.QuestionFilter{State: autonomous.QuestionOpen, Asker: asker, Limit: 100})
}
