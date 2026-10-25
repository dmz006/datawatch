// A session whose project directory is an Automaton-owned linked worktree
// (StartOptions.RequireExistingWorktree) must never turn a missing worktree
// into an empty directory or a freshly initialised repository: Start fails
// with ErrProjectDirNotWorktree and leaves the filesystem untouched. Without
// the option, Start keeps creating directories and initialising repositories.

package session

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scratchGit runs git in dir with a hermetic identity and no global config.
func scratchGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func TestStart_WorktreeOwnedDirIsNeverReinitialised(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", root)
	missing := filepath.Join(root, "prd-worktrees", "gone")

	mgr, _ := newTestManagerWithFake(t)
	_, err := mgr.Start(context.Background(), "task", "", missing, &StartOptions{
		OneShot:                 true,
		Git:                     true,
		PRDID:                   "p1",
		RequireExistingWorktree: true,
	})
	if err == nil {
		t.Fatal("Start succeeded for a missing worktree directory")
	}
	if !errors.Is(err, ErrProjectDirNotWorktree) {
		t.Fatalf("err = %v, want it to wrap ErrProjectDirNotWorktree", err)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error %q does not name the directory %s", err, missing)
	}
	if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
		t.Fatalf("directory must still not exist, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(missing, ".git")); !os.IsNotExist(statErr) {
		t.Fatalf(".git must not exist, stat err=%v", statErr)
	}
}

func TestStart_WorktreeOwnedDirRejectsStandaloneRepo(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", root)
	dir := filepath.Join(root, "standalone")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	scratchGit(t, dir, "init", "-q")

	mgr, _ := newTestManagerWithFake(t)
	_, err := mgr.Start(context.Background(), "task", "", dir, &StartOptions{
		OneShot:                 true,
		Git:                     true,
		PRDID:                   "p1",
		RequireExistingWorktree: true,
	})
	if !errors.Is(err, ErrProjectDirNotWorktree) {
		t.Fatalf("err = %v, want ErrProjectDirNotWorktree", err)
	}
	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if len(entries) != 1 || entries[0].Name() != ".git" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory was modified, entries = %v", names)
	}
}

func TestStart_WorktreeOwnedDirRejectsPlainDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", root)
	dir := filepath.Join(root, "plain")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := checkExistingWorktree(dir); !errors.Is(err, ErrProjectDirNotWorktree) {
		t.Fatalf("err = %v, want ErrProjectDirNotWorktree", err)
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkExistingWorktree(file); !errors.Is(err, ErrProjectDirNotWorktree) {
		t.Fatalf("file: err = %v, want ErrProjectDirNotWorktree", err)
	}
}

func TestStart_WorktreeOwnedDirAcceptsLinkedWorktree(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", root)
	base := filepath.Join(root, "base")
	wt := filepath.Join(root, "prd-worktrees", "w1")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	scratchGit(t, base, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(base, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scratchGit(t, base, "add", "go.mod")
	scratchGit(t, base, "commit", "-q", "-m", "init")
	scratchGit(t, base, "worktree", "add", "-q", "-b", "automaton/w1", wt, "main")

	if err := checkExistingWorktree(wt); err != nil {
		t.Fatalf("a real linked worktree was refused: %v", err)
	}
	// The gitlink is a file, and Start proceeds past the pre-check.
	startGitedOneShot(t, "worker task", wt, "done", &StartOptions{
		OneShot:                 true,
		RequireExistingWorktree: true,
	})
}

func TestStart_PlainDirStillInitialised(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", root)
	dir := filepath.Join(root, "fresh", "nested")

	startGitedOneShot(t, "worker task", dir, "done", &StartOptions{
		OneShot: true,
		Git:     true,
	})

	fi, err := os.Stat(filepath.Join(dir, ".git"))
	if err != nil {
		t.Fatalf("expected the directory to be created and initialised: %v", err)
	}
	if !fi.IsDir() {
		t.Errorf(".git should be a directory for a freshly initialised repository")
	}
}
