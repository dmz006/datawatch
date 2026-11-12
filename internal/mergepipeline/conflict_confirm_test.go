package mergepipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// confirmRepo is conflictRepo with commit messages carrying tag, so two repos
// made in the same second do not share commit SHAs. The conflict is in cfg.txt
// and the model's proposal for it is overlapResolved (x=4).
func confirmRepo(t *testing.T, tag string) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.email", "test@example.com")
	gitIn(t, dir, "config", "user.name", "Test")
	gitIn(t, dir, "config", "commit.gpgsign", "false")
	writeFile(t, dir, "cfg.txt", "x=1\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "base "+tag)
	gitIn(t, dir, "checkout", "-q", "-b", "feature")
	writeFile(t, dir, "cfg.txt", "x=3\n")
	gitIn(t, dir, "commit", "-q", "-am", "feature "+tag)
	gitIn(t, dir, "checkout", "-q", "main")
	writeFile(t, dir, "cfg.txt", "x=2\n")
	gitIn(t, dir, "commit", "-q", "-am", "main "+tag)
	commitBuildTarget(t, dir)
	return dir
}

// confirmEnv is the counter, pending store and question store one scenario shares.
type confirmEnv struct {
	counter ConflictCounterStore
	pending *ConfirmStore
	qs      *autonomous.QuestionStore
}

func newConfirmEnv(t *testing.T) confirmEnv {
	t.Helper()
	dir := t.TempDir()
	c, err := NewConflictCounter(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewConfirmStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return confirmEnv{counter: c, pending: p, qs: newQuestions(t)}
}

// merger is a model-policy merger in mode with confirm count n for project "proj".
func (e confirmEnv) merger(dir, mode string, n int) *Merger {
	m := mergerInMode(dir, PolicyMechanicalAndModel, mode, &fakeModel{reply: overlapResolved}, e.qs, `grep -qx 'x=4' cfg.txt`)
	m.Settings = func() ConflictSettings {
		return ConflictSettings{Policy: PolicyMechanicalAndModel, Mode: mode, ConfirmFirstN: n}
	}
	m.Project = "proj"
	m.Counter = e.counter
	m.Pending = e.pending
	return m
}

func (e confirmEnv) count(t *testing.T) int {
	t.Helper()
	n, err := e.counter.Count("proj")
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func confirmMerge(t *testing.T, m *Merger) Outcome {
	t.Helper()
	o, err := m.MergeBranch(context.Background(), MergeRequest{Branch: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func answerQuestion(t *testing.T, qs *autonomous.QuestionStore, id, option string) {
	t.Helper()
	if _, err := qs.Answer(id, autonomous.Answer{ChosenOptionID: option, Actor: "operator", Surface: "test"}); err != nil {
		t.Fatal(err)
	}
}

func TestConflictConfirm(t *testing.T) {
	t.Run("the first N proposals ask, and an answered apply is applied", func(t *testing.T) {
		env := newConfirmEnv(t)
		const n = 2

		// Proposal 1: count 0 is below 2, so it is held and the operator asked.
		dir := confirmRepo(t, "one")
		head := gitIn(t, dir, "rev-parse", "HEAD")
		o := confirmMerge(t, env.merger(dir, ConflictModeOn, n))
		if o.Status != StatusAsked || o.Attempt == nil || o.Attempt.Result != AttemptAwaitingConfirmation {
			t.Fatalf("proposal 1: outcome = %+v attempt = %+v", o, o.Attempt)
		}
		if o.QuestionID == "" {
			t.Fatal("proposal 1: no confirmation question was raised")
		}
		assertAborted(t, dir, head, "cfg.txt", "x=2\n")
		if env.count(t) != 0 {
			t.Fatalf("held proposal was counted: %d", env.count(t))
		}

		// Operator confirms; the same branch at the same SHAs applies the stored proposal.
		answerQuestion(t, env.qs, o.QuestionID, ConfirmApplyProposal)
		o = confirmMerge(t, env.merger(dir, ConflictModeOn, n))
		if o.Status != StatusResolvedByModel || o.Attempt == nil || o.Attempt.Result != AttemptAccepted {
			t.Fatalf("confirmed apply: outcome = %+v attempt = %+v", o, o.Attempt)
		}
		if got := readFile(t, dir, "cfg.txt"); got != "x=4\n" {
			t.Fatalf("cfg.txt = %q, want the stored proposal", got)
		}
		if env.count(t) != 1 {
			t.Fatalf("count after one applied proposal = %d, want 1", env.count(t))
		}

		// Proposal 2: count 1 is still below 2, so it asks again.
		dir2 := confirmRepo(t, "two")
		head2 := gitIn(t, dir2, "rev-parse", "HEAD")
		o = confirmMerge(t, env.merger(dir2, ConflictModeOn, n))
		if o.Status != StatusAsked || o.Attempt == nil || o.Attempt.Result != AttemptAwaitingConfirmation {
			t.Fatalf("proposal 2: outcome = %+v attempt = %+v", o, o.Attempt)
		}
		assertAborted(t, dir2, head2, "cfg.txt", "x=2\n")
		answerQuestion(t, env.qs, o.QuestionID, ConfirmApplyProposal)
		o = confirmMerge(t, env.merger(dir2, ConflictModeOn, n))
		if o.Status != StatusResolvedByModel || env.count(t) != 2 {
			t.Fatalf("proposal 2 confirmed: status = %s, count = %d", o.Status, env.count(t))
		}

		// Proposal 3: count 2 has reached N, so it applies without asking.
		dir3 := confirmRepo(t, "three")
		o = confirmMerge(t, env.merger(dir3, ConflictModeOn, n))
		if o.Status != StatusResolvedByModel || o.QuestionID != "" {
			t.Fatalf("proposal 3: outcome = %+v, want applied with no question", o)
		}
		if open := openConflictQuestions(env.qs); len(open) != 0 {
			t.Fatalf("proposal 3 raised a question: %+v", open)
		}
		if env.count(t) != 3 {
			t.Fatalf("count = %d, want 3", env.count(t))
		}
	})

	t.Run("N of 0 applies without asking and still counts", func(t *testing.T) {
		env := newConfirmEnv(t)
		dir := confirmRepo(t, "zero")
		o := confirmMerge(t, env.merger(dir, ConflictModeOn, 0))
		if o.Status != StatusResolvedByModel {
			t.Fatalf("outcome = %+v attempt = %+v", o, o.Attempt)
		}
		if got := readFile(t, dir, "cfg.txt"); got != "x=4\n" {
			t.Fatalf("cfg.txt = %q", got)
		}
		if open := openConflictQuestions(env.qs); len(open) != 0 {
			t.Fatalf("N of 0 asked: %+v", open)
		}
		if env.count(t) != 1 {
			t.Fatalf("count = %d, want 1", env.count(t))
		}
	})

	t.Run("a count at N applies without asking", func(t *testing.T) {
		env := newConfirmEnv(t)
		const n = 2
		for i := 0; i < n; i++ {
			if _, err := env.counter.Increment("proj"); err != nil {
				t.Fatal(err)
			}
		}
		dir := confirmRepo(t, "at-n")
		o := confirmMerge(t, env.merger(dir, ConflictModeOn, n))
		if o.Status != StatusResolvedByModel {
			t.Fatalf("outcome = %+v attempt = %+v", o, o.Attempt)
		}
		if open := openConflictQuestions(env.qs); len(open) != 0 {
			t.Fatalf("a count at N asked: %+v", open)
		}
		if env.count(t) != n+1 {
			t.Fatalf("count = %d, want %d", env.count(t), n+1)
		}
	})

	t.Run("shadow never increments", func(t *testing.T) {
		env := newConfirmEnv(t)
		dir := confirmRepo(t, "shadow")
		head := gitIn(t, dir, "rev-parse", "HEAD")
		o := confirmMerge(t, env.merger(dir, "", 1))
		if o.Attempt == nil || o.Attempt.Result != AttemptShadowComputed {
			t.Fatalf("outcome = %+v attempt = %+v", o, o.Attempt)
		}
		assertAborted(t, dir, head, "cfg.txt", "x=2\n")
		if env.count(t) != 0 {
			t.Fatalf("shadow mode incremented the count to %d", env.count(t))
		}
	})

	t.Run("a changed branch SHA invalidates a pending proposal", func(t *testing.T) {
		env := newConfirmEnv(t)
		const n = 2
		dir := confirmRepo(t, "moved")
		head := gitIn(t, dir, "rev-parse", "HEAD")
		oldBranch := gitIn(t, dir, "rev-parse", "feature")
		o := confirmMerge(t, env.merger(dir, ConflictModeOn, n))
		if o.Attempt == nil || o.Attempt.Result != AttemptAwaitingConfirmation {
			t.Fatalf("outcome = %+v attempt = %+v", o, o.Attempt)
		}
		oldKey := confirmKey("proj", "feature", head, oldBranch)
		if _, ok := env.pending.Get(oldKey); !ok {
			t.Fatal("the proposal was not stored as pending")
		}
		answerQuestion(t, env.qs, o.QuestionID, ConfirmApplyProposal)

		// The branch moves: a new commit on feature changes its SHA and keeps the conflict.
		gitIn(t, dir, "checkout", "-q", "feature")
		writeFile(t, dir, "other.txt", "more\n")
		gitIn(t, dir, "add", "other.txt")
		gitIn(t, dir, "commit", "-q", "-m", "feature moved")
		gitIn(t, dir, "checkout", "-q", "main")
		newBranch := gitIn(t, dir, "rev-parse", "feature")
		if newBranch == oldBranch {
			t.Fatal("the branch SHA did not change")
		}

		o = confirmMerge(t, env.merger(dir, ConflictModeOn, n))
		if o.Status == StatusResolvedByModel {
			t.Fatal("a proposal made for the old branch SHA was applied")
		}
		if got := readFile(t, dir, "cfg.txt"); got != "x=2\n" {
			t.Fatalf("cfg.txt = %q, the stale proposal reached the project", got)
		}
		if env.count(t) != 0 {
			t.Fatalf("count = %d, want 0", env.count(t))
		}
		if _, ok := env.pending.Get(oldKey); ok {
			t.Fatal("the stale pending proposal was not dropped")
		}
		if _, ok := env.pending.Get(confirmKey("proj", "feature", head, newBranch)); !ok {
			t.Fatal("the new SHA was not asked about")
		}
	})

	t.Run("pending confirmations survive a reload from disk", func(t *testing.T) {
		dir := t.TempDir()
		s, err := NewConfirmStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		key := confirmKey("proj", "feature", "h1", "b1")
		want := PendingConfirmation{Project: "proj", Branch: "feature", HeadSHA: "h1", BranchSHA: "b1",
			Files: []string{"cfg.txt"}, Hunks: 1, Texts: map[string]string{"cfg.txt": "x=4\n"}, QuestionID: "q1"}
		if err := s.Put(key, want); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "merge-pipeline", "conflict-pending.json")); err != nil {
			t.Fatalf("pending file not written: %v", err)
		}
		again, err := NewConfirmStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := again.Get(key)
		if !ok || got.QuestionID != "q1" || got.Texts["cfg.txt"] != "x=4\n" {
			t.Fatalf("reloaded = %+v, %v", got, ok)
		}
	})
}
