// A session that ends while datawatch waits to see its typed task. The end is
// not the task arriving: only the task on screen is.
package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// teeDeliver types tdTask into a one-shot claude-code session that goes to
// complete during the echo wait, after setup has arranged the screen.
func teeDeliver(t *testing.T, id string, setup func(f *FakeTmux, tmuxSession string)) (*Manager, *Session, error) {
	t.Helper()
	mgr, fake := newTestManagerWithFake(t)
	sess := tdrSaveWaiting(t, mgr, id)
	mgr.store.Mutate(func() { sess.BackendFamily, sess.DaemonDeliversTask, sess.Task = "claude-code", true, tdTask })
	tdrSetPane(fake, sess.TmuxSession, tdIdlePane)
	fake.NoEcho = true // the typed text does not show up in the input box
	c := tdrInstallFakeClock(t, mgr, time.Unix(0, 0).UTC())
	ended := false
	mgr.sleepFn = func(d time.Duration) {
		if !ended && taskSends(fake, sess.TmuxSession, tdTask) > 0 {
			ended = true
			setup(fake, sess.TmuxSession)
			mgr.store.Mutate(func() { sess.State = StateComplete })
		}
		c.sleep(d)
	}
	if !mgr.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("ClaimTaskDelivery lost the claim")
	}
	err := mgr.DeliverOneShotTask(sess.FullID, tdTask, 2, 3*time.Second, 100*time.Millisecond)
	if !ended {
		t.Fatal("setup: the task was never typed")
	}
	return mgr, sess, err
}

func TestTaskDelivery_EndWithoutEchoIsNotDelivery(t *testing.T) {
	mgr, sess, err := teeDeliver(t, "tee1", func(f *FakeTmux, tmuxSession string) {
		// The agent left; the shell's exit line is all there is.
		tdrSetPane(f, tmuxSession, "DATAWATCH_COMPLETE: claude done\n$ ")
	})
	if !errors.Is(err, ErrTaskUndelivered) {
		t.Errorf("DeliverOneShotTask = %v, want ErrTaskUndelivered", err)
	}
	snap := mgr.store.Snapshot(sess)
	if snap.TaskVerifiedAt != nil || mgr.TaskDelivered(sess.FullID) {
		t.Errorf("a task nobody saw arrive reads as delivered: verified=%v delivered=%v",
			snap.TaskVerifiedAt, mgr.TaskDelivered(sess.FullID))
	}
}

func TestTaskDelivery_EndWithTaskOnScreenIsDelivery(t *testing.T) {
	mgr, sess, err := teeDeliver(t, "tee2", func(f *FakeTmux, tmuxSession string) {
		// A quick worker: the task scrolled up and the session is already done.
		tdrSetPane(f, tmuxSession, "❯ Implement the widget.\n● done\nDATAWATCH_COMPLETE: wrote the widget")
	})
	if err != nil {
		t.Errorf("DeliverOneShotTask = %v", err)
	}
	if snap := mgr.store.Snapshot(sess); snap.TaskVerifiedAt == nil {
		t.Error("a task seen on screen is not recorded as delivered")
	}
}

// A restart appends to the same output file. At boot an answer recorded by an
// earlier start is not proof that the task arrived on this one.
func TestBootClaim_EarlierStartsAnswerIsNotProof(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	logFile := filepath.Join(t.TempDir(), "output.log")
	const earlier = "❯ the earlier turn\n⏺ an answer from the earlier start\n"
	if err := os.WriteFile(logFile, []byte(earlier), 0o600); err != nil {
		t.Fatal(err)
	}
	sess := &Session{ID: "tee3", FullID: "testhost-tee3", Hostname: "testhost", TmuxSession: "cs-testhost-tee3",
		State: StateKilled, OneShot: true, BackendFamily: "claude-code", Model: "sonnet",
		DaemonDeliversTask: true, Task: tdTask, ProjectDir: t.TempDir(), LogFile: logFile, UpdatedAt: time.Now()}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatal(err)
	}
	mgr.SetProfileResumeFn(func(*Session) ResumeLaunchFunc {
		return func(context.Context, string, string, string, string, string) error { return nil }
	})
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := mgr.Restart(ctx, sess.FullID); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	cancel()
	mgr.WaitBackground()
	tdrSetPane(fake, sess.TmuxSession, tdIdlePane)
	if !mgr.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("ClaimTaskDelivery lost the claim")
	}
	if kind, _, why := mgr.readTaskClaim(sess.FullID); kind != claimBoxEmpty {
		t.Errorf("claim read as %v (%s), want an empty box with no sign of the task", kind, why)
	}
	// An answer this start recorded still counts.
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, werr := f.WriteString("❯ the task\n⏺ working on it\n")
	if cerr := f.Close(); werr != nil || cerr != nil {
		t.Fatal(werr, cerr)
	}
	if kind, _, why := mgr.readTaskClaim(sess.FullID); kind != claimDelivered {
		t.Errorf("claim read as %v (%s), want delivered", kind, why)
	}
}
