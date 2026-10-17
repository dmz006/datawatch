package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// commitOnCompletion must stage and commit the worker's changes with the
// summary the worker reported in its final output, and record the commit
// SHA and summary on the session — with no config gating.
func TestCommitOnCompletion_CommitsWorkWithWorkerSummary(t *testing.T) {
	dir := t.TempDir()
	gitRunT(t, dir, "init", "-q")
	gitRunT(t, dir, "config", "user.email", "t@t.test")
	gitRunT(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "target.md"), []byte("stub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRunT(t, dir, "add", "-A")
	gitRunT(t, dir, "commit", "-q", "-m", "stub")

	// The "worker" leaves a dirty file exactly like a real session would.
	if err := os.WriteFile(filepath.Join(dir, "target.md"), []byte("stub\nreal content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Session log whose last line carries the completion summary.
	logFile := filepath.Join(t.TempDir(), "session.log")
	logContent := "started task\nworking...\nDATAWATCH_COMPLETE: added the widget\n"
	if err := os.WriteFile(logFile, []byte(logContent), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(filepath.Join(t.TempDir(), "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	mgr := &Manager{store: store, hostname: "testhost"}
	sess := &Session{
		ID: "abcd", FullID: "testhost-abcd", Hostname: "testhost",
		State: StateRunning, OneShot: true, ValidateCommands: []string{"true"}, ProjectDir: dir,
		LogFile: logFile, Task: "add the widget",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := store.Save(sess); err != nil {
		t.Fatal(err)
	}

	mgr.commitOnCompletion(sess, NewProjectGit(dir))

	gitOut := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return strings.TrimSpace(string(out))
	}

	subjectOut := gitOut("log", "-1", "--format=%s")
	if subjectOut != "added the widget" {
		t.Fatalf("HEAD subject = %q, want %q", subjectOut, "added the widget")
	}
	if got, want := sess.CommitSHA, gitOut("rev-parse", "HEAD"); got != want {
		t.Fatalf("CommitSHA = %q, want HEAD %q", got, want)
	}
	if status := gitOut("status", "--porcelain"); status != "" {
		t.Fatalf("working tree should be clean after commit, got %q", status)
	}
	if sess.CompletionSummary != "added the widget" {
		t.Fatalf("CompletionSummary = %q, want %q", sess.CompletionSummary, "added the widget")
	}
	if sess.DiffSummary == "" {
		t.Fatal("DiffSummary should be populated for a non-zero diff")
	}
}

// commitOnCompletion must never commit for a non-one-shot (interactive)
// session, regardless of what the caller's own completion heuristic
// decided — the guard belongs here, not trusted to every StateComplete
// call site individually. Found live 2026-10-16: a long-running
// interactive session hit a transient completion-pattern false positive
// and this function, missing this check, committed whatever was sitting
// in that session's working tree at that instant.
func TestCommitOnCompletion_NeverCommitsForInteractiveSession(t *testing.T) {
	dir := t.TempDir()
	gitRunT(t, dir, "init", "-q")
	gitRunT(t, dir, "config", "user.email", "t@t.test")
	gitRunT(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "target.md"), []byte("stub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRunT(t, dir, "add", "-A")
	gitRunT(t, dir, "commit", "-q", "-m", "stub")

	// Something is sitting dirty in the tree when the false-positive
	// completion fires — exactly the dangerous case.
	if err := os.WriteFile(filepath.Join(dir, "target.md"), []byte("stub\nin-progress edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(filepath.Join(t.TempDir(), "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	mgr := &Manager{store: store, hostname: "testhost"}
	sess := &Session{
		ID: "ab22", FullID: "testhost-ab22", Hostname: "testhost",
		State: StateRunning, OneShot: false, ProjectDir: dir,
		Task:      "",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := store.Save(sess); err != nil {
		t.Fatal(err)
	}

	mgr.commitOnCompletion(sess, NewProjectGit(dir))

	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) == "" {
		t.Fatal("expected the dirty file to remain uncommitted for an interactive session")
	}
	if sess.CommitSHA != "" {
		t.Fatalf("CommitSHA should stay empty for an interactive session, got %q", sess.CommitSHA)
	}
}
