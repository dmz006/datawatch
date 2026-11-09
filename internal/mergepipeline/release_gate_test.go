package mergepipeline

import (
	"fmt"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

func TestDecideRelease(t *testing.T) {
	cases := []struct {
		name         string
		mode         string
		checksPassed bool
		wantMode     string
		wantAction   ReleaseGateAction
		wantReason   string
	}{
		{"auto passing ships", config.ReleaseApprovalAuto, true, config.ReleaseApprovalAuto, ReleaseGateShip, "checks passed; shipping automatically"},
		{"auto failing holds for person", config.ReleaseApprovalAuto, false, config.ReleaseApprovalAuto, ReleaseGateHoldForPerson, "checks failed; a person needs to look before this ships"},
		{"ask passing holds for decision", config.ReleaseApprovalAsk, true, config.ReleaseApprovalAsk, ReleaseGateHoldForDecision, "prepared; waiting for your decision"},
		{"ask failing holds for decision", config.ReleaseApprovalAsk, false, config.ReleaseApprovalAsk, ReleaseGateHoldForDecision, "prepared; waiting for your decision"},
		{"manual passing holds for person", config.ReleaseApprovalManual, true, config.ReleaseApprovalManual, ReleaseGateHoldForPerson, "prepared; a person ships this release"},
		{"manual failing holds for person", config.ReleaseApprovalManual, false, config.ReleaseApprovalManual, ReleaseGateHoldForPerson, "prepared; a person ships this release"},
		{"unknown mode never ships", "yolo", true, config.ReleaseApprovalAsk, ReleaseGateHoldForDecision, "prepared; waiting for your decision"},
		{"empty mode never ships", "", true, config.ReleaseApprovalAsk, ReleaseGateHoldForDecision, "prepared; waiting for your decision"},
		{"case-mismatched mode never ships", "AUTO", true, config.ReleaseApprovalAsk, ReleaseGateHoldForDecision, "prepared; waiting for your decision"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideRelease(tc.mode, "minor", tc.checksPassed)
			if got.Action != tc.wantAction {
				t.Errorf("Action = %q, want %q", got.Action, tc.wantAction)
			}
			if got.Mode != tc.wantMode {
				t.Errorf("Mode = %q, want %q", got.Mode, tc.wantMode)
			}
			if got.Size != "minor" {
				t.Errorf("Size = %q, want %q", got.Size, "minor")
			}
			if got.Reason != tc.wantReason {
				t.Errorf("Reason = %q, want %q", got.Reason, tc.wantReason)
			}
		})
	}
}

func TestDecideReleaseActionValues(t *testing.T) {
	if string(ReleaseGateShip) != "ship" ||
		string(ReleaseGateHoldForDecision) != "hold_for_decision" ||
		string(ReleaseGateHoldForPerson) != "hold_for_person" {
		t.Fatal("ReleaseGateAction string values changed")
	}
}

// fakeReleaseQuestions is an in-memory stand-in for the question store, for
// the release gate tests. It raises a question like the real store does — one
// open question per source line, found rather than raised again — and it
// reads answers back the way the gate needs them: the state, and the current
// (not superseded) answer. It never touches a file.
type fakeReleaseQuestions struct {
	mu sync.Mutex
	// dedupeAnswered makes a raise find an answered question for the same
	// source line, as the gate expects a store to do. The real store dedupes
	// open questions only; see TestReleaseApprovalAskShipsAfterYes.
	dedupeAnswered bool
	seq            int
	raised         int
	byLine         map[string]string
	questions      map[string]autonomous.Question
	answers        map[string][]autonomous.AnswerView
}

func newFakeReleaseQuestions() *fakeReleaseQuestions {
	return &fakeReleaseQuestions{
		byLine:    map[string]string{},
		questions: map[string]autonomous.Question{},
		answers:   map[string][]autonomous.AnswerView{},
	}
}

// newFakeReleaseQuestionsDedupingAnswered is the store the yes path needs: a
// re-run finds the answered question for its release.
func newFakeReleaseQuestionsDedupingAnswered() *fakeReleaseQuestions {
	f := newFakeReleaseQuestions()
	f.dedupeAnswered = true
	return f
}

// RaiseUnlessOpenSourceLine satisfies QuestionRaiser.
func (f *fakeReleaseQuestions) RaiseUnlessOpenSourceLine(q autonomous.Question) (autonomous.Question, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id, ok := f.byLine[q.SourceLineSHA256]; ok {
		have := f.questions[id]
		if have.State == autonomous.QuestionOpen || (f.dedupeAnswered && have.State == autonomous.QuestionAnswered) {
			return have, false, nil
		}
	}
	f.seq++
	f.raised++
	q.ID = fmt.Sprintf("fakeq-%d", f.seq)
	q.State = autonomous.QuestionOpen
	f.questions[q.ID] = q
	f.byLine[q.SourceLineSHA256] = q.ID
	return q, true, nil
}

// Get satisfies ReleaseAnswerReader.
func (f *fakeReleaseQuestions) Get(id string) (autonomous.Question, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q, ok := f.questions[id]
	return q, ok
}

// Answers satisfies ReleaseAnswerReader.
func (f *fakeReleaseQuestions) Answers(id string) ([]autonomous.AnswerView, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.questions[id]; !ok {
		return nil, false
	}
	return append([]autonomous.AnswerView(nil), f.answers[id]...), true
}

// open returns the one open question's id; the test fails unless exactly one
// question is open.
func (f *fakeReleaseQuestions) open(t *testing.T) (string, autonomous.Question) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []string
	for id, q := range f.questions {
		if q.State == autonomous.QuestionOpen {
			ids = append(ids, id)
		}
	}
	if len(ids) != 1 {
		t.Fatalf("open questions = %v; want exactly one", ids)
	}
	return ids[0], f.questions[ids[0]]
}

// answer records an answer to the open question, the way the store does: the
// question moves to answered and the answer becomes its current answer.
func (f *fakeReleaseQuestions) answer(t *testing.T, option string) string {
	t.Helper()
	id, _ := f.open(t)
	f.mu.Lock()
	defer f.mu.Unlock()
	q := f.questions[id]
	q.State = autonomous.QuestionAnswered
	f.questions[id] = q
	f.answers[id] = append(f.answers[id], autonomous.AnswerView{
		Answer: autonomous.Answer{ID: "a-" + id, QuestionID: id, ChosenOptionID: option},
	})
	return id
}

// withdraw marks the open question withdrawn, as the store does when the
// operator or the system takes a question back.
func (f *fakeReleaseQuestions) withdraw(t *testing.T) string {
	t.Helper()
	id, _ := f.open(t)
	f.mu.Lock()
	defer f.mu.Unlock()
	q := f.questions[id]
	q.State = autonomous.QuestionWithdrawn
	f.questions[id] = q
	return id
}

// raisedCount is how many questions were actually raised (not found).
func (f *fakeReleaseQuestions) raisedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.raised
}

// TestReleaseApprovalEffectiveModePerSizeDefaults: with no settings, patch and
// minor ship automatically and major asks, through the config the daemon uses.
func TestReleaseApprovalEffectiveModePerSizeDefaults(t *testing.T) {
	var cfg config.MergePipelineConfig
	want := map[string]string{
		config.ReleaseSizePatch: config.ReleaseApprovalAuto,
		config.ReleaseSizeMinor: config.ReleaseApprovalAuto,
		config.ReleaseSizeMajor: config.ReleaseApprovalAsk,
	}
	for size, mode := range want {
		if got := cfg.EffectiveReleaseApproval("p", size); got != mode {
			t.Errorf("default %s = %q; want %q", size, got, mode)
		}
	}
	if got := cfg.EffectiveReleaseApproval("p", ""); got != config.ReleaseApprovalAsk {
		t.Errorf("unknown size = %q; want ask", got)
	}
}

// TestReleaseApprovalEffectiveModePerSizeFromGlobalConfig: a global value for a
// size replaces that size's default and leaves the others alone.
func TestReleaseApprovalEffectiveModePerSizeFromGlobalConfig(t *testing.T) {
	cfg := config.MergePipelineConfig{ReleaseApproval: config.ReleaseApprovalModes{
		Patch: config.ReleaseApprovalManual,
		Major: config.ReleaseApprovalAuto,
	}}
	cases := map[string]string{
		config.ReleaseSizePatch: config.ReleaseApprovalManual,
		config.ReleaseSizeMinor: config.ReleaseApprovalAuto,
		config.ReleaseSizeMajor: config.ReleaseApprovalAuto,
	}
	for size, mode := range cases {
		if got := cfg.EffectiveReleaseApproval("p", size); got != mode {
			t.Errorf("global %s = %q; want %q", size, got, mode)
		}
	}
}
