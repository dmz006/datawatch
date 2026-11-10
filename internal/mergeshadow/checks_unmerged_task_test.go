package mergeshadow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// taskCheckRepo builds main with a file, branch "auto" with one commit, and
// returns the repository path.
func taskCheckRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q", "-b", "main")
	write(t, repo, "base.txt", "base\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "base")
	git(t, repo, "checkout", "-q", "-b", "auto")
	write(t, repo, "feat.txt", "feat\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "feat")
	git(t, repo, "checkout", "-q", "main")
	return repo
}

func TestCheckUnmergedTaskBranchesNoTaskBranches(t *testing.T) {
	repo := taskCheckRepo(t)
	findings, err := CheckUnmergedTaskBranches(context.Background(), repo, "auto")
	if err != nil {
		t.Fatalf("CheckUnmergedTaskBranches: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("findings %+v, want none", findings)
	}
}

func TestCheckUnmergedTaskBranchesMergedTaskNoFinding(t *testing.T) {
	repo := taskCheckRepo(t)
	git(t, repo, "checkout", "-q", "-b", "auto--tasks-done-t1", "auto")
	write(t, repo, "done.txt", "done\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "done")
	git(t, repo, "checkout", "-q", "auto")
	git(t, repo, "merge", "-q", "--no-ff", "-m", "merge done", "auto--tasks-done-t1")

	findings, err := CheckUnmergedTaskBranches(context.Background(), repo, "auto")
	if err != nil {
		t.Fatalf("CheckUnmergedTaskBranches: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("merged task branch produced findings %+v", findings)
	}
}

func TestCheckUnmergedTaskBranchesCountsUnmergedCommits(t *testing.T) {
	repo := taskCheckRepo(t)
	git(t, repo, "checkout", "-q", "-b", "auto--tasks-open-t2", "main")
	write(t, repo, "one.txt", "1\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "one")
	write(t, repo, "two.txt", "2\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "two")
	git(t, repo, "checkout", "-q", "main")

	// A branch that merely shares the prefix of another name is not a task.
	git(t, repo, "checkout", "-q", "-b", "auto-other", "main")
	write(t, repo, "x.txt", "x\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "x")
	git(t, repo, "checkout", "-q", "main")

	findings, err := CheckUnmergedTaskBranches(context.Background(), repo, "auto")
	if err != nil {
		t.Fatalf("CheckUnmergedTaskBranches: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings %+v, want one", findings)
	}
	f := findings[0]
	if f.Code != UnmergedTaskBranchCode || !f.Blocking {
		t.Errorf("finding %+v, want blocking %s", f, UnmergedTaskBranchCode)
	}
	if !strings.Contains(f.Message, "auto--tasks-open-t2 has 2 commit(s)") {
		t.Errorf("message %q, want the branch and its two commits", f.Message)
	}
}

func TestCheckUnmergedTaskBranchesBadArguments(t *testing.T) {
	ctx := context.Background()
	if _, err := CheckUnmergedTaskBranches(ctx, "", "auto"); err == nil {
		t.Error("empty repo path accepted")
	}
	if _, err := CheckUnmergedTaskBranches(ctx, "/nonexistent", ""); err == nil {
		t.Error("empty branch accepted")
	}
	if _, err := CheckUnmergedTaskBranches(ctx, "/nonexistent", "-x"); err == nil {
		t.Error("branch starting with '-' accepted")
	}
}

func TestCheckUnmergedTaskBranchesGitFailure(t *testing.T) {
	notRepo := t.TempDir()
	if _, err := CheckUnmergedTaskBranches(context.Background(), notRepo, "auto"); err == nil {
		t.Error("listing branches of a non-repository did not fail")
	}
}

func TestUnmergedCommitCountErrors(t *testing.T) {
	repo := taskCheckRepo(t)
	if _, err := unmergedCommitCount(context.Background(), repo, "no-such-branch", "auto"); err == nil {
		t.Error("count for a missing task branch did not fail")
	}
}
