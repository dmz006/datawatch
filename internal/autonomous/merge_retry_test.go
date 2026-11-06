package autonomous

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

func TestMergeRetryPassRetriesBlockedThenMerges(t *testing.T) {
	f := newMergeFixture(t, "", map[string]string{"shared.txt": mergeBase})
	f.addTask("t1", nil, "a.txt", "a\n")
	f.seal()
	ctx := context.Background()
	writeMergeFile(t, f.prd.ProjectDir, "shared.txt", "uncommitted edit\n")

	if n := f.m.MergeRetryPass(ctx); n != 0 {
		t.Fatalf("blocked pass merged %d", n)
	}
	tk := f.task("t1")
	if tk.MergeBlock == nil || tk.MergeBlock.LastRetryAt.IsZero() || tk.MergeState != MergeStatePending {
		t.Fatalf("task = state %q block %+v", tk.MergeState, tk.MergeBlock)
	}
	if _, err := runGitInCtx(ctx, f.prd.ProjectDir, "checkout", "--", "shared.txt"); err != nil {
		t.Fatal(err)
	}
	if n := f.m.MergeRetryPass(ctx); n != 1 {
		t.Fatalf("clean pass merged %d, want 1", n)
	}
	if f.task("t1").MergeState != MergeStateMerged {
		t.Fatalf("state %q", f.task("t1").MergeState)
	}
}

func TestMergeRetryPassSkipsCancelledAndArchived(t *testing.T) {
	for _, st := range []PRDStatus{PRDCancelled, PRDArchived} {
		f := newMergeFixture(t, "", nil)
		f.addTask("t1", nil, "a.txt", "a\n")
		f.prd.Status = st
		f.seal()
		if n := f.m.MergeRetryPass(context.Background()); n != 0 || f.task("t1").MergeState == MergeStateMerged {
			t.Fatalf("%s: merged %d", st, n)
		}
	}
}

// dirtyTree leaves an uncommitted edit to a tracked file in the PRD worktree,
// which blocks a merge; cleanTree puts it back.
func (f *mergeFixture) dirtyTree() {
	f.t.Helper()
	writeMergeFile(f.t, f.prd.ProjectDir, "shared.txt", "uncommitted edit\n")
}

func (f *mergeFixture) cleanTree() {
	f.t.Helper()
	if _, err := runGitInCtx(context.Background(), f.prd.ProjectDir, "checkout", "--", "shared.txt"); err != nil {
		f.t.Fatal(err)
	}
}

func TestMergeRetryPassMergesAfterTreeCleaned(t *testing.T) {
	f := newMergeFixture(t, "", map[string]string{"shared.txt": mergeBase})
	f.addTask("t1", nil, "a.txt", "a\n")
	f.seal()
	ctx := context.Background()
	f.dirtyTree()
	if n := f.m.MergeRetryPass(ctx); n != 0 {
		t.Fatalf("dirty pass merged %d", n)
	}
	if f.task("t1").MergeBlock == nil {
		t.Fatal("no merge block recorded for the dirty tree")
	}
	// No other task completes; only the tree is cleaned.
	f.cleanTree()
	if n := f.m.MergeRetryPass(ctx); n != 1 {
		t.Fatalf("clean pass merged %d, want 1", n)
	}
	tk := f.task("t1")
	if tk.MergeState != MergeStateMerged || tk.MergeBlock != nil {
		t.Fatalf("state %q block %+v", tk.MergeState, tk.MergeBlock)
	}
	if f.readPRDFile("a.txt") != "a\n" {
		t.Fatal("task work not on the PRD branch")
	}
}

func TestMergeRetryPassKeepsBlockWhileDirty(t *testing.T) {
	f := newMergeFixture(t, "", map[string]string{"shared.txt": mergeBase})
	f.addTask("t1", nil, "a.txt", "a\n")
	f.seal()
	ctx := context.Background()
	f.dirtyTree()

	f.m.MergeRetryPass(ctx)
	first := f.task("t1").MergeBlock
	if first == nil || first.LastRetryAt.IsZero() {
		t.Fatalf("first block = %+v", first)
	}
	a1, r1 := first.Attempts, first.LastRetryAt
	time.Sleep(5 * time.Millisecond)
	if n := f.m.MergeRetryPass(ctx); n != 0 {
		t.Fatalf("merged %d while dirty", n)
	}
	tk := f.task("t1")
	if tk.MergeBlock == nil || tk.MergeState == MergeStateMerged {
		t.Fatalf("block dropped: state %q block %+v", tk.MergeState, tk.MergeBlock)
	}
	if tk.MergeBlock.Attempts <= a1 {
		t.Fatalf("attempts %d, was %d", tk.MergeBlock.Attempts, a1)
	}
	if !tk.MergeBlock.LastRetryAt.After(r1) {
		t.Fatalf("LastRetryAt %v not after %v", tk.MergeBlock.LastRetryAt, r1)
	}
}

func TestMergeRetryPassOneAutomatonFailureDoesNotStopOthers(t *testing.T) {
	f := newMergeFixture(t, "", map[string]string{"shared.txt": mergeBase})
	f.addTask("t1", nil, "a.txt", "a\n")
	f.seal()

	// A second Automaton in the same repo, with a completed task of its own.
	prd2 := &PRD{ID: "efgh5678", Title: "Second"}
	path, branch, err := EnsureWorktree(prd2, f.repo, f.wt)
	if err != nil {
		t.Fatal(err)
	}
	prd2.ProjectDir, prd2.Git.Branch = path, branch
	task := Task{ID: "u1", PRDID: prd2.ID, Title: "u1", Status: TaskCompleted}
	tp, tb, err := EnsureTaskWorktree(prd2, &task, f.repo, f.wt)
	if err != nil {
		t.Fatal(err)
	}
	task.WorktreePath, task.WorktreeBranch = tp, tb
	writeMergeFile(t, tp, "b.txt", "b\n")
	gitRun(t, tp, "add", "b.txt")
	gitRun(t, tp, "commit", "-q", "-m", "work of u1")
	prd2.Story = []Story{{ID: "s1", Tasks: []Task{task}}}
	if err := f.m.store.SavePRD(prd2); err != nil {
		t.Fatal(err)
	}

	// The first Automaton's worktree vanishes, so its merge fails outright.
	if err := os.RemoveAll(f.prd.ProjectDir); err != nil {
		t.Fatal(err)
	}
	if n := f.m.MergeRetryPass(context.Background()); n != 1 {
		t.Fatalf("pass merged %d, want 1 (the healthy Automaton)", n)
	}
	got, ok := f.m.store.GetPRD(prd2.ID)
	if !ok || got.Story[0].Tasks[0].MergeState != MergeStateMerged {
		t.Fatalf("healthy Automaton not merged: %+v", got)
	}
}

// retryLoopManager returns a fixture whose blocked task would merge on the
// first pass, with the retry interval set to secs.
func retryLoopFixture(t *testing.T, secs int) *mergeFixture {
	t.Helper()
	f := newMergeFixture(t, "", nil)
	f.addTask("t1", nil, "a.txt", "a\n")
	f.seal()
	f.m.mu.Lock()
	f.m.cfg.Parallel.MergeRetrySeconds = &secs
	f.m.mu.Unlock()
	if err := config.ValidateMergeRetrySeconds(&secs); err != nil {
		t.Fatal(err)
	}
	old := mergeRetryIdleRecheck
	mergeRetryIdleRecheck = 5 * time.Millisecond
	t.Cleanup(func() { mergeRetryIdleRecheck = old })
	return f
}

func TestMergeRetryLoopStopsOnContextCancel(t *testing.T) {
	f := retryLoopFixture(t, 3600)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.m.runMergeRetryLoop(ctx); close(done) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop still running after cancel")
	}
	if f.task("t1").MergeState == MergeStateMerged {
		t.Fatal("pass ran before the interval elapsed")
	}
}

func TestMergeRetryLoopOffAtZero(t *testing.T) {
	f := retryLoopFixture(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.m.runMergeRetryLoop(ctx); close(done) }()
	// Many idle rechecks pass; none may call the retry pass.
	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done
	if tk := f.task("t1"); tk.MergeState == MergeStateMerged || tk.MergeBlock != nil {
		t.Fatalf("pass ran with interval 0: state %q block %+v", tk.MergeState, tk.MergeBlock)
	}
}
