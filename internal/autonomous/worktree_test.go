// BL407 Phase 1 — EnsureWorktree/branchNameFor unit tests, against a
// real scratch git repo (not mocks) per this plan's own testing
// requirement.
package autonomous

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scratchGitRepo creates a real git repo in t.TempDir() with one
// commit on its default branch, and returns its path.
func scratchGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-m", "initial")
	return dir
}

func TestBranchNameFor_SlugifiesTitle(t *testing.T) {
	prd := &PRD{ID: "abc12345", Title: "Add Widget Support!"}
	got := branchNameFor(prd)
	want := "automaton/abc12345-add-widget-support"
	if got != want {
		t.Errorf("branchNameFor = %q, want %q", got, want)
	}
}

func TestBranchNameFor_EmptyTitle(t *testing.T) {
	prd := &PRD{ID: "abc12345"}
	got := branchNameFor(prd)
	want := "automaton/abc12345"
	if got != want {
		t.Errorf("branchNameFor = %q, want %q", got, want)
	}
}

func TestBranchNameFor_TruncatesLongName(t *testing.T) {
	prd := &PRD{ID: "abc12345", Title: strings.Repeat("very long title words ", 10)}
	got := branchNameFor(prd)
	if len(got) > 60 {
		t.Errorf("branchNameFor returned %d chars, want <= 60: %q", len(got), got)
	}
	if strings.HasSuffix(got, "-") {
		t.Errorf("branchNameFor must not end in a dash after truncation: %q", got)
	}
}

func TestEnsureWorktree_NoBaseRepoConfigured(t *testing.T) {
	prd := &PRD{ID: "abc12345"}
	if _, _, err := EnsureWorktree(prd, "", t.TempDir()); err == nil {
		t.Error("expected an error when baseRepo is empty")
	}
}

func TestEnsureWorktree_CreatesWorktreeOnNewBranch(t *testing.T) {
	baseRepo := scratchGitRepo(t)
	worktreeDir := t.TempDir()
	prd := &PRD{ID: "abc12345", Title: "Add widgets"}

	path, branch, err := EnsureWorktree(prd, baseRepo, worktreeDir)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	if branch != "automaton/abc12345-add-widgets" {
		t.Errorf("branch = %q, want automaton/abc12345-add-widgets", branch)
	}
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		t.Errorf("worktree .git not found at %s: %v", path, err)
	}
	if _, err := os.Stat(filepath.Join(path, "README.md")); err != nil {
		t.Errorf("worktree missing checked-out content: %v", err)
	}

	// The new branch must actually exist in the base repo, checked
	// out in the worktree (not just a detached clone).
	cmd := exec.Command("git", "-C", path, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != branch {
		t.Errorf("worktree HEAD branch = %q, want %q", got, branch)
	}
}

func TestEnsureWorktree_IdempotentOnResume(t *testing.T) {
	baseRepo := scratchGitRepo(t)
	worktreeDir := t.TempDir()
	prd := &PRD{ID: "abc12345", Title: "Add widgets"}

	path1, branch1, err := EnsureWorktree(prd, baseRepo, worktreeDir)
	if err != nil {
		t.Fatalf("first EnsureWorktree: %v", err)
	}
	// Simulate a resumed Run(): the PRD now carries the persisted
	// branch from the first call.
	prd.Git.Branch = branch1

	path2, branch2, err := EnsureWorktree(prd, baseRepo, worktreeDir)
	if err != nil {
		t.Fatalf("second EnsureWorktree: %v", err)
	}
	if path2 != path1 || branch2 != branch1 {
		t.Errorf("resume produced (%q,%q), want (%q,%q) unchanged", path2, branch2, path1, branch1)
	}

	// Must not have created a second worktree directory.
	entries, err := os.ReadDir(worktreeDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("worktreeDir has %d entries, want 1: %v", len(entries), entries)
	}
}

func TestEnsureWorktree_BadBaseRepoPath(t *testing.T) {
	prd := &PRD{ID: "abc12345"}
	if _, _, err := EnsureWorktree(prd, t.TempDir()+"/does-not-exist", t.TempDir()); err == nil {
		t.Error("expected an error for a non-existent base repo path")
	}
}

func TestEnsureWorktree_RespectsBaseBranchOverride(t *testing.T) {
	baseRepo := scratchGitRepo(t)
	gitIn := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = baseRepo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// Diverge "develop" from "main" with its own commit, so the test
	// actually proves BaseBranch was honored rather than trivially
	// passing because both branches share one commit.
	gitIn("checkout", "-b", "develop")
	if err := os.WriteFile(filepath.Join(baseRepo, "DEVELOP_ONLY.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn("add", "DEVELOP_ONLY.md")
	gitIn("commit", "-m", "develop-only commit")
	gitIn("checkout", "main")

	prd := &PRD{ID: "abc12345", Title: "x"}
	prd.Git.BaseBranch = "develop"
	path, _, err := EnsureWorktree(prd, baseRepo, t.TempDir())
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	if _, serr := os.Stat(filepath.Join(path, "DEVELOP_ONLY.md")); serr != nil {
		t.Errorf("worktree missing DEVELOP_ONLY.md — BaseBranch override was ignored, worktree came from main instead of develop: %v", serr)
	}
}
