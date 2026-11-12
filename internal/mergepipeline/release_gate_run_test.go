package mergepipeline

// Tests for the release gate wired into a run: DecideRelease decides (or the
// gate holds, in any mode, when a check failed), the gate applies to every
// step that leaves the machine, and the run's record says in plain words what
// happened — including the operator question id when one was raised or found.

import (
	"context"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// gatePipeline is the smallest pipeline the gate has something to gate: one
// check, then the tag and push steps that leave the machine.
func gatePipeline(failingCheck, failingCheckContinues bool) Definition {
	onFailure := OnFailureStop
	if failingCheckContinues {
		onFailure = OnFailureContinue
	}
	cmd := "true"
	if failingCheck {
		cmd = "false"
	}
	return Definition{Steps: []Step{
		{Name: "tests", Kind: KindTest, Command: cmd, OnFailure: onFailure},
		{Name: "tag", Kind: KindTag, Action: ActionTagRelease},
		{Name: "push", Kind: KindShip, Action: ActionPushRelease},
	}}
}

func newGateRun(t *testing.T, dir string, def Definition, ship *fakeShip) *Runner {
	t.Helper()
	return &Runner{
		Dir:          dir,
		Project:      "p",
		Resolver:     resolverWithRecord(t, def),
		Ship:         ship,
		VersionFiles: testVersionFiles,
	}
}

func shipCalls(ship *fakeShip) []string {
	ship.mu.Lock()
	defer ship.mu.Unlock()
	return append([]string(nil), ship.calls...)
}

func stepDetail(run autonomous.MergePipelineRun, step string) string {
	for _, s := range run.Steps {
		if s.Step == step {
			return s.Detail
		}
	}
	return ""
}

func stepOutcome(run autonomous.MergePipelineRun, step string) string {
	for _, s := range run.Steps {
		if s.Step == step {
			return s.Result
		}
	}
	return ""
}

// gateRun runs one pipeline run through r and returns its result.

// not wired: no approval mode, so the run ships exactly as before the gate.
func TestGateNotWiredShipsAsToday(t *testing.T) {
	ship := &fakeShip{}
	r := newGateRun(t, newMergeRepo(t), gatePipeline(false, false), ship)
	res, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if stepOutcome(res.Run, "tag") != autonomous.MergeStepOK || stepOutcome(res.Run, "push") != autonomous.MergeStepOK {
		t.Fatalf("want tag and push ok, got %q and %q", stepOutcome(res.Run, "tag"), stepOutcome(res.Run, "push"))
	}
	calls := shipCalls(ship)
	if len(calls) != 2 {
		t.Fatalf("ship calls = %q; want tag and push", calls)
	}
}

// auto with passing checks ships as today, and never asks a question.
func TestGateAutoShipsWhenChecksPass(t *testing.T) {
	ship := &fakeShip{}
	r := newGateRun(t, newMergeRepo(t), gatePipeline(false, false), ship)
	r.ReleaseApproval = config.ReleaseApprovalAuto
	r.ReleaseSize = config.ReleaseSizePatch
	qs := newQuestions(t)
	r.Questions = qs
	res, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if stepOutcome(res.Run, "tag") != autonomous.MergeStepOK || stepOutcome(res.Run, "push") != autonomous.MergeStepOK {
		t.Fatalf("want tag and push ok, got %q and %q", stepOutcome(res.Run, "tag"), stepOutcome(res.Run, "push"))
	}
	if stepDetail(res.Run, "tag") != "tagged v1.2.3" {
		t.Fatalf("tag detail = %q; want the plain ship note, no gate wording", stepDetail(res.Run, "tag"))
	}
	if open := openQuestionsBy(t, qs, ReleaseQuestionAsker); len(open) != 0 {
		t.Fatalf("auto mode must not ask; open questions = %+v", open)
	}
	if calls := shipCalls(ship); len(calls) != 2 {
		t.Fatalf("ship calls = %q; want tag and push", calls)
	}
}

// manual prepares the release and leaves the ship to a person: the leaving
// steps are held, the run says so in plain words, and nothing ships.
func TestGateManualPreparesAndLeavesTheShipToTheOperator(t *testing.T) {
	ship := &fakeShip{}
	qs := newQuestions(t)
	r := newGateRun(t, newMergeRepo(t), gatePipeline(false, false), ship)
	r.ReleaseApproval = config.ReleaseApprovalManual
	// size intentionally not read: the record must say so rather than guess
	r.Questions = qs
	res, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Run.Result != autonomous.MergeRunOK {
		t.Fatalf("run result = %q; the run holds the ship, it does not fail", res.Run.Result)
	}
	for _, step := range []string{"tag", "push"} {
		got := stepOutcome(res.Run, step)
		if got != autonomous.MergeStepSkipped {
			t.Fatalf("%s result = %q; want skipped", step, got)
		}
		note := stepDetail(res.Run, step)
		if !strings.Contains(note, "release gate (mode manual, size not read): prepared; a person ships this release") {
			t.Fatalf("%s detail = %q; want the mode, the unread size and why it waits", step, note)
		}
	}
	if open := openQuestionsBy(t, qs, ReleaseQuestionAsker); len(open) != 0 {
		t.Fatalf("manual mode must not ask; open questions = %+v", open)
	}
	if calls := shipCalls(ship); len(calls) != 0 {
		t.Fatalf("ship calls = %q; want none", calls)
	}
}

// ask raises the one decision question and holds; a re-run finds the open
// question rather than raising a second one, and still holds.
func TestGateAskRaisesOnceThenFindsTheOpenQuestion(t *testing.T) {
	dir := newMergeRepo(t)
	ship := &fakeShip{}
	qs := newQuestions(t)
	r := newGateRun(t, dir, gatePipeline(false, false), ship)
	r.ReleaseApproval = config.ReleaseApprovalAsk
	r.ReleaseSize = config.ReleaseSizePatch
	r.Questions = qs
	r.ReleaseAnswers = qs

	first, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err != nil {
		t.Fatalf("run 1: %v", err)
	}
	open := openQuestionsBy(t, qs, ReleaseQuestionAsker)
	if len(open) != 1 {
		t.Fatalf("open questions = %+v; want exactly one", open)
	}
	if !strings.Contains(open[0].Text, "Size: patch") || !strings.Contains(open[0].Text, "Release plan for p") {
		t.Fatalf("question text = %q; want the plan with size and project", open[0].Text)
	}
	wantNote := "(operator question " + open[0].ID + ")"
	for _, step := range []string{"tag", "push"} {
		if got := stepOutcome(first.Run, step); got != autonomous.MergeStepSkipped {
			t.Fatalf("run 1 %s = %q; want skipped", step, got)
		}
		if !strings.Contains(stepDetail(first.Run, step), wantNote) {
			t.Fatalf("run 1 %s detail = %q; want %q", step, stepDetail(first.Run, step), wantNote)
		}
	}
	if calls := shipCalls(ship); len(calls) != 0 {
		t.Fatalf("run 1 ship calls = %q; want none", calls)
	}

	// Re-run the same release: it must find the open question, not raise a
	// second one, and keep holding.
	second, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}
	open = openQuestionsBy(t, qs, ReleaseQuestionAsker)
	if len(open) != 1 {
		t.Fatalf("after re-run open questions = %+v; want still exactly one", open)
	}
	for _, step := range []string{"tag", "push"} {
		if got := stepOutcome(second.Run, step); got != autonomous.MergeStepSkipped {
			t.Fatalf("run 2 %s = %q; want skipped", step, got)
		}
		if !strings.Contains(stepDetail(second.Run, step), wantNote) {
			t.Fatalf("run 2 %s detail = %q; want the same question %q", step, stepDetail(second.Run, step), wantNote)
		}
	}
	if calls := shipCalls(ship); len(calls) != 0 {
		t.Fatalf("run 2 ship calls = %q; want none", calls)
	}
}

// fakeYesReader is a ReleaseAnswerReader that reports a stored "yes" for one
// question id only: the operator answered, and the answer is readable.
type fakeYesReader struct{ yesFor string }

func (f fakeYesReader) Get(id string) (autonomous.Question, bool) {
	return autonomous.Question{ID: id, State: autonomous.QuestionAnswered}, true
}

func (f fakeYesReader) Answers(questionID string) ([]autonomous.AnswerView, bool) {
	if questionID != f.yesFor {
		return []autonomous.AnswerView{}, true
	}
	return []autonomous.AnswerView{{Answer: autonomous.Answer{ChosenOptionID: releaseOptionShip}}}, true
}

// ask ships when the decision question is already answered yes: the leaving
// steps run, and the run's record says the operator approved it, with the
// question id.
func TestGateAskShipsWhenTheDecisionIsAlreadyYes(t *testing.T) {
	dir := newMergeRepo(t)
	ship := &fakeShip{}
	qs := newQuestions(t)
	r := newGateRun(t, dir, gatePipeline(false, false), ship)
	r.ReleaseApproval = config.ReleaseApprovalAsk
	r.ReleaseSize = config.ReleaseSizePatch
	r.Questions = qs
	// no reader yet: the first run asks
	first, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err != nil {
		t.Fatalf("run 1: %v", err)
	}
	open := openQuestionsBy(t, qs, ReleaseQuestionAsker)
	if len(open) != 1 {
		t.Fatalf("open questions = %+v; want exactly one", open)
	}
	if stepOutcome(first.Run, "tag") != autonomous.MergeStepSkipped {
		t.Fatalf("run 1 tag = %q; want skipped", stepOutcome(first.Run, "tag"))
	}
	qid := open[0].ID
	if calls := shipCalls(ship); len(calls) != 0 {
		t.Fatalf("run 1 ship calls = %q; want none", calls)
	}

	// The decision is now answered yes: the next run ships.
	r.ReleaseAnswers = fakeYesReader{yesFor: qid}
	second, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if got := stepOutcome(second.Run, "tag"); got != autonomous.MergeStepOK {
		t.Fatalf("run 2 tag = %q; want ok", got)
	}
	wantPrefix := "approved by the operator (operator question " + qid + "); "
	gotDetail := stepDetail(second.Run, "tag")
	if !strings.HasPrefix(gotDetail, wantPrefix) {
		t.Fatalf("run 2 tag detail = %q; want prefix %q", gotDetail, wantPrefix)
	}
	if stepOutcome(second.Run, "push") != autonomous.MergeStepOK {
		t.Fatalf("run 2 push = %q; want ok", stepOutcome(second.Run, "push"))
	}
	calls := shipCalls(ship)
	if len(calls) != 2 {
		t.Fatalf("ship calls = %q; want tag and push", calls)
	}
}

// ask with no question channel wired cannot ask, so it cannot ship guessing:
// the step fails and the run stops short of the machine.
func TestGateAskFailsWhenTheQuestionChannelIsMissing(t *testing.T) {
	ship := &fakeShip{}
	r := newGateRun(t, newMergeRepo(t), gatePipeline(false, false), ship)
	r.ReleaseApproval = config.ReleaseApprovalAsk
	r.ReleaseSize = config.ReleaseSizePatch
	// no Questions raiser
	_, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err == nil || !strings.Contains(err.Error(), "no operator question channel is wired") {
		t.Fatalf("err = %v; want the no-channel error", err)
	}
	if calls := shipCalls(ship); len(calls) != 0 {
		t.Fatalf("ship calls = %q; want none", calls)
	}
}

// A failed check never ships in any mode, and ask does not ask one either:
// a "yes" against failed checks could only be honoured by shipping them.
// The leaving steps are held, the record says why, and nothing ships.
func TestGateNeverShipsAFailedCheckInAnyMode(t *testing.T) {
	for _, mode := range []string{
		config.ReleaseApprovalAuto,
		config.ReleaseApprovalAsk,
		config.ReleaseApprovalManual,
	} {
		t.Run("mode_"+mode, func(t *testing.T) {
			ship := &fakeShip{}
			qs := newQuestions(t)
			r := newGateRun(t, newMergeRepo(t), gatePipeline(true, true), ship)
			r.ReleaseApproval = mode
			r.ReleaseSize = config.ReleaseSizePatch
			r.Questions = qs
			r.ReleaseAnswers = qs

			res, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if res.Run.Result != autonomous.MergeRunCompletedWithFailures {
				t.Fatalf("run result = %q; want completed with failures", res.Run.Result)
			}
			if got := stepOutcome(res.Run, "tests"); got != autonomous.MergeStepFailed {
				t.Fatalf("tests result = %q; want failed", got)
			}
			want := "release gate (mode " + mode + ", size patch): the checks failed; a person needs to look before this ships"
			for _, step := range []string{"tag", "push"} {
				if got := stepOutcome(res.Run, step); got != autonomous.MergeStepSkipped {
					t.Fatalf("%s result = %q; want skipped", step, got)
				}
				if note := stepDetail(res.Run, step); !strings.Contains(note, want) {
					t.Fatalf("%s detail = %q; want the failed-check hold note", step, note)
				}
			}
			if open := openQuestionsBy(t, qs, ReleaseQuestionAsker); len(open) != 0 {
				t.Fatalf("mode %s with a failed check must not ask; open = %+v", mode, open)
			}
			if calls := shipCalls(ship); len(calls) != 0 {
				t.Fatalf("mode %s: ship calls = %q; want none", mode, calls)
			}
		})
	}
}

// A gate wired but told no size still works: it gates on the mode and says
// the size was not read, rather than guessing one. (Covered in the manual
// test above; this pins the ask side.)
func TestGateAskWithUnreadSizeHoldsSayingItWasNotRead(t *testing.T) {
	ship := &fakeShip{}
	qs := newQuestions(t)
	r := newGateRun(t, newMergeRepo(t), gatePipeline(false, false), ship)
	r.ReleaseApproval = config.ReleaseApprovalAsk
	// no ReleaseSize
	r.Questions = qs
	res, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	open := openQuestionsBy(t, qs, ReleaseQuestionAsker)
	if len(open) != 1 {
		t.Fatalf("open questions = %+v; want one", open)
	}
	if strings.Contains(open[0].Text, "Size: ") {
		t.Fatalf("question text = %q; with no size read, the question must not state one", open[0].Text)
	}
	if stepOutcome(res.Run, "tag") != autonomous.MergeStepSkipped || stepOutcome(res.Run, "push") != autonomous.MergeStepSkipped {
		t.Fatalf("tag/push = %q/%q; want both held", stepOutcome(res.Run, "tag"), stepOutcome(res.Run, "push"))
	}
	if steps := strings.Join(shipCalls(ship), ","); steps != "" {
		t.Fatalf("ship calls = %q; want none", steps)
	}
}
