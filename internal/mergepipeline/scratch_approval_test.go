package mergepipeline

// A held release is rebuilt by a later run, so the operator's answer has to be
// found again. These use the daemon's real question store.

import (
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func askRun(t *testing.T, s sandbox, store *autonomous.QuestionStore) RunResult {
	t.Helper()
	r := s.runner(t, noTagSteps())
	r.Questions, r.ReleaseAnswers = store, store
	r.ReleaseSize, r.ReleaseApproval = "major", "ask"
	r.Selection = &fixedSelection{tip: runGit(t, s.dir, "rev-parse", "feature")}
	res, err := mergeFeature(t, r)
	if err != nil {
		t.Fatalf("run: %v %+v", err, res.Run.Steps)
	}
	return res
}

func releaseQuestions(store *autonomous.QuestionStore) []autonomous.Question {
	var out []autonomous.Question
	for _, q := range store.List(autonomous.QuestionFilter{}) {
		if q.AskedBy == ReleaseQuestionAsker {
			out = append(out, q)
		}
	}
	return out
}

func TestIsolatedHeldReleaseShipsOnceApproved(t *testing.T) {
	for name, c := range map[string]struct {
		choice string
		ships  bool
	}{"yes": {releaseOptionShip, true}, "no": {releaseOptionHold, false}} {
		t.Run(name, func(t *testing.T) {
			s := newSandbox(t)
			store, err := autonomous.NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			if askRun(t, s, store).Pushed {
				t.Fatal("an unanswered release shipped")
			}
			qs := releaseQuestions(store)
			if len(qs) != 1 {
				t.Fatalf("questions = %d", len(qs))
			}
			if _, err := store.Answer(qs[0].ID, autonomous.Answer{ChosenOptionID: c.choice, Actor: "operator", Surface: "test"}); err != nil {
				t.Fatal(err)
			}
			res := askRun(t, s, store)
			if res.Pushed != c.ships {
				t.Errorf("after %s: pushed=%v, want %v", name, res.Pushed, c.ships)
			}
			if got := len(releaseQuestions(store)); got != 1 {
				t.Errorf("questions after the answer = %d, want the one already answered", got)
			}
		})
	}
}

// An approval covers the commit it was asked about, not later work on the
// same branch.
func TestIsolatedReleaseApprovalDoesNotCoverNewCommits(t *testing.T) {
	s := newSandbox(t)
	store, err := autonomous.NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	askRun(t, s, store)
	if _, err := store.Answer(releaseQuestions(store)[0].ID, autonomous.Answer{ChosenOptionID: releaseOptionShip, Actor: "operator", Surface: "test"}); err != nil {
		t.Fatal(err)
	}
	runGit(t, s.dir, "checkout", "-q", "feature")
	putFile(t, s.dir, "more.txt", "added after the approval\n")
	runGit(t, s.dir, "add", "-A")
	runGit(t, s.dir, "commit", "-q", "-m", "more work")
	runGit(t, s.dir, "checkout", "-q", "main")
	if askRun(t, s, store).Pushed {
		t.Error("a release approved for the old commit shipped the new one")
	}
	if got := len(releaseQuestions(store)); got != 2 {
		t.Errorf("questions = %d, want a new one for the new commit", got)
	}
}
