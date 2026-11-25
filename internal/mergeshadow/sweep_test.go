package mergeshadow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkdir(t *testing.T, p string) string {
	t.Helper()
	if err := os.MkdirAll(p, 0o750); err != nil {
		t.Fatal(err)
	}
	return p
}

// A daemon that stops mid-trial leaves the holder folder and the worktree
// registration behind; the sweep removes both and touches nothing else.
func TestSweepScratchRemovesLeftoverTrials(t *testing.T) {
	repo := newTrialRepo(t)
	data := t.TempDir()
	scratch := mkdir(t, filepath.Join(data, scratchDirName))

	holder := mkdir(t, filepath.Join(scratch, holderPrefix+"1"))
	git(t, repo, "worktree", "add", "-q", "--detach", filepath.Join(holder, "wt"), "main")
	write(t, filepath.Join(holder, "wt"), "left-by-a-gate.txt", "x\n")
	// Stopped before the checkout was made.
	empty := mkdir(t, filepath.Join(scratch, holderPrefix+"2"))
	// Not a trial: another folder, a file, and a link that has a trial's name.
	other := mkdir(t, filepath.Join(scratch, "keep-me"))
	write(t, scratch, holderPrefix+"file", "x\n")
	outside := mkdir(t, filepath.Join(t.TempDir(), "outside"))
	write(t, outside, "precious.txt", "x\n")
	link := filepath.Join(scratch, holderPrefix+"link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	// A worktree of the same repository outside the scratch folder.
	mine := filepath.Join(t.TempDir(), "mine")
	git(t, repo, "worktree", "add", "-q", "--detach", mine, "main")
	head, refs, status := snapshot(t, repo)

	removed, err := SweepScratch(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 2 || removed[0] != holder || removed[1] != empty {
		t.Fatalf("removed %v, want %s and %s", removed, holder, empty)
	}
	for _, p := range []string{holder, empty} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s still present", p)
		}
	}
	for _, p := range []string{other, link, filepath.Join(scratch, holderPrefix+"file"), filepath.Join(outside, "precious.txt"), mine} {
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("%s must be kept: %v", p, err)
		}
	}
	list := git(t, repo, "worktree", "list", "--porcelain")
	if strings.Contains(list, holderPrefix) {
		t.Fatalf("trial still registered:\n%s", list)
	}
	if !strings.Contains(list, "mine") {
		t.Fatalf("the other worktree lost its registration:\n%s", list)
	}
	if h, r, s := snapshot(t, repo); h != head || r != refs || s != status {
		t.Fatal("the repository's head, refs or status changed")
	}
}

// A data directory that sits inside a repository must not make the sweep treat
// that repository as the owner of a half-made trial.
func TestSweepScratchHalfMadeTrialInsideARepository(t *testing.T) {
	repo := newTrialRepo(t)
	data := mkdir(t, filepath.Join(repo, "data"))
	holder := mkdir(t, filepath.Join(data, scratchDirName, holderPrefix+"1"))
	mkdir(t, filepath.Join(holder, "wt"))
	head, refs, _ := snapshot(t, repo)

	removed, err := SweepScratch(context.Background(), data)
	if err != nil || len(removed) != 1 {
		t.Fatalf("removed %v err %v", removed, err)
	}
	if h, r, _ := snapshot(t, repo); h != head || r != refs {
		t.Fatal("the surrounding repository changed")
	}
	if _, err := os.Stat(filepath.Join(repo, "a.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestSweepScratchNothingToDo(t *testing.T) {
	for _, data := range []string{"", t.TempDir()} {
		if removed, err := SweepScratch(context.Background(), data); err != nil || len(removed) != 0 {
			t.Fatalf("%q: removed %v err %v", data, removed, err)
		}
	}
}
