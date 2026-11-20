package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A task whose branch is gone from the base repository but whose worktree
// still holds the finished commit (detached) is merged, not retried forever.
func TestMergeRecreatesMissingTaskBranchFromDetachedWorktree(t *testing.T) {
	f := newMergeFixture(t, "", nil)
	f.addTask("t1", nil, "a.txt", "from a\n")
	f.seal()
	tk := f.task("t1")
	gitRun(t, tk.WorktreePath, "checkout", "-q", "--detach")
	gitRun(t, f.repo, "branch", "-D", tk.WorktreeBranch)
	if taskBranchExists(context.Background(), f.prd.ProjectDir, tk.WorktreeBranch) {
		t.Fatal("test setup: the branch should be gone")
	}
	out, err := f.m.MergeTaskBranch(context.Background(), f.prd, tk)
	if err != nil || out != MergeOutcomeMerged {
		t.Fatalf("outcome %s err %v", out, err)
	}
	if f.readPRDFile("a.txt") != "from a\n" {
		t.Fatal("the recovered task work is not on the PRD branch")
	}
	if !taskBranchExists(context.Background(), f.prd.ProjectDir, tk.WorktreeBranch) {
		t.Fatal("the branch was not recreated")
	}
}

// A task worktree that is a clone of its own holds a commit the base
// repository has never seen: it is fetched, then merged.
func TestMergeFetchesTaskCommitFromStandaloneCloneWorktree(t *testing.T) {
	f := newMergeFixture(t, "", nil)
	f.addTask("t1", nil, "a.txt", "from a\n")
	f.seal()
	tk := f.task("t1")
	// Replace the linked worktree with a standalone clone carrying its own commit.
	clone := filepath.Join(f.wt, "standalone-t1")
	gitRun(t, f.repo, "clone", "-q", f.repo, clone)
	gitRun(t, clone, "config", "user.email", "t@example.com")
	gitRun(t, clone, "config", "user.name", "t")
	gitRun(t, clone, "checkout", "-q", "--detach", "origin/"+f.prd.Git.Branch)
	writeMergeFile(t, clone, "clone.txt", "from the clone\n")
	gitRun(t, clone, "add", "clone.txt")
	gitRun(t, clone, "commit", "-q", "-m", "work of t1 in a clone")
	sha := gitOut(t, clone, "rev-parse", "HEAD")
	gitRun(t, f.repo, "worktree", "remove", "--force", tk.WorktreePath)
	gitRun(t, f.repo, "branch", "-D", tk.WorktreeBranch)
	tk.WorktreePath = clone
	if _, err := runGitInCtx(context.Background(), f.repo, "cat-file", "-e", sha+"^{commit}"); err == nil {
		t.Fatal("test setup: the base repository must not have the clone's commit")
	}
	out, err := f.m.MergeTaskBranch(context.Background(), f.prd, tk)
	if err != nil || out != MergeOutcomeMerged {
		t.Fatalf("outcome %s err %v", out, err)
	}
	if f.readPRDFile("clone.txt") != "from the clone\n" {
		t.Fatal("the clone's work is not on the PRD branch")
	}
}

// With no branch and no worktree there is nothing to recover: a clear error,
// and the tree is left alone.
func TestMergeMissingBranchWithoutWorktreeReportsClearly(t *testing.T) {
	f := newMergeFixture(t, "", nil)
	f.addTask("t1", nil, "a.txt", "from a\n")
	f.seal()
	tk := f.task("t1")
	gitRun(t, f.repo, "worktree", "remove", "--force", tk.WorktreePath)
	gitRun(t, f.repo, "branch", "-D", tk.WorktreeBranch)
	out, err := f.m.MergeTaskBranch(context.Background(), f.prd, tk)
	if err == nil || out != MergeOutcomeDeferred {
		t.Fatalf("outcome %s err %v, want a deferred error", out, err)
	}
	if !strings.Contains(err.Error(), "cannot be recovered") {
		t.Fatalf("error does not say the work cannot be recovered: %v", err)
	}
	f.assertCleanTree()
}

// An untracked file identical to the branch's file no longer blocks the merge.
func TestMergeDropsUntrackedCopyIdenticalToTheTaskFile(t *testing.T) {
	f := newMergeFixture(t, "", nil)
	f.addTask("t1", nil, "report.md", "the report\n")
	f.seal()
	writeMergeFile(t, f.prd.ProjectDir, "report.md", "the report\n")
	tk := f.task("t1")
	out, err := f.m.MergeTaskBranch(context.Background(), f.prd, tk)
	if err != nil || out != MergeOutcomeMerged {
		t.Fatalf("outcome %s err %v", out, err)
	}
	if f.readPRDFile("report.md") != "the report\n" {
		t.Fatal("the task's file is not on the PRD branch")
	}
	if _, err := os.Stat(filepath.Join(f.prd.ProjectDir, ".datawatch", "merge-aside", "t1", "report.md")); err == nil {
		t.Fatal("an identical copy must be dropped, not moved aside")
	}
	f.assertCleanTree()
}

// A different untracked file is moved aside, never deleted, and the merge goes on.
func TestMergeMovesDifferingUntrackedFileAside(t *testing.T) {
	f := newMergeFixture(t, "", nil)
	f.addTask("t1", nil, "report.md", "the task's report\n")
	f.seal()
	writeMergeFile(t, f.prd.ProjectDir, "report.md", "an earlier draft\n")
	tk := f.task("t1")
	out, err := f.m.MergeTaskBranch(context.Background(), f.prd, tk)
	if err != nil || out != MergeOutcomeMerged {
		t.Fatalf("outcome %s err %v", out, err)
	}
	if f.readPRDFile("report.md") != "the task's report\n" {
		t.Fatal("the merged file is not the task's")
	}
	kept, rerr := os.ReadFile(filepath.Join(f.prd.ProjectDir, ".datawatch", "merge-aside", "t1", "report.md"))
	if rerr != nil || string(kept) != "an earlier draft\n" {
		t.Fatalf("the differing file was not preserved aside: %v %q", rerr, kept)
	}
}

// An untracked file the merge does not touch stays exactly where it was.
func TestMergeLeavesUnrelatedUntrackedFilesAlone(t *testing.T) {
	f := newMergeFixture(t, "", nil)
	f.addTask("t1", nil, "a.txt", "from a\n")
	f.seal()
	writeMergeFile(t, f.prd.ProjectDir, "scratch.txt", "mine\n")
	tk := f.task("t1")
	if out, err := f.m.MergeTaskBranch(context.Background(), f.prd, tk); err != nil || out != MergeOutcomeMerged {
		t.Fatalf("outcome %s err %v", out, err)
	}
	if f.readPRDFile("scratch.txt") != "mine\n" {
		t.Fatal("an unrelated untracked file was touched")
	}
}
