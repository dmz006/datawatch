package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAttemptRestoreBringsBackKilledWork(t *testing.T) {
	dir := snapTestRepo(t)
	base := snapTestGit(t, dir, "rev-parse", "HEAD")

	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("edited tracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("brand new untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref, err := SnapshotAttemptWork(context.Background(), dir, "task-restore", 1)
	if err != nil {
		t.Fatalf("SnapshotAttemptWork: %v", err)
	}
	if ref == "" {
		t.Fatal("expected a snapshot ref for a dirty tree")
	}

	// Simulate the kill losing the work.
	snapTestGit(t, dir, "reset", "-q", "--hard", base)
	if err := os.Remove(filepath.Join(dir, "new.txt")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "tracked.txt")); string(got) != "original\n" {
		t.Fatalf("setup: tracked.txt after reset = %q, want original", got)
	}

	if err := RestoreAttemptWork(context.Background(), dir, ref); err != nil {
		t.Fatalf("RestoreAttemptWork: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "tracked.txt")); string(got) != "edited tracked\n" {
		t.Errorf("tracked.txt after restore = %q, want edited tracked", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "new.txt")); string(got) != "brand new untracked\n" {
		t.Errorf("new.txt after restore = %q, want brand new untracked", got)
	}
}

func TestAttemptRestoreRemovesFilesDeletedInSnapshot(t *testing.T) {
	dir := snapTestRepo(t)
	base := snapTestGit(t, dir, "rev-parse", "HEAD")

	if err := os.Remove(filepath.Join(dir, "tracked.txt")); err != nil {
		t.Fatal(err)
	}
	ref, err := SnapshotAttemptWork(context.Background(), dir, "task-deleted", 1)
	if err != nil {
		t.Fatalf("SnapshotAttemptWork: %v", err)
	}
	if ref == "" {
		t.Fatal("expected a snapshot ref for a deletion")
	}

	// Reset brings tracked.txt back, so the restore must delete it again.
	snapTestGit(t, dir, "reset", "-q", "--hard", base)
	if _, err := os.Stat(filepath.Join(dir, "tracked.txt")); err != nil {
		t.Fatalf("setup: tracked.txt should exist after reset: %v", err)
	}

	if err := RestoreAttemptWork(context.Background(), dir, ref); err != nil {
		t.Fatalf("RestoreAttemptWork: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "tracked.txt")); !os.IsNotExist(err) {
		t.Errorf("tracked.txt should be removed after restore, stat err = %v", err)
	}
}

func TestAttemptRestoreEmptyRefIsNoop(t *testing.T) {
	dir := snapTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("left alone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RestoreAttemptWork(context.Background(), dir, ""); err != nil {
		t.Fatalf("empty ref must return nil, got %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "tracked.txt")); string(got) != "left alone\n" {
		t.Errorf("empty ref changed the work tree: %q", got)
	}
}

func TestAttemptRestoreKeepsHEAD(t *testing.T) {
	dir := snapTestRepo(t)
	base := snapTestGit(t, dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref, err := SnapshotAttemptWork(context.Background(), dir, "task-head", 1)
	if err != nil {
		t.Fatalf("SnapshotAttemptWork: %v", err)
	}
	snapTestGit(t, dir, "reset", "-q", "--hard", base)

	if err := RestoreAttemptWork(context.Background(), dir, ref); err != nil {
		t.Fatalf("RestoreAttemptWork: %v", err)
	}
	if head := snapTestGit(t, dir, "rev-parse", "HEAD"); head != base {
		t.Errorf("HEAD moved from %s to %s", base, head)
	}
}

// TestAttemptRestoreCleansUpTempDir: the deferred RemoveAll of the temp
// index dir is best-effort (never an error of its own), but it must
// actually run on the success path — a successful restore must leave no
// attempt-restore-* dir behind in the temp dir.
func TestAttemptRestoreCleansUpTempDir(t *testing.T) {
	dir := snapTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref, err := SnapshotAttemptWork(context.Background(), dir, "task-rclean", 1)
	if err != nil || ref == "" {
		t.Fatalf("setup snapshot: ref=%q err=%v", ref, err)
	}

	before := attemptTempEntries(t, "attempt-restore-")
	if err := RestoreAttemptWork(context.Background(), dir, ref); err != nil {
		t.Fatalf("RestoreAttemptWork: %v", err)
	}
	for name := range attemptTempEntries(t, "attempt-restore-") {
		if _, existedBefore := before[name]; !existedBefore {
			t.Errorf("RestoreAttemptWork left temp dir %s behind — best-effort cleanup did not run", name)
		}
	}
}

// TestAttemptRestoreReadonlyTargetSurfacesError: a read-only work tree
// makes the checkout of the restored files fail — that failure must be
// returned to the caller, not swallowed by the (best-effort) temp
// cleanup of the function.
func TestAttemptRestoreReadonlyTargetSurfacesError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission bits")
	}
	dir := snapTestRepo(t)
	base := snapTestGit(t, dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref, err := SnapshotAttemptWork(context.Background(), dir, "task-rro", 1)
	if err != nil || ref == "" {
		t.Fatalf("setup snapshot: ref=%q err=%v", ref, err)
	}
	// Simulate the loss, then make the restore target unwritable.
	snapTestGit(t, dir, "reset", "-q", "--hard", base)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Errorf("restore work tree perms: %v", err)
		}
	})

	if err := RestoreAttemptWork(context.Background(), dir, ref); err == nil {
		t.Fatal("RestoreAttemptWork into a read-only work tree: want the checkout failure surfaced, got nil")
	}
}
