package mergepipeline

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// withOrigin gives the merge repository a bare remote named origin that holds
// main and feature, so a run can fetch main and a test can move it.
func withOrigin(t *testing.T, dir string) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, filepath.Dir(bare), "init", "--bare", "-q", "-b", "main", bare)
	runGit(t, dir, "remote", "add", "origin", bare)
	runGit(t, dir, "push", "-q", "origin", "main", "feature")
	return bare
}

// landOnRemoteMain clones the bare remote elsewhere, sets the version files to
// version on main and pushes it, the way another machine would move main.
func landOnRemoteMain(t *testing.T, bare, version string) {
	t.Helper()
	other := filepath.Join(t.TempDir(), "other")
	runGit(t, filepath.Dir(other), "clone", "-q", bare, other)
	runGit(t, other, "config", "user.email", "t@example.invalid")
	runGit(t, other, "config", "user.name", "Test")
	runGit(t, other, "config", "commit.gpgsign", "false")
	for _, f := range testVersionFiles {
		putFile(t, other, f, "package x\n\nvar Version = \""+version+"\"\n")
	}
	runGit(t, other, "commit", "-q", "-am", "main moves to "+version)
	runGit(t, other, "push", "-q", "origin", "main")
}

// movingShip is a ShipRunner whose push finds the remote's main moved for the
// first moves calls. Each moved call runs onMove first, so the test can move
// the remote at the moment the push sees it.
type movingShip struct {
	fakeShip
	mu     sync.Mutex
	pushes int
	moves  int
	onMove func()
}

func (m *movingShip) Push(ctx context.Context, dir, branch, tag string) error {
	m.mu.Lock()
	m.pushes++
	moved := m.pushes <= m.moves
	m.mu.Unlock()
	if moved {
		if m.onMove != nil {
			m.onMove()
		}
		return ErrMainMoved
	}
	return m.fakeShip.Push(ctx, dir, branch, tag)
}

func (m *movingShip) pushCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pushes
}

// countLines counts the lines of text that equal want.
func countLines(text, want string) int {
	n := 0
	for _, l := range strings.Split(text, "\n") {
		if l == want {
			n++
		}
	}
	return n
}

func TestMainMovedRedoComputesVersionFromMovedMain(t *testing.T) {
	dir := newMergeRepo(t)
	bare := withOrigin(t, dir)
	rec := &memRecorder{}
	ship := &movingShip{moves: 1}
	ship.onMove = func() { landOnRemoteMain(t, bare, "1.4.0") }
	r := &Runner{Resolver: resolverWithRecord(t, definitionWithoutReleaseValidator()), VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: ship, Recorder: rec}

	res, err := r.Run(context.Background(), RunRequest{Title: "moved", Branches: []string{"feature"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Run.Result != autonomous.MergeRunOK {
		t.Fatalf("run result = %q, want ok", res.Run.Result)
	}
	if got := ship.pushCount(); got != 2 {
		t.Fatalf("push calls = %d, want 2 (one moved, one ok)", got)
	}
	assertBothVersions(t, dir, "1.4.1")
	if got := res.Run.ReleaseVersion; got != "1.4.1" {
		t.Fatalf("run ReleaseVersion = %q, want 1.4.1", got)
	}
	changelog := getFile(t, dir, "CHANGELOG.md")
	if n := strings.Count(changelog, "## v1.4.1 "); n != 1 {
		t.Fatalf("changelog has %d v1.4.1 headings, want 1:\n%s", n, changelog)
	}
	subjects := runGit(t, dir, "log", "--format=%s", "main")
	if n := countLines(subjects, "chore(release): v1.4.1 bump for merged branches"); n != 1 {
		t.Fatalf("v1.4.1 bump commits = %d, want 1:\n%s", n, subjects)
	}
	if n := countLines(subjects, "chore(release): v1.2.4 bump for merged branches"); n != 0 {
		t.Fatalf("a bump commit from the discarded attempt survived:\n%s", subjects)
	}
	if out := runGit(t, dir, "tag", "--list", "v1.2.4"); out != "" {
		t.Fatalf("the discarded attempt's tag survived: %q", out)
	}
	if !strings.Contains(getFile(t, dir, "feature.txt"), "new") {
		t.Fatal("the feature commit is missing after the redo")
	}
	steps := stepResults(res.Run)
	if steps["main-moved-redo-1"] != autonomous.MergeStepOK {
		t.Fatalf("redo step result = %q, want ok; steps = %v", steps["main-moved-redo-1"], steps)
	}
	found := false
	for _, s := range res.Run.Steps {
		if s.Step == "main-moved-redo-1" && strings.Contains(s.Detail, "starts again from the rebase step") {
			found = true
		}
	}
	if !found {
		t.Fatal("the redo was not recorded as a run step note")
	}
}

func TestMainMovedRedoFailsAfterThreeRedos(t *testing.T) {
	dir := newMergeRepo(t)
	withOrigin(t, dir)
	rec := &memRecorder{}
	ship := &movingShip{moves: 100}
	r := &Runner{Resolver: resolverWithRecord(t, definitionWithoutReleaseValidator()), VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: ship, Recorder: rec}

	res, err := r.Run(context.Background(), RunRequest{Title: "never settles", Branches: []string{"feature"}})
	if err == nil {
		t.Fatal("run succeeded, want a failure once main kept moving")
	}
	if !errors.Is(err, ErrMainMoved) {
		t.Fatalf("error %v does not wrap ErrMainMoved", err)
	}
	if !strings.Contains(err.Error(), "main kept moving") {
		t.Fatalf("error %q does not say main kept moving", err)
	}
	if res.Run.Result != autonomous.MergeRunFailed {
		t.Fatalf("run result = %q, want failed", res.Run.Result)
	}
	if !strings.Contains(res.Run.Reason, "main kept moving") {
		t.Fatalf("run reason %q does not say main kept moving", res.Run.Reason)
	}
	if got := ship.pushCount(); got != maxMainMovedRedos+1 {
		t.Fatalf("push calls = %d, want %d", got, maxMainMovedRedos+1)
	}
	steps := stepResults(res.Run)
	for i := 1; i <= maxMainMovedRedos; i++ {
		name := fmt.Sprintf("main-moved-redo-%d", i)
		if steps[name] != autonomous.MergeStepOK {
			t.Fatalf("%s result = %q, want ok; steps = %v", name, steps[name], steps)
		}
	}
	if _, ok := steps["main-moved-redo-4"]; ok {
		t.Fatal("a fourth redo ran; the limit is three")
	}
	if n := countLines(runGit(t, dir, "log", "--format=%s", "main"), "chore(release): v1.2.4 bump for merged branches"); n > 1 {
		t.Fatalf("bump commits on main = %d, want at most 1 (each redo resets the last)", n)
	}
}

// newTrackedRunState returns a run state for dir with main's head recorded,
// as a run has after it takes the lock.
func newTrackedRunState(t *testing.T, dir string) *runState {
	t.Helper()
	r := &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: &fakeShip{}}
	s := &runState{r: r, m: &Merger{Dir: dir}, run: autonomous.MergePipelineRun{ID: "run-test"}}
	if err := s.startTracking(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestUndoableRefusesCommitTheRunDidNotMake(t *testing.T) {
	dir := newMergeRepo(t)
	s := newTrackedRunState(t, dir)
	putFile(t, dir, "operator.txt", "x\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "operator commit")
	err := s.undoable(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not made by this run") {
		t.Fatalf("undoable = %v, want a refusal naming the foreign commit", err)
	}
}

func TestObserveMainMarksMainMovedOutsideTheRun(t *testing.T) {
	dir := newMergeRepo(t)
	s := newTrackedRunState(t, dir)
	// feature is not an ancestor of main, so main no longer extends what the
	// run last saw.
	s.observedMain = runGit(t, dir, "rev-parse", "feature")
	if err := s.observeMain(context.Background()); err != nil {
		t.Fatalf("observeMain: %v", err)
	}
	if !s.foreignMain {
		t.Fatal("foreignMain = false, want true after main moved outside the run")
	}
	if err := s.undoable(context.Background()); err == nil || !strings.Contains(err.Error(), "something other than this run") {
		t.Fatalf("undoable = %v, want a refusal for a foreign move", err)
	}
}

func TestRedoWithoutARemoteFailsAndDeletesTheRunsTag(t *testing.T) {
	dir := newMergeRepo(t)
	s := newTrackedRunState(t, dir)
	runGit(t, dir, "tag", "v9.9.9")
	s.tagMade = "v9.9.9"
	rec := &memRecorder{}
	s.r.Recorder = rec
	err := s.recordRedo(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "no remote") {
		t.Fatalf("recordRedo = %v, want a no-remote failure", err)
	}
	if out := runGit(t, dir, "tag", "--list", "v9.9.9"); out != "" {
		t.Fatalf("the run's tag survived the redo: %q", out)
	}
	if s.tagMade != "" {
		t.Fatalf("tagMade = %q after deleting the tag, want empty", s.tagMade)
	}
	if len(s.run.Steps) != 1 || s.run.Steps[0].Step != "main-moved-redo-1" || s.run.Steps[0].Result != autonomous.MergeStepFailed {
		t.Fatalf("steps = %+v, want one failed redo note", s.run.Steps)
	}
}
