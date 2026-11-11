package autonomous

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClassifyCleanupPermissionErrors(t *testing.T) {
	if ClassifyCleanupPermissionErrors([]WorktreeCleanupFailed{{Error: "boom"}}) != nil {
		t.Fatal("non-permission failure must give nil")
	}
	dir := t.TempDir()
	iss := ClassifyCleanupPermissionErrors([]WorktreeCleanupFailed{
		{Worktree: dir, Error: "unlinkat x: Permission denied"},
		{Worktree: dir, Error: "permission denied"},
		{Worktree: filepath.Join(dir, "gone"), Error: "permission denied"},
	})
	if iss == nil || iss.Count != 3 || len(iss.Paths) != 2 || !iss.Fixable || iss.Hidden || iss.Why == "" {
		t.Fatalf("unexpected %+v", iss)
	}
	iss = ClassifyCleanupPermissionErrors([]WorktreeCleanupFailed{{Worktree: filepath.Join(dir, "gone"), Error: "permission denied"}})
	if iss == nil || !iss.Hidden || iss.Fixable {
		t.Fatalf("gone folder must be hidden: %+v", iss)
	}
}

func TestFixCleanupPermissions(t *testing.T) {
	base := t.TempDir()
	wt := filepath.Join(base, "wt")
	sub := filepath.Join(wt, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(sub, "f.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	of := filepath.Join(outside, "o.txt")
	if err := os.WriteFile(of, []byte("x"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(wt, "link")); err != nil {
		t.Fatal(err)
	}
	os.Chmod(f, 0o444)
	os.Chmod(sub, 0o555)
	t.Cleanup(func() { os.Chmod(sub, 0o755) })

	n, err := FixCleanupPermissions(base, []string{wt})
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if fi, _ := os.Stat(f); fi.Mode().Perm()&0o200 == 0 {
		t.Fatal("file still read-only")
	}
	if fi, _ := os.Stat(sub); fi.Mode().Perm()&0o200 == 0 {
		t.Fatal("dir still read-only")
	}
	if fi, _ := os.Stat(of); fi.Mode().Perm() != 0o444 {
		t.Fatal("file behind symlink was changed")
	}
	if _, err := FixCleanupPermissions(base, []string{outside}); err == nil {
		t.Fatal("path outside base must be refused")
	}
	if _, err := FixCleanupPermissions(base, []string{base}); err == nil {
		t.Fatal("base itself must be refused")
	}
}
