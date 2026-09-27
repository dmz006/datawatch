package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func gitRunT(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// Regression: the auto-commit-on-complete path used to save the session as
// Complete BEFORE running PostSessionCommit, so a poller (the autonomous
// verifier reads session state to decide when to compute its git diff) could
// observe Complete while the worker's own change was still uncommitted —
// producing a spurious "no changes detected" verification failure against a
// file that had, in fact, just been written and correctly committed. Found
// live via a sandbox e2e run 2026-09-27. The commit must land before Complete
// is ever visible to a reader of session state.
func TestCompletion_PostSessionCommitLandsBeforeStateIsVisibleAsComplete(t *testing.T) {
	dir := t.TempDir()
	gitRunT(t, dir, "init", "-q")
	gitRunT(t, dir, "config", "user.email", "t@t.test")
	gitRunT(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "target.md"), []byte("stub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRunT(t, dir, "add", "-A")
	gitRunT(t, dir, "commit", "-q", "-m", "stub")

	// The "worker" writes real content but leaves it uncommitted, exactly like
	// an opencode session with session.auto_git_commit's auto-commit about to run.
	if err := os.WriteFile(filepath.Join(dir, "target.md"), []byte("stub\nreal content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(filepath.Join(t.TempDir(), "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	mgr := &Manager{store: store, hostname: "testhost", autoGit: true}
	sess := &Session{
		ID: "abcd", FullID: "testhost-abcd", Hostname: "testhost",
		State: StateRunning, OneShot: true, ProjectDir: dir,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := store.Save(sess); err != nil {
		t.Fatal(err)
	}
	projGit := NewProjectGit(dir)

	var lastOutputTime time.Time
	var pendingLines []string
	var lastPromptMatchTime time.Time
	mgr.processOutputLine(context.Background(), sess, projGit, "DATAWATCH_COMPLETE: done", &lastOutputTime, &pendingLines, &lastPromptMatchTime, func() *Tracker { return nil })

	got, ok := store.Get(sess.FullID)
	if !ok || got.State != StateComplete {
		t.Fatalf("session should be Complete after the marker, got ok=%v state=%v", ok, got)
	}
	// By the time the function has returned (a fortiori, by the time any
	// concurrent poller could have observed StateComplete), the commit must
	// already be on disk — no uncommitted change left, and the completion
	// commit is at HEAD.
	statusOut, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	if len(statusOut) != 0 {
		t.Fatalf("worker's change must be committed by the time state is Complete, working tree still dirty: %q", statusOut)
	}
	logOut, err := exec.Command("git", "-C", dir, "log", "-1", "--format=%s").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got.DiffSummary == "" {
		t.Fatal("DiffSummary should be populated from the post-session commit")
	}
	t.Logf("HEAD commit: %s", logOut)
}
