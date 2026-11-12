package autonomous

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// uwcWorktree makes a repository one level inside a base directory and
// returns both.
func uwcWorktree(t *testing.T) (base, wt string) {
	t.Helper()
	wt = uwRepo(t)
	return filepath.Dir(wt), wt
}

func TestUncommittedDiffShowsTrackedAndNewFiles(t *testing.T) {
	base, wt := uwcWorktree(t)
	uwWrite(t, wt, "base.txt", "changed\n")
	uwWrite(t, wt, "fresh.txt", "brand new\n")
	got, err := UncommittedDiff(context.Background(), base, wt)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"base.txt", "+changed", "fresh.txt", "+brand new"} {
		if !strings.Contains(got, want) {
			t.Errorf("diff missing %q:\n%s", want, got)
		}
	}
	if st := uwGit(t, wt, "status", "--porcelain"); !strings.Contains(st, "?? fresh.txt") {
		t.Errorf("diff must not stage anything, status: %q", st)
	}
}

func TestUncommittedDiffCleanWorktreeIsEmpty(t *testing.T) {
	base, wt := uwcWorktree(t)
	got, err := UncommittedDiff(context.Background(), base, wt)
	if err != nil || got != "" {
		t.Fatalf("want empty diff, got %q err %v", got, err)
	}
}

func TestCommitUncommittedCommitsListedFiles(t *testing.T) {
	base, wt := uwcWorktree(t)
	uwWrite(t, wt, "base.txt", "changed\n")
	uwWrite(t, wt, "fresh.txt", "brand new\n")
	before := uwCount(t, wt)
	sha, err := CommitUncommitted(context.Background(), base, wt, "save the work")
	if err != nil {
		t.Fatal(err)
	}
	if sha == "" || sha != uwGit(t, wt, "rev-parse", "HEAD") {
		t.Errorf("sha %q is not HEAD", sha)
	}
	if uwCount(t, wt) == before {
		t.Error("no commit was made")
	}
	if st := uwGit(t, wt, "status", "--porcelain"); st != "" {
		t.Errorf("worktree still dirty: %q", st)
	}
	if msg := uwGit(t, wt, "log", "-1", "--format=%s"); msg != "save the work" {
		t.Errorf("message = %q", msg)
	}
}

func TestCommitUncommittedRejectsEmptyMessage(t *testing.T) {
	base, wt := uwcWorktree(t)
	uwWrite(t, wt, "fresh.txt", "x\n")
	before := uwCount(t, wt)
	for _, msg := range []string{"", "   \n\t"} {
		if _, err := CommitUncommitted(context.Background(), base, wt, msg); !errors.Is(err, ErrUncommittedEmptyMessage) {
			t.Errorf("message %q: err = %v", msg, err)
		}
	}
	if uwCount(t, wt) != before || !strings.Contains(uwGit(t, wt, "status", "--porcelain"), "fresh.txt") {
		t.Error("a rejected commit changed the worktree")
	}
}

func TestCommitUncommittedRejectsCleanWorktree(t *testing.T) {
	base, wt := uwcWorktree(t)
	if _, err := CommitUncommitted(context.Background(), base, wt, "msg"); !errors.Is(err, ErrUncommittedNothing) {
		t.Fatalf("err = %v", err)
	}
}

func TestUncommittedWorktreeOutsideBaseRejected(t *testing.T) {
	_, wt := uwcWorktree(t)
	uwWrite(t, wt, "fresh.txt", "x\n")
	otherBase := t.TempDir()
	ctx := context.Background()
	if _, err := UncommittedDiff(ctx, otherBase, wt); !errors.Is(err, ErrUncommittedOutsideBase) {
		t.Errorf("diff: err = %v", err)
	}
	if _, err := CommitUncommitted(ctx, otherBase, wt, "msg"); !errors.Is(err, ErrUncommittedOutsideBase) {
		t.Errorf("commit: err = %v", err)
	}
	// The base itself and a ".." escape are outside too.
	base := filepath.Dir(wt)
	if _, err := CommitUncommitted(ctx, base, base, "msg"); !errors.Is(err, ErrUncommittedOutsideBase) {
		t.Errorf("base itself: err = %v", err)
	}
	if _, err := CommitUncommitted(ctx, base, filepath.Join(wt, "..", "..", ".."), "msg"); err == nil {
		t.Error("escape path accepted")
	}
	if _, err := CommitUncommitted(ctx, "", wt, "msg"); !errors.Is(err, ErrUncommittedOutsideBase) {
		t.Errorf("empty base: err = %v", err)
	}
	if !strings.Contains(uwGit(t, wt, "status", "--porcelain"), "fresh.txt") {
		t.Error("a rejected commit staged or committed files")
	}
}

func TestUncommittedSymlinkEscapeRejected(t *testing.T) {
	_, wt := uwcWorktree(t)
	base := t.TempDir()
	link := filepath.Join(base, "link")
	if err := os.Symlink(wt, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, err := UncommittedDiff(context.Background(), base, link); !errors.Is(err, ErrUncommittedOutsideBase) {
		t.Fatalf("err = %v", err)
	}
}

func TestUncommittedSubdirectoryOfWorktreeRejected(t *testing.T) {
	base, wt := uwcWorktree(t)
	sub := filepath.Join(wt, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := UncommittedDiff(context.Background(), base, sub); !errors.Is(err, ErrUncommittedOutsideBase) {
		t.Fatalf("err = %v", err)
	}
}
