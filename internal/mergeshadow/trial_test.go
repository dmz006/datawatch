package mergeshadow

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// newTrialRepo builds main with a.txt/b.txt, branch "clean" adding c.txt,
// and branch "conflict" editing a.txt while main also edits a.txt.
func newTrialRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q", "-b", "main")
	write(t, repo, "a.txt", "one\n")
	write(t, repo, "b.txt", "bee\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "base")
	git(t, repo, "checkout", "-q", "-b", "clean")
	write(t, repo, "c.txt", "cee\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "clean")
	git(t, repo, "checkout", "-q", "main")
	git(t, repo, "checkout", "-q", "-b", "conflict")
	write(t, repo, "a.txt", "branch\n")
	write(t, repo, "b.txt", "branch-b\n")
	git(t, repo, "commit", "-q", "-am", "conflict")
	git(t, repo, "checkout", "-q", "main")
	write(t, repo, "a.txt", "main\n")
	write(t, repo, "b.txt", "main-b\n")
	git(t, repo, "commit", "-q", "-am", "main change")
	return repo
}

func snapshot(t *testing.T, repo string) (string, string, string) {
	return git(t, repo, "rev-parse", "HEAD"),
		git(t, repo, "for-each-ref"),
		git(t, repo, "status", "--porcelain")
}

func assertUntouched(t *testing.T, repo, scratch string, head, refs, status string) {
	t.Helper()
	h, r, s := snapshot(t, repo)
	if h != head || r != refs || s != status {
		t.Fatalf("repository changed:\nHEAD %q->%q\nrefs %q->%q\nstatus %q->%q", head, h, refs, r, status, s)
	}
	if wl := git(t, repo, "worktree", "list", "--porcelain"); strings.Count(wl, "worktree ") != 1 {
		t.Fatalf("worktree left behind:\n%s", wl)
	}
	if ents, _ := os.ReadDir(scratch); len(ents) != 0 {
		t.Fatalf("scratch not empty: %v", ents)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "worktrees")); err == nil {
		ents, _ := os.ReadDir(filepath.Join(repo, ".git", "worktrees"))
		if len(ents) != 0 {
			t.Fatalf("worktree metadata left: %v", ents)
		}
	}
}

func TestTrialMergeCleanAndConflict(t *testing.T) {
	repo := newTrialRepo(t)
	scratch := filepath.Join(t.TempDir(), "scratch")
	head, refs, status := snapshot(t, repo)

	got, err := TrialMerge(context.Background(), repo, "main", "clean", scratch)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("clean merge conflicts = %#v", got)
	}
	assertUntouched(t, repo, scratch, head, refs, status)

	got, err = TrialMerge(context.Background(), repo, "main", "conflict", scratch)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.txt", "b.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("conflicts = %v, want %v", got, want)
	}
	assertUntouched(t, repo, scratch, head, refs, status)
}

func TestTrialMergeErrorsCleanUp(t *testing.T) {
	repo := newTrialRepo(t)
	scratch := filepath.Join(t.TempDir(), "scratch")
	head, refs, status := snapshot(t, repo)

	if _, err := TrialMerge(context.Background(), repo, "main", "no-such-branch", scratch); err == nil {
		t.Fatal("expected error for unknown work branch")
	}
	assertUntouched(t, repo, scratch, head, refs, status)

	if _, err := TrialMerge(context.Background(), repo, "no-such-default", "clean", scratch); err == nil {
		t.Fatal("expected error for unknown default branch")
	}
	assertUntouched(t, repo, scratch, head, refs, status)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := TrialMerge(ctx, repo, "main", "clean", scratch); err == nil {
		t.Fatal("expected error for cancelled context")
	}
	assertUntouched(t, repo, scratch, head, refs, status)
}

func TestTrialMergeArgumentChecks(t *testing.T) {
	repo := newTrialRepo(t)
	scratch := filepath.Join(t.TempDir(), "scratch")
	ctx := context.Background()
	if _, err := TrialMerge(ctx, "", "main", "clean", scratch); err == nil {
		t.Error("empty repo accepted")
	}
	if _, err := TrialMerge(ctx, repo, "main", "--abort", scratch); err == nil {
		t.Error("option-like branch accepted")
	}
	if _, err := TrialMerge(ctx, repo, "main", "clean", filepath.Join(repo, "scratch")); err == nil {
		t.Error("scratch inside project tree accepted")
	}
}
