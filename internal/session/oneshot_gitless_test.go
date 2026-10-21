// Git-less one-shots (StartOptions.Git false, the default) must never touch
// git at all: no auto-init into the working dir, no snapshot at launch, no
// commit on completion — even inside a repo that already contains the
// operator's own work. Git-true one-shots keep the validated-commit
// behaviour: the completion commit lands only after the declared validation
// commands pass. The one-shot reaper keeps closing finished Git-false
// one-shots; it just never commits for them.

package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gitStatusOf returns the repo's `git status --porcelain` output (trimmed).
func gitStatusOf(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func gitHead(t *testing.T, dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// startGitedOneShot starts a one-shot session with the given StartOptions
// whose fake worker writes hello.txt into the project dir and reports
// `summary` in its final log line (as a real worker would).
func startGitedOneShot(t *testing.T, task, dir, summary string, opts *StartOptions) (*Manager, *Session) {
	t.Helper()
	mgr, fake := newTestManagerWithFake(t)
	mgr.SetLLMBackend("test", func(_ context.Context, _, tmuxSession, projectDir, logFile string) error {
		if err := os.WriteFile(filepath.Join(projectDir, "hello.txt"), []byte("hello\n"), 0o644); err != nil {
			t.Errorf("fake launch: write worker file: %v", err)
		}
		if logFile != "" {
			f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err == nil {
				_, _ = f.WriteString("DATAWATCH_COMPLETE: " + summary + "\n")
				_ = f.Close()
			}
		}
		_ = fake.NewSessionWithSize(tmuxSession, 80, 24)
		_ = fake.PipeOutput(tmuxSession, "/dev/null")
		return nil
	})
	sess, err := mgr.Start(context.Background(), task, "", dir, opts)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return mgr, sess
}

// completeOneShot feeds the completion marker through the same
// processOutputLine path a real log tail would, completing the session.
func completeOneShot(t *testing.T, mgr *Manager, sess *Session, marker string) *Session {
	t.Helper()
	var lastOutputTime time.Time
	var pendingLines []string
	var lastPromptMatch time.Time
	mgr.processOutputLine(context.Background(), sess, NewProjectGit(sess.ProjectDir), marker, &lastOutputTime, &pendingLines, &lastPromptMatch, func() *Tracker { return nil })
	current, ok := mgr.store.Get(sess.FullID)
	if !ok {
		t.Fatalf("session %s not in store after completion", sess.FullID)
	}
	return current
}

// (1) A Git-false one-shot in a non-repo dir must leave no .git behind and
// make no commit after completion — the worker's output just sits in the
// dir, untracked (there is no tracker) and uncommitted.
func TestGitlessOneShotNonRepoDirLeavesNoGit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "scratch.txt"), []byte("operator scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr, sess := startGitedOneShot(t, "worker task", dir, "done", &StartOptions{
		OneShot:          true,
		Git:              false,
		ValidateCommands: []string{"true"},
	})

	// No repo was created at launch.
	if _, err := os.Stat(filepath.Join(dir, ".git")); !os.IsNotExist(err) {
		t.Fatalf(".git must not exist after Start with Git=false, stat err=%v", err)
	}

	current := completeOneShot(t, mgr, sess, "DATAWATCH_COMPLETE: done")

	if current.State != StateComplete {
		t.Errorf("state = %v, want Complete", current.State)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); !os.IsNotExist(err) {
		t.Fatal(".git must not exist after completion with Git=false")
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").CombinedOutput()
	if err == nil {
		t.Errorf("git unexpectedly found a repository: %s", strings.TrimSpace(string(out)))
	}
	if current.CommitSHA != "" {
		t.Errorf("CommitSHA = %q, want empty for a Git-false session", current.CommitSHA)
	}
	if _, err := os.Stat(filepath.Join(dir, "hello.txt")); err != nil {
		t.Errorf("worker file should exist untouched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "scratch.txt")); err != nil {
		t.Errorf("operator scratch file must be untouched: %v", err)
	}
}

// (2) A Git-false one-shot that runs inside a repo must make no commit,
// leave HEAD unchanged and touch neither the index nor the working tree —
// the operator's own dirty work stays exactly where it was.
func TestGitlessOneShotInRepoMakesNoCommit(t *testing.T) {
	dir := t.TempDir()
	gitRunT(t, dir, "init", "-q", "-b", "main")
	gitRunT(t, dir, "config", "user.email", "test@example.com")
	gitRunT(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("operator's committed work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRunT(t, dir, "add", "-A")
	gitRunT(t, dir, "commit", "-q", "-m", "operator commit")

	// Uncommitted change, as an operator would naturally have one.
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("operator's committed work\nin-progress edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	headBefore := gitHead(t, dir)

	mgr, sess := startGitedOneShot(t, "worker task", dir, "done", &StartOptions{
		OneShot:          true,
		Git:              false,
		ValidateCommands: []string{"true"},
	})
	statusAfterWorker := gitStatusOf(t, dir)
	// The uncommitted edit and the worker's file are both visible now.
	if !strings.Contains(statusAfterWorker, "work.txt") || !strings.Contains(statusAfterWorker, "hello.txt") {
		t.Fatalf("sanity: status before completion = %q", statusAfterWorker)
	}

	current := completeOneShot(t, mgr, sess, "DATAWATCH_COMPLETE: done")

	if current.State != StateComplete {
		t.Errorf("state = %v, want Complete", current.State)
	}
	if got := gitHead(t, dir); got != headBefore {
		t.Errorf("HEAD changed: before=%q after=%q — a Git-false one-shot must not commit", headBefore, got)
	}
	if got := gitStatusOf(t, dir); got != statusAfterWorker {
		t.Errorf("working tree/index changed after completion:\nbefore: %q\nafter:  %q", statusAfterWorker, got)
	}
	if cached, _ := exec.Command("git", "-C", dir, "diff", "--cached").Output(); strings.TrimSpace(string(cached)) != "" {
		t.Errorf("index must be untouched; cached diff:\n%s", strings.TrimSpace(string(cached)))
	}
	if current.CommitSHA != "" {
		t.Errorf("CommitSHA = %q, want empty", current.CommitSHA)
	}
	if strings.HasPrefix(current.ValidationResult, "passed") {
		t.Errorf("a Git-false session must never reach a passing validation, got %q", current.ValidationResult)
	}
}

// (3) A Git-true one-shot keeps the validated-commit behaviour: the
// completion commit happens only after the validation commands pass.
func TestGitedOneShotCommitsOnlyAfterValidationPasses(t *testing.T) {
	dir := t.TempDir()
	gitRunT(t, dir, "init", "-q", "-b", "main")
	gitRunT(t, dir, "config", "user.email", "test@example.com")
	gitRunT(t, dir, "config", "user.name", "Test")
	gitRunT(t, dir, "commit", "--allow-empty", "-q", "-m", "initial")

	mgr, sess := startGitedOneShot(t, "worker task", dir, "validated-commit check", &StartOptions{
		OneShot: true,
		Git:     true,
		// The validation command has an observable side effect, so the test
		// can prove it only ran at completion (not at launch) and landed
		// before the commit.
		ValidateCommands: []string{"touch validated-ran"},
	})
	if _, err := os.Stat(filepath.Join(dir, "validated-ran")); !os.IsNotExist(err) {
		t.Fatal("sanity: validation must not have run before completion")
	}

	current := completeOneShot(t, mgr, sess, "DATAWATCH_COMPLETE: validated-commit check")

	if current.State != StateComplete {
		t.Errorf("state = %v, want Complete", current.State)
	}
	if _, err := os.Stat(filepath.Join(dir, "validated-ran")); err != nil {
		t.Fatalf("validation command must have run before the commit: %v", err)
	}
	if current.ValidationResult != "passed" {
		t.Errorf("ValidationResult = %q, want passed", current.ValidationResult)
	}
	if current.CommitSHA == "" {
		t.Fatal("expected a completion commit for a Git-true one-shot")
	}
	if got := gitOut(t, dir, "log", "-1", "--pretty=%s"); got != "validated-commit check" {
		t.Errorf("commit subject = %q, want the completion summary", got)
	}
	if got := gitOut(t, dir, "status", "--porcelain"); got != "" {
		t.Errorf("working tree must be clean after a validated commit, got: %s", got)
	}
}

// The failing-validation side of the same gate: a command that fails blocks
// the commit entirely, while the session still completes.
func TestGitedOneShotFailingValidationBlocksCommit(t *testing.T) {
	dir := t.TempDir()
	gitRunT(t, dir, "init", "-q", "-b", "main")
	gitRunT(t, dir, "config", "user.email", "test@example.com")
	gitRunT(t, dir, "config", "user.name", "Test")
	gitRunT(t, dir, "commit", "--allow-empty", "-q", "-m", "initial")

	mgr, sess := startGitedOneShot(t, "worker task", dir, "worker finished", &StartOptions{
		OneShot:          true,
		Git:              true,
		ValidateCommands: []string{"true", "false"},
	})

	current := completeOneShot(t, mgr, sess, "DATAWATCH_COMPLETE: worker finished")

	if current.State != StateComplete {
		t.Errorf("state = %v, want Complete — the session still finishes", current.State)
	}
	if !strings.HasPrefix(current.ValidationResult, "failed") {
		t.Errorf("ValidationResult = %q, want a failed prefix", current.ValidationResult)
	}
	if current.CommitSHA != "" {
		t.Errorf("CommitSHA = %q — a failing validation must block the commit", current.CommitSHA)
	}
	if got := gitOut(t, dir, "status", "--porcelain"); !strings.Contains(got, "hello.txt") {
		t.Errorf("the worker's file must remain visible and uncommitted:\n%s", got)
	}
}

// (4) The one-shot reaper still closes a finished Git-false one-shot —
// complete state, pane killed — but never commits, even in a repo.
func TestReaperClosesGitlessOneShotWithoutCommitting(t *testing.T) {
	dir := t.TempDir()
	gitRunT(t, dir, "init", "-q", "-b", "main")
	gitRunT(t, dir, "config", "user.email", "test@example.com")
	gitRunT(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("operator's work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRunT(t, dir, "add", "-A")
	gitRunT(t, dir, "commit", "-q", "-m", "operator commit")
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("operator's work\nin-flight\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	headBefore := gitHead(t, dir)
	statusBefore := gitStatusOf(t, dir)

	mgr, fake := newTestManagerWithFake(t)
	sess := &Session{
		ID: "reap1", FullID: "testhost-reap1", Hostname: "testhost",
		State: StateRunning, OneShot: true, Git: false,
		TmuxSession: "cs-reap1", ProjectDir: dir, Task: "worker task",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := mgr.store.Save(sess); err != nil {
		t.Fatal(err)
	}
	if err := fake.NewSessionWithSize("cs-reap1", 80, 24); err != nil {
		t.Fatal(err)
	}
	fake.Pane["cs-reap1"] = "worker output\nDATAWATCH_COMPLETE: all done\n"

	reaped := mgr.ReapFinishedOneShots()

	if len(reaped) != 1 || reaped[0] != "testhost-reap1" {
		t.Fatalf("reaped = %v, want [testhost-reap1]", reaped)
	}
	current, ok := mgr.store.Get("testhost-reap1")
	if !ok || current.State != StateComplete {
		t.Fatalf("session must be Complete after the reaper, got ok=%v state=%v", ok, current.State)
	}
	if fake.SessionExists("cs-reap1") {
		t.Error("the reaper must kill the finished one-shot's pane")
	}
	if current.CommitSHA != "" {
		t.Errorf("CommitSHA = %q — the reaper must not commit for a Git-false one-shot", current.CommitSHA)
	}
	if got := gitHead(t, dir); got != headBefore {
		t.Errorf("HEAD changed: before=%q after=%q", headBefore, got)
	}
	if got := gitStatusOf(t, dir); got != statusBefore {
		t.Errorf("working tree/index changed:\nbefore: %q\nafter:  %q", statusBefore, got)
	}
}
