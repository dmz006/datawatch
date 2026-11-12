package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// uwWrite writes a file under dir, failing the test on error.
func uwWrite(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// uwCommitFile commits one new file in dir.
func uwCommitFile(t *testing.T, dir, name string) {
	t.Helper()
	uwWrite(t, dir, name, name+"\n")
	uwGit(t, dir, "add", name)
	uwGit(t, dir, "commit", "-q", "-m", "add "+name)
}

func uwCount(t *testing.T, dir string) string {
	t.Helper()
	return uwGit(t, dir, "rev-list", "--count", "HEAD")
}

// uwHeadChanges returns the HEAD commit's name-status lines.
func uwHeadChanges(t *testing.T, dir string) []string {
	t.Helper()
	out := uwGit(t, dir, "show", "--name-status", "--format=", "HEAD")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func uwCleanStaged(t *testing.T, dir string, paths ...string) {
	t.Helper()
	args := append([]string{"diff", "--cached", "--name-only", "--"}, paths...)
	if got := uwGit(t, dir, args...); got != "" {
		t.Fatalf("staged area not clean for listed paths: %q", got)
	}
}

func TestUncommittedCommitStagedDeletionOnly(t *testing.T) {
	dir := uwRepo(t)
	uwCommitFile(t, dir, "extra.txt")
	uwGit(t, dir, "rm", "-q", "extra.txt")
	before := uwGit(t, dir, "rev-parse", "HEAD")
	n := uwCount(t, dir)

	sha, err := DefaultTaskCommit(context.Background(), dir, []string{"extra.txt"}, "remove extra")
	if err != nil {
		t.Fatal(err)
	}
	if after := uwGit(t, dir, "rev-parse", "HEAD"); sha != after || sha == before {
		t.Fatalf("returned sha %s, HEAD %s, before %s", sha, after, before)
	}
	if n != "2" || uwCount(t, dir) != "3" {
		t.Fatalf("HEAD must advance by exactly one commit, count %s -> %s", n, uwCount(t, dir))
	}
	if got := uwHeadChanges(t, dir); !sameSet(got, []string{"D\textra.txt"}) {
		t.Fatalf("commit records %q, want D\\textra.txt", got)
	}
	uwCleanStaged(t, dir, "extra.txt")
}

func TestUncommittedCommitUnstagedDeletionOfTrackedFile(t *testing.T) {
	dir := uwRepo(t)
	uwCommitFile(t, dir, "extra.txt")
	if err := os.Remove(filepath.Join(dir, "extra.txt")); err != nil {
		t.Fatal(err)
	}
	before := uwGit(t, dir, "rev-parse", "HEAD")

	sha, err := DefaultTaskCommit(context.Background(), dir, []string{"extra.txt"}, "remove extra")
	if err != nil {
		t.Fatal(err)
	}
	if after := uwGit(t, dir, "rev-parse", "HEAD"); sha != after || sha == before {
		t.Fatalf("returned sha %s, HEAD %s, before %s", sha, after, before)
	}
	if uwCount(t, dir) != "3" {
		t.Fatalf("HEAD must advance by exactly one commit, count = %s", uwCount(t, dir))
	}
	if got := uwHeadChanges(t, dir); !sameSet(got, []string{"D\textra.txt"}) {
		t.Fatalf("commit records %q, want D\\textra.txt", got)
	}
}

func TestUncommittedCommitMixedEditsNewFilesAndDeletion(t *testing.T) {
	dir := uwRepo(t)
	uwCommitFile(t, dir, "gone.txt")
	uwWrite(t, dir, "base.txt", "base\nedited\n")
	uwWrite(t, dir, "new.txt", "new\n")
	uwWrite(t, dir, "other.txt", "other\n")
	uwGit(t, dir, "rm", "-q", "gone.txt")
	n := uwCount(t, dir)

	_, err := DefaultTaskCommit(context.Background(), dir, []string{"base.txt", "new.txt", "gone.txt"}, "mixed")
	if err != nil {
		t.Fatal(err)
	}
	if now := uwCount(t, dir); now == n || now != "3" {
		t.Fatalf("HEAD must advance by exactly one commit, count %s -> %s", n, now)
	}
	want := []string{"M\tbase.txt", "A\tnew.txt", "D\tgone.txt"}
	if got := uwHeadChanges(t, dir); !sameSet(got, want) {
		t.Fatalf("commit records %q, want %q", got, want)
	}
	if st := uwGit(t, dir, "status", "--porcelain"); st != "?? other.txt" {
		t.Fatalf("status = %q, want only other.txt untracked", st)
	}
	uwCleanStaged(t, dir, "base.txt", "new.txt", "gone.txt")
}

func TestUncommittedCommitPathAbsentEverywhereIsSkipped(t *testing.T) {
	dir := uwRepo(t)
	uwWrite(t, dir, "real.txt", "real\n")

	sha, err := DefaultTaskCommit(context.Background(), dir, []string{"ghost.txt", "real.txt"}, "real")
	if err != nil {
		t.Fatal(err)
	}
	if uwCount(t, dir) != "2" {
		t.Fatalf("want 2 commits, got %s", uwCount(t, dir))
	}
	if got := uwHeadChanges(t, dir); !sameSet(got, []string{"A\treal.txt"}) {
		t.Fatalf("commit records %q, want only real.txt", got)
	}

	again, err := DefaultTaskCommit(context.Background(), dir, []string{"ghost.txt"}, "ghost")
	if err != nil {
		t.Fatal(err)
	}
	if again != sha || uwCount(t, dir) != "2" {
		t.Fatalf("ghost-only call returned %s (count %s), want %s and no new commit", again, uwCount(t, dir), sha)
	}
}

func TestUncommittedCommitPathOutsideRepositoryStillFails(t *testing.T) {
	dir := uwRepo(t)
	other := t.TempDir()
	abs := filepath.Join(other, "outside.txt")
	if err := os.WriteFile(abs, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := uwGit(t, dir, "rev-parse", "HEAD")

	for _, p := range []string{"../outside.txt", abs} {
		sha, err := DefaultTaskCommit(context.Background(), dir, []string{p}, "escape")
		if err == nil {
			t.Fatalf("%s: want an error, got sha %s", p, sha)
		}
		if !strings.Contains(err.Error(), "git add") {
			t.Errorf("%s: error should mention git add, got %v", p, err)
		}
		if after := uwGit(t, dir, "rev-parse", "HEAD"); after != before {
			t.Fatalf("%s: HEAD moved to %s", p, after)
		}
	}
}
