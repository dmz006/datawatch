// Daemon-restart task-echo regression tests.
//
// After a daemon restart a claude-code PRD worker that was mid-run has its
// typed task (whose wrapped last line begins "DATAWATCH_COMPLETE: <one-line
// plain-text summary ...>") still on the pane. The task-echo suppression reads
// only the in-memory Manager.taskDeliveredAt, which is empty in a new Manager,
// while the persisted Session.TaskDeliveredAt is ignored, so the scanners read
// the echo as a completion and end the session ~18s after boot.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/session -run TestRestartEcho

package session

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

const (
	reOnlyPlaceholderPane = "▐▛███▜▌  Claude Code v2.0.14\n\n" +
		"DATAWATCH_COMPLETE: <one-line plain-text summary of what was done>\n"
	reRealSummaryPane = tdEchoed +
		"\n● Added the widget.\n" +
		"DATAWATCH_COMPLETE: added the widget and its tests\n"
)

// restartEchoSetup runs the pre-restart half: Manager A on dataDir starts a
// one-shot claude-code PRD session, wins the task claim (persisting
// TaskDeliveredAt) and the pane shows pane. Manager A is then stopped. It
// returns a new Manager B on the same data dir and fake tmux, plus a counter
// of B's onSessionEnd calls.
func restartEchoSetup(t *testing.T, pane string) (*Manager, *FakeTmux, *Session, *atomic.Int32) {
	t.Helper()
	dataDir := t.TempDir()
	a, err := NewManager("testhost", dataDir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	allowTestTemp(a)
	fake := a.WithFakeTmux()

	ctxA, cancelA := context.WithCancel(context.Background())
	sess, err := a.Start(ctxA, tdTask, "", t.TempDir(), &StartOptions{
		Backend: "claude-code",
		OneShot: true,
		PRDID:   "prd-restart-echo",
		LaunchFn: func(_ context.Context, _, tmuxSession, _, _ string) error {
			_ = fake.NewSessionWithSize(tmuxSession, 80, 24)
			_ = fake.PipeOutput(tmuxSession, "/dev/null")
			fake.mu.Lock()
			fake.Pane[tmuxSession] = tdIdlePane
			fake.mu.Unlock()
			return nil
		},
	})
	if err != nil {
		cancelA()
		t.Fatalf("Start: %v", err)
	}
	if !a.ClaimTaskDelivery(sess.FullID) {
		cancelA()
		t.Fatal("ClaimTaskDelivery lost the claim")
	}
	cur, _ := a.GetSession(sess.FullID)
	cur.State = StateRunning
	if err := a.SaveSession(cur); err != nil {
		cancelA()
		t.Fatalf("SaveSession: %v", err)
	}
	// Stop Manager A (the old daemon) before the new one boots.
	cancelA()
	a.mu.Lock()
	for _, c := range a.monitors {
		c()
	}
	a.mu.Unlock()

	fake.mu.Lock()
	fake.Pane[sess.TmuxSession] = pane
	fake.mu.Unlock()

	b, err := NewManager("testhost", dataDir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	allowTestTemp(b)
	b.tmux = fake
	b.mu.Lock()
	_, armed := b.taskDeliveredAt[sess.FullID]
	b.mu.Unlock()
	if armed {
		t.Fatal("new Manager unexpectedly has an in-memory taskDeliveredAt entry")
	}
	if loaded, ok := b.GetSession(sess.FullID); !ok || loaded.TaskDeliveredAt == nil {
		t.Fatal("persisted TaskDeliveredAt did not survive the reload")
	}
	ended := &atomic.Int32{}
	b.SetOnSessionEnd(func(*Session) { ended.Add(1) })
	return b, fake, sess, ended
}

// restartEchoObserve resumes monitors on b and watches the session for the
// scan window, returning early if it leaves running.
func restartEchoObserve(t *testing.T, b *Manager, sess *Session) *Session {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	b.ResumeMonitors(ctx)

	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		cur, ok := b.GetSession(sess.FullID)
		if !ok {
			t.Fatalf("session %s vanished", sess.FullID)
		}
		if cur.State != StateRunning {
			return cur
		}
		time.Sleep(50 * time.Millisecond)
	}
	cur, _ := b.GetSession(sess.FullID)
	return cur
}

// The echoed task (no real completion line) must not end the session after a
// restart.
func TestRestartEcho_TypedTaskEchoDoesNotComplete(t *testing.T) {
	b, _, sess, ended := restartEchoSetup(t, tdEchoed)
	cur := restartEchoObserve(t, b, sess)
	if cur.State != StateRunning {
		t.Errorf("post-restart task echo read as completion: state=%q, want %q", cur.State, StateRunning)
	}
	if n := ended.Load(); n != 0 {
		t.Errorf("post-restart task echo read as completion: onSessionEnd called %d times, want 0", n)
	}
}

// A bare placeholder line is never a completion, restart or not.
func TestRestartEcho_PlaceholderOnlyDoesNotComplete(t *testing.T) {
	b, _, sess, ended := restartEchoSetup(t, reOnlyPlaceholderPane)
	cur := restartEchoObserve(t, b, sess)
	if cur.State != StateRunning {
		t.Errorf("post-restart placeholder line read as completion: state=%q, want %q", cur.State, StateRunning)
	}
	if n := ended.Load(); n != 0 {
		t.Errorf("post-restart placeholder line read as completion: onSessionEnd called %d times, want 0", n)
	}
}

// A real model summary after the echoed task must still complete the session.
func TestRestartEcho_RealSummaryAfterEchoCompletes(t *testing.T) {
	b, _, sess, ended := restartEchoSetup(t, reRealSummaryPane)
	cur := restartEchoObserve(t, b, sess)
	if cur.State != StateComplete {
		t.Errorf("real completion after restart not detected: state=%q, want %q", cur.State, StateComplete)
	}
	// The end callback runs just after the state flips; wait for it on a loaded runner.
	for wait := time.Now().Add(3 * time.Second); ended.Load() == 0 && time.Now().Before(wait); {
		time.Sleep(25 * time.Millisecond)
	}
	if n := ended.Load(); n == 0 {
		t.Error("onSessionEnd was not called for a real completion after restart")
	}
}
