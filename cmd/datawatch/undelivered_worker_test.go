package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/session"
)

// A restarted Automaton worker with automatic commits on, over a clean tree,
// that leaves before datawatch ever typed its task. The completion commit finds
// nothing to commit but stores the launch command's own exit line as the
// worker's summary, and the response captured on the earlier launch is still on
// the record. Neither is the task arriving: the worker is never done.
func TestRestartedWorkerNeverGivenItsTaskIsNotDone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.name=made-up", "-c", "user.email=made-up@example.invalid", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	data := t.TempDir()
	mgr, err := session.NewManager("testhost", data, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	fake := mgr.WithFakeTmux()
	// The relaunch starts nothing: the claude backend would run real tmux.
	var relaunchedWith string
	mgr.SetProfileResumeFn(func(*session.Session) session.ResumeLaunchFunc {
		return func(_ context.Context, task, _, _, _, _ string) error {
			relaunchedWith = task
			return nil
		}
	})

	logFile := filepath.Join(data, "output.log")
	const id = "testhost-aaaa"
	if err := mgr.SaveSession(&session.Session{
		ID: "aaaa", FullID: id, Hostname: "testhost", TmuxSession: "cs-" + id,
		State: session.StateKilled, UpdatedAt: time.Now(), CreatedAt: time.Now(),
		BackendFamily: "claude-code", Model: "sonnet", OneShot: true, Git: true,
		DaemonDeliversTask: true, Task: "TASK: create the made-up file",
		PRDID: "bbbb", TaskID: "cccc", ProjectDir: dir, WriteScope: []string{dir},
		LogFile: logFile, TrackingDir: t.TempDir(),
		// Left by the earlier launch: the screen datawatch captured.
		LastResponse: "a screen captured on the earlier launch",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := mgr.Restart(context.Background(), id); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if relaunchedWith != "" {
		t.Fatalf("setup: the relaunch carried the task on its command line: %q", relaunchedWith)
	}
	// The agent leaves: the shell prints the launch command's exit line and
	// the terminal goes.
	if err := os.WriteFile(logFile, []byte("DATAWATCH_COMPLETE: claude done\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fake.KillSession("cs-" + id); err != nil {
		t.Fatal(err)
	}
	mgr.ReconnectBackends(func(*session.Session, *session.BackendState) {})

	s, ok := mgr.GetSession(id)
	if !ok {
		t.Fatal("session gone")
	}
	// The record the engine is handed, as the defect produces it.
	if s.State != session.StateComplete || s.CompletionSummary != "claude done" || s.CommitSHA != "" {
		t.Fatalf("setup: state=%s summary=%q commit=%q", s.State, s.CompletionSummary, s.CommitSHA)
	}
	if mgr.TaskDelivered(id) {
		t.Fatal("setup: the task reads as delivered")
	}

	err = undeliveredWorkerFailure(s, mgr.TaskDelivered(id), mgr.TaskDeliveryFailure(id))
	if !errors.Is(err, autonomouspkg.ErrSessionKilledBeforeDelivery) {
		t.Fatalf("a worker that never got its task can be marked done: err = %v", err)
	}
	if !errors.Is(neverGivenTaskFailure(s, ""), autonomouspkg.ErrSessionKilledBeforeDelivery) {
		t.Error("the checkpoint verdict would still mark it done")
	}
}

// A claim is saved before anything is typed, so for a task datawatch types
// itself a claim with no proof is not the task arriving.
func TestUnprovenDeliveryIsNotTheTaskArriving(t *testing.T) {
	now := time.Now()
	worker := func(edit func(*session.Session)) *session.Session {
		s := &session.Session{FullID: "h-1", BackendFamily: "claude-code", OneShot: true,
			State: session.StateComplete, DaemonDeliversTask: true, Task: "the task",
			CompletionSummary: "wrote the file", CommitSHA: "abc123"}
		edit(s)
		return s
	}
	for _, c := range []struct {
		name    string
		s       *session.Session
		never   bool
		claimed bool
	}{
		{"claimed, never proven", worker(func(s *session.Session) { s.TaskDeliveredAt, s.TaskDeliveryUnverified = &now, true }), true, true},
		{"never claimed", worker(func(*session.Session) {}), true, false},
		{"verified", worker(func(s *session.Session) { s.TaskDeliveredAt, s.TaskVerifiedAt = &now, &now }), false, true},
		{"delivered before proof was recorded", worker(func(s *session.Session) { s.TaskDeliveredAt = &now }), false, true},
		{"task on the command line", worker(func(s *session.Session) { s.DaemonDeliversTask = false }), false, false},
		{"another agent", worker(func(s *session.Session) { s.BackendFamily = "opencode" }), false, false},
		{"remote worker", worker(func(s *session.Session) { s.AgentID = "agent-1" }), false, false},
		{"job without a terminal", worker(func(s *session.Session) { s.Subprocess = true }), false, false},
	} {
		if got := neverGivenTaskFailure(c.s, "") != nil; got != c.never {
			t.Errorf("%s: neverGivenTaskFailure = %v, want %v", c.name, got, c.never)
		}
		// With a commit and its own summary the older check alone would let
		// every one of these through.
		if got := undeliveredWorkerFailure(c.s, c.claimed, "") != nil; got != c.never {
			t.Errorf("%s: undeliveredWorkerFailure = %v, want %v", c.name, got, c.never)
		}
	}
	err := neverGivenTaskFailure(worker(func(*session.Session) {}), "task delivery failed: made-up reason")
	if err == nil || !strings.Contains(err.Error(), "made-up reason") ||
		!strings.Contains(err.Error(), autonomouspkg.LaunchFailureReason) {
		t.Errorf("err does not say why: %v", err)
	}
	if neverGivenTaskFailure(nil, "") != nil || undeliveredWorkerFailure(nil, false, "") != nil {
		t.Error("no session became a launch failure")
	}
}

// The launch command prints its own exit line whenever Claude Code leaves, so
// it is not the worker saying it finished.
func TestLauncherExitLineIsNotTheWorkersMarker(t *testing.T) {
	s := &session.Session{FullID: "h-1", BackendFamily: "claude-code", OneShot: true,
		State: session.StateComplete, CompletionSummary: "claude done", LastResponse: "a captured screen"}
	if undeliveredWorkerFailure(s, false, "") == nil {
		t.Error("the exit line and a captured screen passed for finished work")
	}
	s.CommitSHA = "abc123"
	if undeliveredWorkerFailure(s, false, "") == nil {
		t.Error("the exit line and a commit passed for finished work")
	}
	s.CompletionSummary = "wrote the file"
	if err := undeliveredWorkerFailure(s, false, ""); err != nil {
		t.Errorf("a worker with its own summary and a commit lost the output path: %v", err)
	}
}

// Both checks are asked before the verifier can accept anything: the first
// ahead of the checkpoint verdict, the second before the tree is verified.
func TestVerifierAsksForDeliveryBeforeAccepting(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	never := strings.Index(body, "neverGivenTaskFailure(ls, ")
	accept := strings.Index(body, "return *doneVerdict, nil")
	older := strings.Index(body, "undeliveredWorkerFailure(ls, ")
	verify := strings.Index(body, "NoteVerificationStarted(task.ID)")
	if never < 0 || accept < 0 || older < 0 || verify < 0 || never > accept || accept > older || older > verify {
		t.Errorf("order in main.go: never=%d accept=%d older=%d verify=%d", never, accept, older, verify)
	}
}
