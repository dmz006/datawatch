package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// snapTestGit runs git in dir with a fixed identity so no global config is needed.
func snapTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// snapTestRepo returns a repo with one commit containing tracked.txt.
func snapTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	snapTestGit(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapTestGit(t, dir, "add", "tracked.txt")
	snapTestGit(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func TestAttemptSnapshotCapturesTrackedAndUntracked(t *testing.T) {
	dir := snapTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("edited tracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("brand new untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ref, err := SnapshotAttemptWork(context.Background(), dir, "task-1", 2)
	if err != nil {
		t.Fatalf("SnapshotAttemptWork: %v", err)
	}
	if ref != "refs/datawatch/attempts/task-1/2" {
		t.Fatalf("ref = %q, want refs/datawatch/attempts/task-1/2", ref)
	}

	if got := snapTestGit(t, dir, "show", ref+":tracked.txt"); got != "edited tracked" {
		t.Errorf("tracked.txt in snapshot = %q, want %q", got, "edited tracked")
	}
	if got := snapTestGit(t, dir, "show", ref+":new.txt"); got != "brand new untracked" {
		t.Errorf("new.txt in snapshot = %q, want %q", got, "brand new untracked")
	}
	// The user's real index must not have picked up the untracked file.
	if staged := snapTestGit(t, dir, "diff", "--cached", "--name-only"); staged != "" {
		t.Errorf("real index changed, staged files: %q", staged)
	}
}

func TestAttemptSnapshotNoChangesReturnsEmpty(t *testing.T) {
	dir := snapTestRepo(t)

	ref, err := SnapshotAttemptWork(context.Background(), dir, "task-clean", 1)
	if err != nil {
		t.Fatalf("SnapshotAttemptWork: %v", err)
	}
	if ref != "" {
		t.Fatalf("ref = %q, want empty", ref)
	}
	if refs := snapTestGit(t, dir, "for-each-ref", "refs/datawatch/"); refs != "" {
		t.Errorf("expected no refs, got %q", refs)
	}
}

func TestAttemptSnapshotLeavesStashStackUnchanged(t *testing.T) {
	dir := snapTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if before := snapTestGit(t, dir, "stash", "list"); before != "" {
		t.Fatalf("stash list before = %q, want empty", before)
	}
	if _, err := SnapshotAttemptWork(context.Background(), dir, "task-stash", 1); err != nil {
		t.Fatalf("SnapshotAttemptWork: %v", err)
	}
	if after := snapTestGit(t, dir, "stash", "list"); after != "" {
		t.Errorf("stash list after = %q, want empty", after)
	}
}

func TestAttemptSnapshotSanitizesTaskKey(t *testing.T) {
	dir := snapTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ref, err := SnapshotAttemptWork(context.Background(), dir, "../evil task/x", 3)
	if err != nil {
		t.Fatalf("SnapshotAttemptWork: %v", err)
	}
	want := "refs/datawatch/attempts/---evil-task-x/3"
	if ref != want {
		t.Fatalf("ref = %q, want %q", ref, want)
	}
	if refs := snapTestGit(t, dir, "for-each-ref", "--format=%(refname)", "refs/datawatch/"); refs != want {
		t.Errorf("refs = %q, want only %q", refs, want)
	}
}
