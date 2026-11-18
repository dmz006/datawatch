package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func sweepGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

func sweepRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	sweepGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	sweepGit(t, repo, "add", ".")
	sweepGit(t, repo, "commit", "-m", "init")
	return repo
}

func TestDaemonStartSweepRemovesStaleHoldersAndWorktrees(t *testing.T) {
	repo := sweepRepo(t)
	data := t.TempDir()
	m, err := NewManager(data, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.CreatePRD("spec", repo, "ollama", "", ""); err != nil {
		t.Fatal(err)
	}
	holder := filepath.Join(data, mergeShadowScratchDir, mergeShadowHolderPrefix+"1")
	wt := filepath.Join(holder, "wt")
	if err := os.MkdirAll(holder, 0o750); err != nil {
		t.Fatal(err)
	}
	sweepGit(t, repo, "worktree", "add", "--detach", wt, "main")
	// A holder with no worktree in it is also stale.
	empty := filepath.Join(data, mergeShadowScratchDir, mergeShadowHolderPrefix+"2")
	if err := os.MkdirAll(empty, 0o750); err != nil {
		t.Fatal(err)
	}

	n, err := NewAPI(m).StaleTrialSweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Fatalf("removed %d, want at least 2", n)
	}
	for _, p := range []string{holder, empty} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s still present", p)
		}
	}
	if out := sweepGit(t, repo, "worktree", "list"); strings.Contains(out, "trial-merge-") {
		t.Fatalf("worktree still registered: %s", out)
	}
}

func TestPruneTrialWorktreesDetachedOnly(t *testing.T) {
	repo := sweepRepo(t)
	scratch := filepath.Join(t.TempDir(), mergeShadowScratchDir)
	if err := os.MkdirAll(scratch, 0o750); err != nil {
		t.Fatal(err)
	}
	trial := filepath.Join(scratch, "trial-merge-a", "wt")
	liveDetached := filepath.Join(scratch, "trial-merge-live", "wt")
	branched := filepath.Join(scratch, "trial-merge-b", "wt")
	outside := filepath.Join(t.TempDir(), "outside")
	sweepGit(t, repo, "worktree", "add", "--detach", trial, "main")
	sweepGit(t, repo, "worktree", "add", "--detach", liveDetached, "main")
	sweepGit(t, repo, "worktree", "add", "-b", "work", branched, "main")
	sweepGit(t, repo, "worktree", "add", "--detach", outside, "main")

	removed, err := PruneTrialWorktrees(context.Background(), NewMergeGitRunner(), repo, scratch, []string{liveDetached})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || !strings.HasSuffix(removed[0], filepath.Join("trial-merge-a", "wt")) {
		t.Fatalf("removed %v, want only the stale detached trial", removed)
	}
	for _, p := range []string{liveDetached, branched, outside} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s must be kept: %v", p, err)
		}
	}
	if _, err := os.Stat(trial); !os.IsNotExist(err) {
		t.Fatal("stale trial worktree still present")
	}
}
