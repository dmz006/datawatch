package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// reapGit runs git in dir and fails the test on error.
func reapGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func reapHasBranch(t *testing.T, repo, branch string) bool {
	t.Helper()
	return exec.Command("git", "-C", repo, "rev-parse", "--verify", "-q", "refs/heads/"+branch).Run() == nil
}

func reapHasEntry(es []ReapEntry, branch string) bool {
	for _, e := range es {
		if e.Branch == branch {
			return true
		}
	}
	return false
}

// TestAPIReapDelegation drives the API wrapper's reaper methods against a
// throwaway repo with a bare remote and a throwaway data directory: dry run,
// run, restore, and a branch whose work is not on main is kept.
func TestAPIReapDelegation(t *testing.T) {
	repo := gitRepo(t, true)
	const (
		work      = "automaton/prd-1"
		contained = "automaton/prd-1--tasks-done-t1"
		kept      = "automaton/prd-1--tasks-wip-t2"
	)
	// contained: points at main's tip, so every commit is already on main.
	reapGit(t, repo, "branch", contained)
	// kept: carries a commit that is not on main.
	reapGit(t, repo, "checkout", "-q", "-b", kept)
	if err := writeReapFile(repo, "wip.txt"); err != nil {
		t.Fatal(err)
	}
	reapGit(t, repo, "add", "wip.txt")
	reapGit(t, repo, "commit", "-q", "-m", "unmerged work")
	reapGit(t, repo, "checkout", "-q", "main")
	// The Automaton's work branch is gone (merged and dropped).
	mainTip := reapGit(t, repo, "rev-parse", "main")

	m, err := NewManager(t.TempDir(), Config{WorktreeBaseRepo: repo}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.store.CreatePRD("reap", repo, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	p.Git.Branch = work
	p.Story = []Story{{Tasks: []Task{
		{ID: "t1", WorktreeBranch: contained},
		{ID: "t2", WorktreeBranch: kept},
	}}}
	if err := m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	api := NewAPI(m)
	ctx := context.Background()

	// Dry run: names both, changes nothing.
	rep, backups, err := api.ReapPreview(ctx)
	if err != nil {
		t.Fatalf("ReapPreview: %v", err)
	}
	if !reapHasEntry(rep.Reaped, contained) || !reapHasEntry(rep.Waiting, kept) || len(backups) != 0 {
		t.Fatalf("preview = %+v backups=%v", rep, backups)
	}
	if !reapHasBranch(t, repo, contained) || !reapHasBranch(t, repo, kept) {
		t.Fatal("dry run must not delete a branch")
	}

	// Run: the contained branch goes (backup first), the other stays.
	rep, err = api.ReapRun(ctx)
	if err != nil {
		t.Fatalf("ReapRun: %v", err)
	}
	if !reapHasEntry(rep.Reaped, contained) || reapHasEntry(rep.Reaped, kept) {
		t.Fatalf("run reaped = %+v", rep.Reaped)
	}
	if reapHasBranch(t, repo, contained) {
		t.Fatal("contained branch should be deleted")
	}
	if !reapHasBranch(t, repo, kept) {
		t.Fatal("not-contained branch must be kept")
	}
	if !reapHasEntry(rep.Waiting, kept) {
		t.Fatalf("kept branch should be waiting: %+v", rep.Waiting)
	}

	// Restore: the branch returns at its old tip.
	ref, sha, err := api.ReapRestore(ctx, contained)
	if err != nil {
		t.Fatalf("ReapRestore: %v", err)
	}
	if !strings.HasPrefix(ref, reapBackupRefPrefix) || sha != mainTip {
		t.Fatalf("restore ref=%q sha=%q want tip %q", ref, sha, mainTip)
	}
	if !reapHasBranch(t, repo, contained) {
		t.Fatal("restored branch missing")
	}
}

func writeReapFile(dir, name string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o644)
}
