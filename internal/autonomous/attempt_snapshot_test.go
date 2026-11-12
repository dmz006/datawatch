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

// attemptTempEntries lists the entries in the process temp dir whose
// names start with prefix, so the cleanup tests can compare before and
// after without assuming an empty temp dir.
func attemptTempEntries(t *testing.T, prefix string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]bool)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			out[e.Name()] = true
		}
	}
	return out
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

// TestAttemptSnapshotCleansUpTempDir: the deferred RemoveAll of the temp
// index dir is best-effort (never an error of its own), but it must
// actually run on the success path — a successful snapshot must leave
// no attempt-snapshot-* dir behind in the temp dir.
func TestAttemptSnapshotCleansUpTempDir(t *testing.T) {
	dir := snapTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Point the temp dir at a private empty area. The system temp dir is
	// shared with every other test binary `go test ./...` runs at the
	// same time (cmd/datawatch snapshots there too), and a snapshot
	// another process has in flight owns an attempt-snapshot-* dir until
	// it finishes — scanning the shared dir mistook that for a leak.
	tmpArea := t.TempDir()
	t.Setenv("TMPDIR", tmpArea)

	ref, err := SnapshotAttemptWork(context.Background(), dir, "task-cleanup", 1)
	if err != nil {
		t.Fatalf("SnapshotAttemptWork: %v", err)
	}
	if ref == "" {
		t.Fatal("expected a snapshot ref for a dirty tree")
	}
	for name := range attemptTempEntries(t, "attempt-snapshot-") {
		t.Errorf("SnapshotAttemptWork left temp dir %s behind — best-effort cleanup did not run", name)
	}
}

// TestAttemptSnapshotUnusableTempAreaSurfacesError: a read-only temp
// area makes MkdirTemp fail BEFORE any git work — that failure must be
// returned (wrapped) to the caller, not swallowed by the best-effort
// cleanup path.
func TestAttemptSnapshotUnusableTempAreaSurfacesError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission bits")
	}
	dir := snapTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	readOnly := t.TempDir()
	if err := os.Chmod(readOnly, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(readOnly, 0o755); err != nil {
			t.Errorf("restore temp area perms: %v", err)
		}
	})
	t.Setenv("TMPDIR", readOnly)

	ref, err := SnapshotAttemptWork(context.Background(), dir, "task-ro", 1)
	if err == nil {
		t.Fatalf("SnapshotAttemptWork with a read-only temp area: want error, got ref %q", ref)
	}
	if !strings.Contains(err.Error(), "create snapshot temp dir") {
		t.Errorf("error %q must be the wrapped MkdirTemp failure", err)
	}
}
