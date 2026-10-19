// One-shot session completion flow, driven end to end with the fake tmux /
// fake LLM launch harness used by the other manager tests. A one-shot
// session that finishes must leave a clean working tree with the worker's
// changes committed, and the commit subject carries the worker's
// DATAWATCH_COMPLETE: summary (or falls back to the task title when the
// worker printed none). The commit must exist before the state is
// published as Complete, so any poller that reads state sees the commit.

package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// driveOneShotCompletion starts a one-shot session whose fake launch
// function writes a worker file into the repo and emits `logBody` on the
// session log. It then feeds the `markerLine` through the same
// processOutputLine path a real log tail would, completing the session.
func driveOneShotCompletion(t *testing.T, task, logBody, markerLine string) (sess *Session, dir string) {
	t.Helper()
	dir = t.TempDir()
	gitRunT(t, dir, "init", "-q", "-b", "main")
	gitRunT(t, dir, "config", "user.email", "test@example.com")
	gitRunT(t, dir, "config", "user.name", "Test")
	gitRunT(t, dir, "commit", "--allow-empty", "-q", "-m", "initial")

	mgr, fake := newTestManagerWithFake(t)
	mgr.SetLLMBackend("fake-flow", func(_ context.Context, _, tmuxSession, projectDir, logFile string) error {
		// The worker does its job: drops a file in the repo and prints
		// its log output.
		if logBody != "" {
			if err := os.WriteFile(logFile, []byte(logBody), 0o644); err != nil {
				t.Errorf("fake launch: write log: %v", err)
			}
		}
		if err := os.WriteFile(filepath.Join(projectDir, "hello.txt"), []byte("hello\n"), 0o644); err != nil {
			t.Errorf("fake launch: write worker file: %v", err)
		}
		_ = fake.NewSessionWithSize(tmuxSession, 80, 24)
		_ = fake.PipeOutput(tmuxSession, "/dev/null")
		return nil
	})

	var err error
	sess, err = mgr.Start(context.Background(), task, "", dir, &StartOptions{OneShot: true, ValidateCommands: []string{"true"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Observe the repo at the moment the session is published as Complete —
	// by then the worker's commit must already exist.
	subjectAtCompleteSeen := ""
	mgr.SetOnSessionEnd(func(*Session) {
		subjectAtCompleteSeen = gitOut(t, dir, "log", "-1", "--pretty=%s")
	})

	var lastOutputTime time.Time
	var pendingLines []string
	var lastPromptMatch time.Time
	mgr.processOutputLine(context.Background(), sess, NewProjectGit(dir), markerLine, &lastOutputTime, &pendingLines, &lastPromptMatch, func() *Tracker { return nil })

	current, ok := mgr.store.Get(sess.FullID)
	if !ok || current.State != StateComplete {
		t.Fatalf("one-shot session must reach StateComplete after the marker line, got %+v", current)
	}

	sha := gitOut(t, dir, "log", "-1", "--pretty=%H")
	if current.CommitSHA != sha {
		t.Errorf("CommitSHA %q — want the HEAD sha %q", current.CommitSHA, sha)
	}
	if subjectAtCompleteSeen == "" {
		t.Error("no commit observed at the moment the session was published as Complete")
	}
	if got := gitOut(t, dir, "status", "--porcelain"); got != "" {
		t.Errorf("working tree must be clean after completion, got: %s", got)
	}
	out := gitOut(t, dir, "log", "-1", "--name-only", "--pretty=")
	if !strings.Contains(out, "hello.txt") {
		t.Errorf("HEAD commit must include the worker's file, got:\n%s", out)
	}
	return sess, dir
}

func TestOneShotCompletion_SummaryBecomesCommitSubject(t *testing.T) {
	const summary = "created hello file"
	_, dir := driveOneShotCompletion(t, "create a hello file",
		"wrote hello.txt\nDATAWATCH_COMPLETE: "+summary+"\n",
		"DATAWATCH_COMPLETE: "+summary)

	subject := gitOut(t, dir, "log", "-1", "--pretty=%s")
	if subject != summary {
		t.Fatalf("commit subject %q — want the worker summary %q", subject, summary)
	}
}

func TestOneShotCompletion_NoSummaryFallsBackToSessionWork(t *testing.T) {
	const task = "write the report"
	_, dir := driveOneShotCompletion(t, task,
		"working on the report, no summary emitted\n",
		"DATAWATCH_COMPLETE:")

	// The worker prompt must never become the subject; the fallback names the session.
	subject := gitOut(t, dir, "log", "-1", "--pretty=%s")
	if !strings.HasPrefix(subject, "session work") || strings.Contains(subject, task) {
		t.Fatalf("commit subject %q — want the session-work fallback without the task text", subject)
	}
}
