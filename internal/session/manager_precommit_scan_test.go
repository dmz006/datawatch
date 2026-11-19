package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// initPreCommitScanRepo — a bare git repo with one committed stub and one
// dirty file, exactly like a task's worktree looks when the completion
// commit fires (mirrors manager_completion_commit_test.go).
func initPreCommitScanRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRunT(t, dir, "init", "-q")
	gitRunT(t, dir, "config", "user.email", "t@t.test")
	gitRunT(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "target.md"), []byte("stub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRunT(t, dir, "add", "-A")
	gitRunT(t, dir, "commit", "-q", "-m", "stub")
	if err := os.WriteFile(filepath.Join(dir, "target.md"), []byte("stub\nreal content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// newPreCommitScanManager — a Manager with only what commitOnCompletion
// needs (a store and a hostname), like the completion-commit tests.
func newPreCommitScanManager(t *testing.T) *Manager {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return &Manager{store: store, hostname: "testhost"}
}

// preCommitScanSession — one-shot, git-enabled, PRD-owned (so the
// ad-hoc validation gate is skipped), dirty-worktree task session saved
// to the store.
func preCommitScanSession(t *testing.T, mgr *Manager, dir string, id string) *Session {
	t.Helper()
	sess := &Session{
		ID: id, FullID: "testhost-" + id, Hostname: "testhost",
		State: StateRunning, OneShot: true, Git: true,
		PRDID: "prd-precommit", ProjectDir: dir,
		Task:      "add the widget",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := mgr.store.Save(sess); err != nil {
		t.Fatal(err)
	}
	return sess
}

// The hook fires exactly once for a task session that passes every guard,
// receives the current session, and the completion commit still lands.
func TestPreCommitScan_FiresOnceWhenAllGuardsPass(t *testing.T) {
	dir := initPreCommitScanRepo(t)
	mgr := newPreCommitScanManager(t)
	sess := preCommitScanSession(t, mgr, dir, "abcd")

	calls := 0
	var got *Session
	mgr.SetPreCommitScan(func(s *Session) {
		calls++
		got = s
	})

	mgr.commitOnCompletion(sess, NewProjectGit(dir))

	if calls != 1 {
		t.Fatalf("hook fired %d times, want exactly 1", calls)
	}
	if got == nil || got.FullID != sess.FullID {
		t.Fatalf("hook received %+v, want session %s", got, sess.FullID)
	}
	if sess.CommitSHA == "" {
		t.Fatal("completion commit should still land (CommitSHA empty)")
	}
	out := gitStatusOf(t, dir)
	if strings.TrimSpace(out) != "" {
		t.Fatalf("working tree should be clean after commit, got %q", out)
	}
}

// A panicking hook must be logged and recovered: the commit still lands
// and no panic escapes commitOnCompletion.
func TestPreCommitScan_PanickingHookIsRecoveredAndCommitStillLands(t *testing.T) {
	dir := initPreCommitScanRepo(t)
	mgr := newPreCommitScanManager(t)
	sess := preCommitScanSession(t, mgr, dir, "ab01")

	mgr.SetPreCommitScan(func(s *Session) {
		panic("scan exploded")
	})

	mgr.commitOnCompletion(sess, NewProjectGit(dir)) // must not panic

	if sess.CommitSHA == "" {
		t.Fatal("a panicking hook must not stop the completion commit (CommitSHA empty)")
	}
	out := gitStatusOf(t, dir)
	if strings.TrimSpace(out) != "" {
		t.Fatalf("working tree should be clean after commit, got %q", out)
	}
}

// Guard parity: an interactive (non-one-shot) session never reaches the
// commit path, so the hook must not fire either.
func TestPreCommitScan_NeverFiresForInteractiveSession(t *testing.T) {
	dir := initPreCommitScanRepo(t)
	mgr := newPreCommitScanManager(t)
	sess := &Session{
		ID: "ab22", FullID: "testhost-ab22", Hostname: "testhost",
		State: StateRunning, OneShot: false, ProjectDir: dir,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := mgr.store.Save(sess); err != nil {
		t.Fatal(err)
	}

	calls := 0
	mgr.SetPreCommitScan(func(s *Session) { calls++ })

	mgr.commitOnCompletion(sess, NewProjectGit(dir))

	if calls != 0 {
		t.Fatalf("hook fired %d times for an interactive session, want 0", calls)
	}
	if sess.CommitSHA != "" {
		t.Fatal("interactive session must not commit")
	}
	out := gitStatusOf(t, dir)
	if strings.TrimSpace(out) == "" {
		t.Fatal("expected the dirty file to remain uncommitted for an interactive session")
	}
}

// Guard parity: a session that already committed (at most once per
// session) must not fire the hook again.
func TestPreCommitScan_NeverFiresWhenSessionAlreadyCommitted(t *testing.T) {
	dir := initPreCommitScanRepo(t)
	mgr := newPreCommitScanManager(t)
	sess := preCommitScanSession(t, mgr, dir, "ab33")
	sess.CommitSHA = "0123456789abcdef0123456789abcdef0123abcd"
	if err := mgr.store.Save(sess); err != nil {
		t.Fatal(err)
	}

	calls := 0
	mgr.SetPreCommitScan(func(s *Session) { calls++ })

	mgr.commitOnCompletion(sess, NewProjectGit(dir))

	if calls != 0 {
		t.Fatalf("hook fired %d times for an already-committed session, want 0", calls)
	}
}
