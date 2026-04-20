// BL29 — git checkpoint + rollback tests.

package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func setupGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	mustGit := func(args ...string) {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	mustGit("init", "-q")
	mustGit("config", "user.email", "test@example.com")
	mustGit("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit("add", ".")
	mustGit("commit", "-q", "-m", "initial")
	return dir
}

func TestBL29_TagCheckpoint_PreAndPost(t *testing.T) {
	dir := setupGitRepo(t)
	g := NewProjectGit(dir)
	if err := g.TagCheckpoint("pre", "abcd", "test task"); err != nil {
		t.Fatalf("pre tag: %v", err)
	}
	if err := g.TagCheckpoint("post", "abcd", "test task"); err != nil {
		t.Fatalf("post tag: %v", err)
	}
	out, err := gitOutput(dir, "tag", "--list", "datawatch-*")
	if err != nil {
		t.Fatal(err)
	}
	if want := "datawatch-post-abcd"; !contains(out, want) {
		t.Errorf("missing tag %q in: %s", want, out)
	}
	if want := "datawatch-pre-abcd"; !contains(out, want) {
		t.Errorf("missing tag %q in: %s", want, out)
	}
}

func TestBL29_TagCheckpoint_InvalidKindRejected(t *testing.T) {
	dir := setupGitRepo(t) // need a real repo so the kind check runs
	g := NewProjectGit(dir)
	if err := g.TagCheckpoint("middle", "abcd", "x"); err == nil {
		t.Errorf("expected error for invalid kind")
	}
}

func TestBL29_TagCheckpoint_NotARepoNoOp(t *testing.T) {
	g := NewProjectGit(t.TempDir())
	if err := g.TagCheckpoint("pre", "abcd", "x"); err != nil {
		t.Errorf("non-repo should be no-op, got error: %v", err)
	}
}

func TestBL29_Rollback_Success(t *testing.T) {
	dir := setupGitRepo(t)
	g := NewProjectGit(dir)
	_ = g.TagCheckpoint("pre", "ses1", "initial state")

	// Make a change + commit it (so working tree is clean before rollback).
	if err := os.WriteFile(filepath.Join(dir, "NEW"), []byte("after\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_ = runGit(dir, "add", ".")
	_ = runGit(dir, "commit", "-q", "-m", "post-session change")

	if err := g.Rollback("ses1", false); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "NEW")); err == nil {
		t.Error("NEW should not exist after rollback")
	}
}

func TestBL29_Rollback_TagMissing(t *testing.T) {
	dir := setupGitRepo(t)
	g := NewProjectGit(dir)
	if err := g.Rollback("nonexistent", false); err == nil {
		t.Error("expected error for missing tag")
	}
}

func TestBL29_Rollback_DirtyTreeRefusedWithoutForce(t *testing.T) {
	dir := setupGitRepo(t)
	g := NewProjectGit(dir)
	_ = g.TagCheckpoint("pre", "ses2", "initial")
	if err := os.WriteFile(filepath.Join(dir, "DIRTY"), []byte("uncommitted\n"), 0644); err != nil {
		t.Fatal(err)
	}
	err := g.Rollback("ses2", false)
	if err == nil {
		t.Error("expected refusal on dirty tree without force")
	}
	// With force, succeeds.
	if err := g.Rollback("ses2", true); err != nil {
		t.Errorf("force should succeed, got: %v", err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
