// Pre-session snapshot commit in Start: one-shot task sessions always get
// a deterministic snapshot of a dirty tree before launch (regardless of the
// manager's auto-commit setting), while interactive (non-one-shot) sessions
// no longer auto-commit in Start at all.

package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newDirtyRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "datawatch@localhost"},
		{"config", "user.name", "datawatch"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("dirty work in progress\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A one-shot Start must snapshot a dirty tree even when the manager-level
// auto-commit setting is off and the per-session override is explicitly false
// (the override is no longer consulted in Start).
func TestStart_OneShotSnapshotsDirtyTreeWithAutoCommitOff(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	mgr.SetAutoGit(false, true) // manager auto-commit off
	dir := newDirtyRepo(t)

	mgr.SetLLMBackend("test", func(_ context.Context, _, tmuxSession, projectDir, _ string) error {
		if got := headSubject(t, projectDir); !strings.HasPrefix(got, "pre-session[") {
			t.Errorf("HEAD at launch = %q, want a pre-session[ snapshot commit", got)
		}
		if status, _ := exec.Command("git", "-C", projectDir, "status", "--porcelain").Output(); len(strings.TrimSpace(string(status))) != 0 {
			t.Errorf("working tree still dirty at launch: %q", strings.TrimSpace(string(status)))
		}
		_ = fake.NewSessionWithSize(tmuxSession, 80, 24)
		_ = fake.PipeOutput(tmuxSession, "/dev/null")
		return nil
	})

	if _, err := mgr.Start(context.Background(), "do the work", "", dir, &StartOptions{
		OneShot:       true,
		AutoGitCommit: boolPtr(false),
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

// A one-shot Start in a non-repo dir must still init (autoGitInit on) and
// snapshot the dirty tree.
func TestStart_OneShotInitsNonRepoDirAndSnapshots(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	mgr.SetAutoGit(false, true)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("untracked dirt\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr.SetLLMBackend("test", func(_ context.Context, _, tmuxSession, projectDir, _ string) error {
		if !NewProjectGit(projectDir).IsRepo() {
			t.Errorf("project dir is not a git repo at launch; expected init")
		}
		if got := headSubject(t, projectDir); !strings.HasPrefix(got, "pre-session[") {
			t.Errorf("HEAD at launch = %q, want a pre-session[ snapshot commit", got)
		}
		_ = fake.NewSessionWithSize(tmuxSession, 80, 24)
		_ = fake.PipeOutput(tmuxSession, "/dev/null")
		return nil
	})

	if _, err := mgr.Start(context.Background(), "do the work", "", dir, &StartOptions{OneShot: true}); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

func TestStart_OneShotInitsEvenWithAutoGitInitOff(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	mgr.SetAutoGit(false, false) // both manager auto-git settings off
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("untracked dirt\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr.SetLLMBackend("test", func(_ context.Context, _, tmuxSession, projectDir, _ string) error {
		if !NewProjectGit(projectDir).IsRepo() {
			t.Errorf("project dir is not a git repo at launch; expected init")
		}
		if got := headSubject(t, projectDir); !strings.HasPrefix(got, "pre-session[") {
			t.Errorf("HEAD at launch = %q, want a pre-session[ snapshot commit", got)
		}
		_ = fake.NewSessionWithSize(tmuxSession, 80, 24)
		_ = fake.PipeOutput(tmuxSession, "/dev/null")
		return nil
	})

	if _, err := mgr.Start(context.Background(), "do the work", "", dir, &StartOptions{OneShot: true}); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

// Interactive (non-one-shot) sessions must not auto-commit in Start, even
// with the manager auto-commit setting on.
func TestStart_InteractiveDoesNotAutoCommit(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	mgr.SetAutoGit(true, true)
	dir := newDirtyRepo(t)

	mgr.SetLLMBackend("test", func(_ context.Context, _, tmuxSession, projectDir, _ string) error {
		if err := exec.Command("git", "-C", projectDir, "rev-parse", "--verify", "HEAD").Run(); err == nil {
			t.Errorf("interactive session made a commit at launch: %q", headSubject(t, projectDir))
		}
		_ = fake.NewSessionWithSize(tmuxSession, 80, 24)
		_ = fake.PipeOutput(tmuxSession, "/dev/null")
		return nil
	})

	if _, err := mgr.Start(context.Background(), "chat session", "", dir, &StartOptions{
		OneShot:       false,
		AutoGitCommit: boolPtr(true),
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

func boolPtr(v bool) *bool { return &v }
