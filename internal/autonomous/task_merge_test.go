package autonomous

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// mergeFixture is a PRD worktree in a real temporary git repo, a manager on
// the given conflict policy, and a helper that gives a task its own worktree
// and branch. Every task is completed and saved in the manager's store.
type mergeFixture struct {
	t    *testing.T
	repo string
	wt   string
	prd  *PRD
	m    *Manager
}

func newMergeFixture(t *testing.T, policy string, base map[string]string) *mergeFixture {
	t.Helper()
	repo, wtDir, prd := prdWorktreeFixture(t)
	m := newTaskWorktreeManager(t, repo, wtDir, true)
	m.mu.Lock()
	m.cfg.Parallel.MergeConflictPolicy = policy
	m.mu.Unlock()
	for name, body := range base {
		writeMergeFile(t, prd.ProjectDir, name, body)
		gitRun(t, prd.ProjectDir, "add", name)
	}
	if len(base) > 0 {
		gitRun(t, prd.ProjectDir, "commit", "-q", "-m", "base files")
	}
	f := &mergeFixture{t: t, repo: repo, wt: wtDir, prd: prd, m: m}
	return f
}

func writeMergeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// addTask appends a completed task whose branch carries one commit writing
// file=body. Tasks are added before the PRD is stored, so call seal after.
func (f *mergeFixture) addTask(id string, deps []string, file, body string) {
	f.t.Helper()
	task := Task{ID: id, PRDID: f.prd.ID, Title: "task " + id, Status: TaskCompleted, DependsOn: deps}
	path, branch, err := EnsureTaskWorktree(f.prd, &task, f.repo, f.wt)
	if err != nil {
		f.t.Fatal(err)
	}
	task.WorktreePath, task.WorktreeBranch = path, branch
	writeMergeFile(f.t, path, file, body)
	gitRun(f.t, path, "add", file)
	gitRun(f.t, path, "commit", "-q", "-m", "work of "+id)
	if len(f.prd.Story) == 0 {
		f.prd.Story = []Story{{ID: "s1"}}
	}
	f.prd.Story[0].Tasks = append(f.prd.Story[0].Tasks, task)
}

func (f *mergeFixture) seal() {
	f.t.Helper()
	if err := f.m.store.SavePRD(f.prd); err != nil {
		f.t.Fatal(err)
	}
}

func (f *mergeFixture) task(id string) *Task {
	for i := range f.prd.Story[0].Tasks {
		if f.prd.Story[0].Tasks[i].ID == id {
			return &f.prd.Story[0].Tasks[i]
		}
	}
	f.t.Fatalf("no task %s", id)
	return nil
}

func (f *mergeFixture) readPRDFile(name string) string {
	b, err := os.ReadFile(filepath.Join(f.prd.ProjectDir, name))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(b)
}

// assertCleanTree fails when the PRD worktree has a merge in progress or
// uncommitted changes: a refused conflict must leave it as it found it.
func (f *mergeFixture) assertCleanTree() {
	f.t.Helper()
	if st := gitOut(f.t, f.prd.ProjectDir, "status", "--porcelain", "--untracked-files=no"); st != "" {
		f.t.Fatalf("PRD worktree not clean: %s", st)
	}
	if _, err := os.Stat(filepath.Join(gitOut(f.t, f.prd.ProjectDir, "rev-parse", "--git-dir"), "MERGE_HEAD")); err == nil {
		f.t.Fatal("merge left in progress")
	}
}

const mergeBase = "l1\nl2\nl3\n"

func TestMergeTaskBranchClean(t *testing.T) {
	f := newMergeFixture(t, "", nil)
	f.addTask("t1", nil, "a.txt", "from a\n")
	f.seal()
	out, err := f.m.MergeTaskBranch(context.Background(), f.prd, f.task("t1"))
	if err != nil || out != MergeOutcomeMerged {
		t.Fatalf("outcome %s err %v", out, err)
	}
	tk := f.task("t1")
	if tk.MergeState != MergeStateMerged {
		t.Fatalf("merge_state = %q", tk.MergeState)
	}
	if head := gitOut(t, f.prd.ProjectDir, "rev-parse", "HEAD"); tk.MergeCommit != head {
		t.Fatalf("merge_commit %q, PRD head %q", tk.MergeCommit, head)
	}
	if f.readPRDFile("a.txt") != "from a\n" {
		t.Fatal("task work not on the PRD branch")
	}
	// Merging again is a no-op that reports the same state.
	if out, err := f.m.MergeTaskBranch(context.Background(), f.prd, tk); err != nil || out != MergeOutcomeMerged {
		t.Fatalf("second merge: %s %v", out, err)
	}
}

func TestMergeTaskBranchSkipsTaskWithoutBranch(t *testing.T) {
	f := newMergeFixture(t, "", nil)
	task := &Task{ID: "plain", PRDID: f.prd.ID, Status: TaskCompleted}
	out, err := f.m.MergeTaskBranch(context.Background(), f.prd, task)
	if err != nil || out != MergeOutcomeSkipped || task.MergeState != "" {
		t.Fatalf("outcome %s err %v state %q", out, err, task.MergeState)
	}
}

func TestMergeOrderFollowsDependenciesThenTaskOrder(t *testing.T) {
	f := newMergeFixture(t, "", nil)
	f.addTask("t1", nil, "a.txt", "a\n")
	f.addTask("t2", []string{"t1"}, "b.txt", "b\n")
	f.addTask("t3", nil, "c.txt", "c\n")
	f.addTask("t4", nil, "d.txt", "d\n")
	f.seal()
	ctx := context.Background()

	// A dependent cannot jump its dependency; a later sibling cannot jump an
	// earlier one that is waiting.
	if out, err := f.m.MergeTaskBranch(ctx, f.prd, f.task("t2")); err != nil || out != MergeOutcomeDeferred {
		t.Fatalf("t2 before t1: %s %v", out, err)
	}
	if got := f.task("t2").MergeState; got != MergeStatePending {
		t.Fatalf("deferred task state = %q", got)
	}
	if out, err := f.m.MergeTaskBranch(ctx, f.prd, f.task("t4")); err != nil || out != MergeOutcomeDeferred {
		t.Fatalf("t4 before t1..t3: %s %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(f.prd.ProjectDir, "b.txt")); err == nil {
		t.Fatal("a deferred task's work reached the PRD branch")
	}

	n, err := f.m.MergeCompletedTasks(ctx, f.prd)
	if err != nil || n != 4 {
		t.Fatalf("merged %d err %v", n, err)
	}
	log := gitOut(t, f.prd.ProjectDir, "log", "--merges", "--reverse", "--format=%s")
	var got []string
	for _, l := range strings.Split(log, "\n") {
		got = append(got, strings.Fields(l)[2]) // "Merge task <id>: ..."
	}
	if strings.Join(got, ",") != "t1:,t2:,t3:,t4:" {
		t.Fatalf("merge order = %v", got)
	}
}

func TestMergeAdditiveConflictResolvedMechanically(t *testing.T) {
	f := newMergeFixture(t, "", map[string]string{"shared.txt": mergeBase})
	f.addTask("t1", nil, "shared.txt", "l1\nfrom-a\nl2\nl3\n")
	f.addTask("t2", nil, "shared.txt", "l1\nfrom-b\nl2\nl3\n")
	f.seal()
	ctx := context.Background()
	for _, id := range []string{"t1", "t2"} {
		out, err := f.m.MergeTaskBranch(ctx, f.prd, f.task(id))
		if err != nil || out != MergeOutcomeMerged {
			t.Fatalf("%s: %s %v", id, out, err)
		}
	}
	got := f.readPRDFile("shared.txt")
	for _, want := range []string{"from-a", "from-b", "l1", "l2", "l3"} {
		if !strings.Contains(got, want) {
			t.Fatalf("resolved file lost %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "<<<<<<<") {
		t.Fatalf("conflict markers left:\n%s", got)
	}
	f.assertCleanTree()
	if f.task("t2").MergeCommit == "" || f.task("t2").MergeState != MergeStateMerged {
		t.Fatalf("t2 not recorded as merged: %+v", f.task("t2"))
	}
}

// nonAdditive makes both tasks change line 2 differently.
func nonAdditive(f *mergeFixture) {
	f.addTask("t1", nil, "shared.txt", "l1\nA wins\nl3\n")
	f.addTask("t2", nil, "shared.txt", "l1\nB wins\nl3\n")
	f.seal()
}

func TestMergeNonAdditiveConflictHeldWithoutModel(t *testing.T) {
	f := newMergeFixture(t, "", map[string]string{"shared.txt": mergeBase})
	nonAdditive(f)
	ctx := context.Background()
	if out, err := f.m.MergeTaskBranch(ctx, f.prd, f.task("t1")); err != nil || out != MergeOutcomeMerged {
		t.Fatalf("t1: %s %v", out, err)
	}
	head := gitOut(t, f.prd.ProjectDir, "rev-parse", "HEAD")
	out, err := f.m.MergeTaskBranch(ctx, f.prd, f.task("t2"))
	if err != nil || out != MergeOutcomeHeld {
		t.Fatalf("t2: %s %v", out, err)
	}
	tk := f.task("t2")
	if tk.MergeState != MergeStateHeld || tk.Status != TaskReviewNeeded {
		t.Fatalf("state %q status %q; want held / review_needed (the held-question path)", tk.MergeState, tk.Status)
	}
	if tk.ReviewSummary == nil || !strings.Contains(tk.ReviewSummary.Text, "shared.txt") {
		t.Fatalf("held task does not name the file: %+v", tk.ReviewSummary)
	}
	if gitOut(t, f.prd.ProjectDir, "rev-parse", "HEAD") != head {
		t.Fatal("PRD branch moved by a held merge")
	}
	f.assertCleanTree()
	// The task branch and its work are untouched.
	if gitOut(t, tk.WorktreePath, "show", "HEAD:shared.txt") != "l1\nB wins\nl3" {
		t.Fatal("task branch work changed")
	}
}

func TestMergeNonAdditiveConflictResolvedByModelWhenVerified(t *testing.T) {
	f := newMergeFixture(t, "", map[string]string{"shared.txt": mergeBase})
	nonAdditive(f)
	var gotReq MergeResolveRequest
	f.m.SetMergeResolver(
		func(_ context.Context, req MergeResolveRequest) (map[string]string, error) {
			gotReq = req
			return map[string]string{"shared.txt": "l1\nA and B\nl3\n"}, nil
		},
		func(context.Context, *PRD, *Task, string) error { return nil },
	)
	ctx := context.Background()
	if _, err := f.m.MergeTaskBranch(ctx, f.prd, f.task("t1")); err != nil {
		t.Fatal(err)
	}
	out, err := f.m.MergeTaskBranch(ctx, f.prd, f.task("t2"))
	if err != nil || out != MergeOutcomeMerged {
		t.Fatalf("t2: %s %v", out, err)
	}
	if len(gotReq.Files) != 1 || gotReq.Files[0].Path != "shared.txt" || !strings.Contains(gotReq.Files[0].Conflicted, "<<<<<<<") {
		t.Fatalf("resolver request = %+v", gotReq.Files)
	}
	if f.readPRDFile("shared.txt") != "l1\nA and B\nl3\n" {
		t.Fatal("model resolution not applied")
	}
	f.assertCleanTree()
}

func TestMergeModelResolutionRejectedWhenVerificationFails(t *testing.T) {
	f := newMergeFixture(t, "", map[string]string{"shared.txt": mergeBase})
	nonAdditive(f)
	f.m.SetMergeResolver(
		func(context.Context, MergeResolveRequest) (map[string]string, error) {
			return map[string]string{"shared.txt": "l1\nbroken\nl3\n"}, nil
		},
		func(context.Context, *PRD, *Task, string) error { return errors.New("tests failed") },
	)
	ctx := context.Background()
	if _, err := f.m.MergeTaskBranch(ctx, f.prd, f.task("t1")); err != nil {
		t.Fatal(err)
	}
	head := gitOut(t, f.prd.ProjectDir, "rev-parse", "HEAD")
	out, err := f.m.MergeTaskBranch(ctx, f.prd, f.task("t2"))
	if err != nil || out != MergeOutcomeHeld {
		t.Fatalf("t2: %s %v", out, err)
	}
	if !strings.Contains(f.task("t2").Error, "verification") {
		t.Fatalf("hold reason = %q", f.task("t2").Error)
	}
	if gitOut(t, f.prd.ProjectDir, "rev-parse", "HEAD") != head || f.readPRDFile("shared.txt") != "l1\nA wins\nl3\n" {
		t.Fatal("rejected resolution leaked into the PRD branch")
	}
	f.assertCleanTree()
}

func TestMergeConflictPolicyBlockHoldsAutomaton(t *testing.T) {
	f := newMergeFixture(t, config.MergeConflictPolicyBlock, map[string]string{"shared.txt": mergeBase})
	nonAdditive(f)
	ctx := context.Background()
	if _, err := f.m.MergeTaskBranch(ctx, f.prd, f.task("t1")); err != nil {
		t.Fatal(err)
	}
	out, err := f.m.MergeTaskBranch(ctx, f.prd, f.task("t2"))
	if err != nil || out != MergeOutcomeConflict {
		t.Fatalf("t2: %s %v", out, err)
	}
	if f.prd.Status != PRDBlocked || !strings.Contains(f.prd.BlockedReason, "shared.txt") {
		t.Fatalf("PRD status %q reason %q", f.prd.Status, f.prd.BlockedReason)
	}
	if f.task("t2").MergeState != MergeStateConflict {
		t.Fatalf("merge_state = %q", f.task("t2").MergeState)
	}
	f.assertCleanTree()
}

func TestMergeConflictPolicyBlockDoesNotResolveAdditive(t *testing.T) {
	f := newMergeFixture(t, config.MergeConflictPolicyBlock, map[string]string{"shared.txt": mergeBase})
	f.addTask("t1", nil, "shared.txt", "l1\nfrom-a\nl2\nl3\n")
	f.addTask("t2", nil, "shared.txt", "l1\nfrom-b\nl2\nl3\n")
	f.seal()
	ctx := context.Background()
	_, _ = f.m.MergeTaskBranch(ctx, f.prd, f.task("t1"))
	if out, _ := f.m.MergeTaskBranch(ctx, f.prd, f.task("t2")); out != MergeOutcomeConflict {
		t.Fatalf("block policy resolved a conflict on its own: %s", out)
	}
}

func TestMergeConflictPolicyTriageSendsToTriage(t *testing.T) {
	f := newMergeFixture(t, config.MergeConflictPolicyTriage, map[string]string{"shared.txt": mergeBase})
	nonAdditive(f)
	ctx := context.Background()
	if _, err := f.m.MergeTaskBranch(ctx, f.prd, f.task("t1")); err != nil {
		t.Fatal(err)
	}
	out, err := f.m.MergeTaskBranch(ctx, f.prd, f.task("t2"))
	if err != nil || out != MergeOutcomeConflict {
		t.Fatalf("t2: %s %v", out, err)
	}
	tk := f.task("t2")
	if tk.Status != TaskFailed || tk.MergeState != MergeStateConflict || tk.FailureReason == nil {
		t.Fatalf("task = status %q state %q reason %v", tk.Status, tk.MergeState, tk.FailureReason)
	}
	if f.prd.Status != PRDFailed {
		t.Fatalf("PRD status = %q", f.prd.Status)
	}
	stored, ok := f.m.store.GetPRD(f.prd.ID)
	if !ok || stored.Triage == nil {
		t.Fatal("no triage record was saved for the Automaton")
	}
	if rec := f.prd.triageFailureTask(); rec == nil || rec.ID != "t2" {
		t.Fatalf("triage points at %v, want t2", rec)
	}
	f.assertCleanTree()
}

func TestMergeRefusesDirtyPRDWorktree(t *testing.T) {
	f := newMergeFixture(t, "", map[string]string{"shared.txt": mergeBase})
	f.addTask("t1", nil, "a.txt", "a\n")
	f.seal()
	writeMergeFile(t, f.prd.ProjectDir, "shared.txt", "uncommitted edit\n")
	out, err := f.m.MergeTaskBranch(context.Background(), f.prd, f.task("t1"))
	if err == nil || out == MergeOutcomeMerged {
		t.Fatalf("merged over uncommitted work: %s %v", out, err)
	}
	if f.readPRDFile("shared.txt") != "uncommitted edit\n" {
		t.Fatal("uncommitted work was touched")
	}
}
