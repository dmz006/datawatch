// BL428 (design docs/plans/2026-10-14-concurrency-and-budget-design.md,
// decision 3a) — canonical project-tree key resolution.
package autonomous

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// newTestRepo creates an absolute, committed git repo under t.TempDir
// and returns its absClean'd root.
func newTestRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("-C", repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "hello.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("-C", repo, "add", "-A")
	git("-C", repo, "-c", "user.email=test@test", "-c", "user.name=test", "commit", "-q", "-m", "init")
	return absClean(repo)
}

// addTestWorktree links a new worktree of repo at an absolute sibling
// path and returns its absClean'd root.
func addTestWorktree(t *testing.T, repo, name string) string {
	t.Helper()
	wt := filepath.Join(t.TempDir(), name)
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("-C", repo, "worktree", "add", "-b", "feature/"+name, wt)
	return absClean(wt)
}

func TestTreeKeyMainCheckout(t *testing.T) {
	repo := newTestRepo(t)
	key, root, err := ResolveTreeKey(&PRD{ID: "a1", ProjectDir: repo}, nil)
	if err != nil {
		t.Fatalf("ResolveTreeKey: %v", err)
	}
	if want := "tree:" + repo; key != want {
		t.Errorf("key = %q; want %q", key, want)
	}
	if root != repo {
		t.Errorf("treeRoot = %q; want %q", root, repo)
	}
}

func TestTreeKeyLinkedWorktreeSameBase(t *testing.T) {
	repo := newTestRepo(t)
	wt := addTestWorktree(t, repo, "wt")

	repoKey, repoRoot, err := ResolveTreeKey(&PRD{ID: "a1", ProjectDir: repo}, nil)
	if err != nil {
		t.Fatalf("main checkout: %v", err)
	}
	wtKey, wtRoot, err := ResolveTreeKey(&PRD{ID: "a2", ProjectDir: wt}, nil)
	if err != nil {
		t.Fatalf("linked worktree: %v", err)
	}

	want := "tree:" + repo
	if repoKey != want {
		t.Errorf("main checkout key = %q; want %q", repoKey, want)
	}
	if wtKey != repoKey {
		t.Errorf("linked worktree key = %q; main checkout key = %q; want both %q", wtKey, repoKey, want)
	}
	if wtKey != want {
		t.Errorf("linked worktree key = %q; want %q", wtKey, want)
	}
	if repoRoot != repo {
		t.Errorf("main checkout treeRoot = %q; want %q", repoRoot, repo)
	}
	if wtRoot != wt {
		t.Errorf("linked worktree treeRoot = %q; want %q (the actual write root)", wtRoot, wt)
	}
}

func TestTreeKeySymlinkAndRelativeIdentical(t *testing.T) {
	repo := newTestRepo(t)
	parent := filepath.Dir(repo)
	ln := filepath.Join(parent, "ln-repo")
	if err := os.Symlink(repo, ln); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	byRepo, byRepoRoot, err := ResolveTreeKey(&PRD{ID: "a1", ProjectDir: repo}, nil)
	if err != nil {
		t.Fatalf("direct: %v", err)
	}
	byLn, byLnRoot, err := ResolveTreeKey(&PRD{ID: "a2", ProjectDir: ln}, nil)
	if err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if byRepo != byLn {
		t.Errorf("symlink key = %q; direct key = %q; want equal", byLn, byRepo)
	}
	if byLnRoot != byRepoRoot {
		t.Errorf("symlink treeRoot = %q; direct treeRoot = %q; want equal (resolved)", byLnRoot, byRepoRoot)
	}

	// Relative spelling of the same dir must key identically.
	t.Chdir(parent)
	byRel, _, err := ResolveTreeKey(&PRD{ID: "a3", ProjectDir: filepath.Base(repo)}, nil)
	if err != nil {
		t.Fatalf("relative: %v", err)
	}
	if byRepo != byRel {
		t.Errorf("relative key = %q; direct key = %q; want equal", byRel, byRepo)
	}
}

func TestTreeKeyWorktreeModePerWorktree(t *testing.T) {
	repo := newTestRepo(t)
	wt := addTestWorktree(t, repo, "wt")

	key, root, err := ResolveTreeKey(&PRD{ID: "a1", ProjectDir: wt, Git: GitWorkflow{Branch: "automaton/a1"}}, nil)
	if err != nil {
		t.Fatalf("ResolveTreeKey: %v", err)
	}
	// Worktree mode keys per worktree — NOT the shared base.
	want := "tree:" + wt
	if key != want {
		t.Errorf("key = %q; want %q", key, want)
	}
	if key == "tree:"+repo {
		t.Errorf("worktree-mode key collapsed to shared base %q", key)
	}
	if root != wt {
		t.Errorf("treeRoot = %q; want %q", root, wt)
	}

	// A per-task override back to the base repo is a different tree.
	keyOverride, rootOverride, err := ResolveTreeKey(
		&PRD{ID: "a1", ProjectDir: wt, Git: GitWorkflow{Branch: "automaton/a1"}},
		&Task{ID: "t1", ProjectDir: repo})
	if err != nil {
		t.Fatalf("task override: %v", err)
	}
	if keyOverride != "tree:"+repo || rootOverride != repo {
		t.Errorf("override key/root = %q/%q; want %q/%q", keyOverride, rootOverride, "tree:"+repo, repo)
	}
}

func TestTreeKeyTwoWorktreesDifferent(t *testing.T) {
	repo := newTestRepo(t)
	wt1 := addTestWorktree(t, repo, "wt-1")
	wt2 := addTestWorktree(t, repo, "wt-2")

	k1, _, err := ResolveTreeKey(&PRD{ID: "a1", ProjectDir: wt1, Git: GitWorkflow{Branch: "automaton/a1"}}, nil)
	if err != nil {
		t.Fatalf("prd1: %v", err)
	}
	k2, _, err := ResolveTreeKey(&PRD{ID: "a2", ProjectDir: wt2, Git: GitWorkflow{Branch: "automaton/a2"}}, nil)
	if err != nil {
		t.Fatalf("prd2: %v", err)
	}
	if k1 == k2 {
		t.Errorf("two worktree-mode PRDs resolved to one key %q; want distinct", k1)
	}
	if k1 != "tree:"+wt1 || k2 != "tree:"+wt2 {
		t.Errorf("keys = %q / %q; want %q / %q", k1, k2, "tree:"+wt1, "tree:"+wt2)
	}
}

func TestTreeKeyNonGitFallback(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plain", "proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dir = absClean(dir)

	key, root, err := ResolveTreeKey(&PRD{ID: "a1", ProjectDir: dir}, nil)
	if err != nil {
		t.Fatalf("ResolveTreeKey: %v", err)
	}
	if want := "tree:" + dir; key != want {
		t.Errorf("key = %q; want %q", key, want)
	}
	if root != dir {
		t.Errorf("treeRoot = %q; want %q", root, dir)
	}

	// A sibling plain dir must be a distinct key.
	other := absClean(filepath.Join(t.TempDir(), "other-proj"))
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	key2, _, err := ResolveTreeKey(&PRD{ID: "a2", ProjectDir: other}, nil)
	if err != nil {
		t.Fatalf("ResolveTreeKey other: %v", err)
	}
	if key == key2 {
		t.Errorf("two plain dirs resolved to one key %q; want distinct", key)
	}
}

func TestTreeKeyNoProjectDirErrors(t *testing.T) {
	if _, _, err := ResolveTreeKey(&PRD{ID: "a1"}, nil); err == nil {
		t.Error("empty PRD.ProjectDir: expected error")
	}
	if _, _, err := ResolveTreeKey(nil, nil); err == nil {
		t.Error("nil prd/task: expected error")
	}
}

func TestTreeLockPathMainCheckout(t *testing.T) {
	repo := newTestRepo(t)
	key, root, err := ResolveTreeKey(&PRD{ID: "a1", ProjectDir: repo}, nil)
	if err != nil {
		t.Fatalf("ResolveTreeKey: %v", err)
	}
	want := filepath.Join(repo, ".datawatch", "tree.lock")
	if got := treeLockPath(key, root); got != want {
		t.Errorf("treeLockPath = %q; want %q", got, want)
	}
}

func TestTreeLockPathWorktree(t *testing.T) {
	wt := absClean(filepath.Join(t.TempDir(), "prd-ab12cd34"))
	key, root, err := ResolveTreeKey(&PRD{ID: "a1", ProjectDir: wt, Git: GitWorkflow{Branch: "automaton/a1"}}, nil)
	if err != nil {
		t.Fatalf("ResolveTreeKey: %v", err)
	}
	want := filepath.Join(wt, ".datawatch", "tree.lock")
	if got := treeLockPath(key, root); got != want {
		t.Errorf("treeLockPath = %q; want %q", got, want)
	}
}

func TestTreeLockPathSharedBaseOneFile(t *testing.T) {
	repo := newTestRepo(t)
	wt := addTestWorktree(t, repo, "wt")

	k1, r1, err := ResolveTreeKey(&PRD{ID: "a1", ProjectDir: repo}, nil)
	if err != nil {
		t.Fatalf("main checkout: %v", err)
	}
	k2, r2, err := ResolveTreeKey(&PRD{ID: "a2", ProjectDir: wt}, nil)
	if err != nil {
		t.Fatalf("linked worktree: %v", err)
	}
	// Both holders of the shared base must agree on ONE advisory file,
	// discoverable in the main checkout.
	want := filepath.Join(repo, ".datawatch", "tree.lock")
	if got := treeLockPath(k1, r1); got != want {
		t.Errorf("main checkout lock = %q; want %q", got, want)
	}
	if got := treeLockPath(k2, r2); got != want {
		t.Errorf("linked worktree lock = %q; want %q (same shared file)", got, want)
	}
}
