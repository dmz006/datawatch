package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func tgit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// tagRepo makes a work repo with one commit and a bare remote named origin.
func tagRepo(t *testing.T) (work, remote string) {
	t.Helper()
	root := t.TempDir()
	remote = filepath.Join(root, "remote.git")
	work = filepath.Join(root, "work")
	tgit(t, root, "init", "--bare", "-b", "main", remote)
	tgit(t, root, "init", "-b", "main", work)
	tgit(t, work, "config", "user.name", "t")
	tgit(t, work, "config", "user.email", "t@t")
	tgit(t, work, "remote", "add", "origin", remote)
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	tgit(t, work, "add", ".")
	tgit(t, work, "commit", "-m", "one")
	return work, remote
}

func TestFormatTag(t *testing.T) {
	if got, err := FormatTag("v{version}", "1.2.3"); err != nil || got != "v1.2.3" {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := FormatTag("release", "1.2.3"); err == nil {
		t.Error("format without placeholder must fail")
	}
	if _, err := FormatTag("v{version}", ""); err == nil {
		t.Error("empty version must fail")
	}
}

func TestIsClean(t *testing.T) {
	work, _ := tagRepo(t)
	ctx := context.Background()
	if ok, err := IsClean(ctx, work); err != nil || !ok {
		t.Fatalf("clean: %v %v", ok, err)
	}
	_ = os.WriteFile(filepath.Join(work, "u.txt"), []byte("x"), 0o644)
	if ok, _ := IsClean(ctx, work); ok {
		t.Error("untracked file must make the tree dirty")
	}
}

func TestTagLifecycle(t *testing.T) {
	work, remote := tagRepo(t)
	ctx := context.Background()
	head, err := HeadCommit(ctx, work)
	if err != nil {
		t.Fatal(err)
	}
	if st, _, _ := TagStatus(ctx, work, "v1.0.0", head); st != TagAbsent {
		t.Fatalf("want absent, got %s", st)
	}
	if err := CreateAnnotatedTag(ctx, work, "v1.0.0", "Release 1.0.0"); err != nil {
		t.Fatal(err)
	}
	if typ := tgit(t, work, "cat-file", "-t", "refs/tags/v1.0.0"); typ != "tag" {
		t.Errorf("tag must be annotated, got %s", typ)
	}
	if st, _, _ := TagStatus(ctx, work, "v1.0.0", head); st != TagSameCommit {
		t.Fatalf("want same_commit, got %s", st)
	}
	if err := PushBranchRef(ctx, work, "origin", "main"); err != nil {
		t.Fatal(err)
	}
	if err := PushTag(ctx, work, "origin", "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	if tgit(t, remote, "tag", "-l", "v1.0.0") != "v1.0.0" {
		t.Error("tag missing on remote")
	}
	_ = os.WriteFile(filepath.Join(work, "b.txt"), []byte("b"), 0o644)
	tgit(t, work, "add", ".")
	tgit(t, work, "commit", "-m", "two")
	head2, _ := HeadCommit(ctx, work)
	if st, at, _ := TagStatus(ctx, work, "v1.0.0", head2); st != TagOtherCommit || at != head {
		t.Fatalf("want other_commit at %s, got %s %s", head, st, at)
	}
}

func TestPushNeverForces(t *testing.T) {
	for _, a := range [][]string{ShipPushBranchArgs("origin", "main"), PushTagArgs("origin", "v1")} {
		for _, s := range a {
			if s == "--force" || s == "-f" || strings.HasPrefix(s, "+") || strings.Contains(s, ":+") {
				t.Errorf("force in %v", a)
			}
		}
	}
	work, _ := tagRepo(t)
	ctx := context.Background()
	for _, bad := range []string{"+main", "-x", "a b", "", "a..b"} {
		if err := PushBranchRef(ctx, work, "origin", bad); err == nil {
			t.Errorf("branch %q must be rejected", bad)
		}
		if err := PushTag(ctx, work, "origin", bad); err == nil {
			t.Errorf("tag %q must be rejected", bad)
		}
	}
}

func TestPushBranchNonFastForwardRefused(t *testing.T) {
	work, remote := tagRepo(t)
	ctx := context.Background()
	if err := PushBranchRef(ctx, work, "origin", "main"); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "other")
	tgit(t, filepath.Dir(other), "clone", remote, other)
	tgit(t, other, "config", "user.name", "t")
	tgit(t, other, "config", "user.email", "t@t")
	_ = os.WriteFile(filepath.Join(other, "o.txt"), []byte("o"), 0o644)
	tgit(t, other, "add", ".")
	tgit(t, other, "commit", "-m", "other")
	tgit(t, other, "push", "origin", "main")
	_ = os.WriteFile(filepath.Join(work, "w.txt"), []byte("w"), 0o644)
	tgit(t, work, "add", ".")
	tgit(t, work, "commit", "-m", "diverge")
	if err := PushBranchRef(ctx, work, "origin", "main"); err == nil {
		t.Fatal("diverged push must be refused, not forced")
	}
}

func TestCurrentBranch(t *testing.T) {
	work, _ := tagRepo(t)
	if b, _ := CurrentBranch(context.Background(), work); b != "main" {
		t.Errorf("got %q", b)
	}
}
