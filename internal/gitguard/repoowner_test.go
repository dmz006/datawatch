package gitguard

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func mkRepo(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "init", "-q")
	gitRun(t, root, "commit", "-q", "--allow-empty", "-m", "i")
	return root
}

func TestCheckRepoOwner(t *testing.T) {
	ctx := context.Background()
	anc := mkRepo(t)

	if top, err := CheckRepoOwner(ctx, anc, "status"); err != nil || top != anc {
		t.Fatalf("own repo: %q %v", top, err)
	}

	plain := filepath.Join(anc, "plain")
	if err := os.Mkdir(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	var hooked string
	RefusalHook = func(op, dir, top string) { hooked = op + "|" + dir + "|" + top }
	t.Cleanup(func() { RefusalHook = nil })
	_, err := CheckRepoOwner(ctx, plain, "add")
	if !errors.Is(err, ErrRepoNotOwned) {
		t.Fatalf("plain nested: want refusal, got %v", err)
	}
	if hooked != "add|"+plain+"|"+anc {
		t.Fatalf("hook = %q", hooked)
	}
	for _, s := range []string{plain, anc} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error %q lacks %s", err, s)
		}
	}
	RefusalHook = nil

	inner := filepath.Join(anc, "inner")
	if err := os.Mkdir(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, inner, "init", "-q")
	if _, err := CheckRepoOwner(ctx, inner, "commit"); err != nil {
		t.Fatalf("nested own repo: %v", err)
	}

	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, anc, "worktree", "add", "-q", "-b", "wtb", wt)
	wt, _ = filepath.EvalSymlinks(wt)
	if _, err := CheckRepoOwner(ctx, wt, "status", RepoOwnerOptions{ExpectedRepo: anc}); err != nil {
		t.Fatalf("worktree with expected: %v", err)
	}
	if _, err := CheckRepoOwner(ctx, wt, "status"); !errors.Is(err, ErrRepoNotOwned) {
		t.Fatalf("worktree without expected: %v", err)
	}
	other := mkRepo(t)
	if _, err := CheckRepoOwner(ctx, wt, "status", RepoOwnerOptions{ExpectedRepo: other}); !errors.Is(err, ErrRepoNotOwned) {
		t.Fatalf("worktree with wrong expected: %v", err)
	}

	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(inner, link); err != nil {
		t.Fatal(err)
	}
	if top, err := CheckRepoOwner(ctx, link, "status"); err != nil || top != inner {
		t.Fatalf("symlink: %q %v", top, err)
	}
}

func TestCheckRepoOwnerIgnoresCeiling(t *testing.T) {
	anc := mkRepo(t)
	plain := filepath.Join(anc, "p")
	_ = os.Mkdir(plain, 0o755)
	t.Setenv("GIT_CEILING_DIRECTORIES", anc)
	if _, err := CheckRepoOwner(context.Background(), plain, "status"); !errors.Is(err, ErrRepoNotOwned) {
		t.Fatalf("ceiling must not hide ancestor: %v", err)
	}
}

func TestIsHomeOrAncestor(t *testing.T) {
	base, _ := filepath.EvalSymlinks(t.TempDir())
	home := filepath.Join(base, "a", "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	sub := filepath.Join(home, "proj")
	_ = os.Mkdir(sub, 0o755)
	sib := filepath.Join(base, "a", "other")
	_ = os.Mkdir(sib, 0o755)
	for dir, want := range map[string]bool{
		home: true, filepath.Join(base, "a"): true, base: true, "/": true,
		sub: false, sib: false,
	} {
		if got := IsHomeOrAncestor(dir); got != want {
			t.Errorf("IsHomeOrAncestor(%s)=%v want %v", dir, got, want)
		}
	}
}
