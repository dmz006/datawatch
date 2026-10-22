// Task delivery timing against the main input prompt.
//
// DeliverOneShotTask types a task only once the claude-code main prompt is
// visible, retrying a missed prompt with a linear backoff and releasing the
// claim after the last attempt. These tests drive the pane of the fake
// terminal multiplexer through three timings: the prompt appears late, the
// prompt is drawn behind passive banner / update / doctor lines, and the
// prompt never appears.
//
// The per-attempt prompt window is now explicit on each
// DeliverOneShotTask call (these tests pass short windows for speed);
// TestTaskDelivery_MainPromptNeverAppears burns ~2s per attempt with
// maxAttempts=2.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/session -run TestTaskDelivery_

package session

import (
	"errors"
	"testing"
	"time"
)

// tdrBannerPane is the ready main prompt drawn behind passive lines: a product
// banner, an update notice and a doctor hint. None of them is an active choice
// dialog, so the prompt must still be recognised and the task delivered.
const tdrBannerPane = "▐▛███▜▌  Claude Code v2.0.14\n" +
	"✔ Update installed · Restart to apply: /restart\n" +
	"Found 2 setting issues · run /doctor for details\n\n" +
	"❯ Try \"how does this work?\"\n  ? for shortcuts"

// tdrSaveWaiting saves a one-shot claude-code session that is waiting for
// input on the fake multiplexer. No screen watcher is started, so the pane
// only changes when the test changes it.
func tdrSaveWaiting(t *testing.T, mgr *Manager, id string) *Session {
	t.Helper()
	sess := &Session{
		ID:          id,
		FullID:      "testhost-" + id,
		TmuxSession: "cs-" + id,
		OneShot:     true,
		State:       StateWaitingInput,
		UpdatedAt:   time.Now(),
	}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	return sess
}

// tdrSetPane replaces the visible pane content of one fake tmux session.
func tdrSetPane(f *FakeTmux, tmuxSession, pane string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Pane[tmuxSession] = pane
}

// (1) The disclaimer is on screen at first and the ready main prompt appears
// a couple of seconds later. Delivery must succeed without typing the task
// more than once.
func TestTaskDelivery_MainPromptAppearsLate(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := tdrSaveWaiting(t, mgr, "tdr1")
	tdrSetPane(fake, sess.TmuxSession, tdDisclaimerPane)
	if mgr.MainPromptReady(sess.FullID) {
		t.Fatal("disclaimer pane read as a ready main prompt")
	}

	lateReady := make(chan struct{})
	go func() {
		defer close(lateReady)
		time.Sleep(2 * time.Second)
		tdrSetPane(fake, sess.TmuxSession, tdIdlePane)
	}()

	if !mgr.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("ClaimTaskDelivery lost the claim")
	}
	err := mgr.DeliverOneShotTask(sess.FullID, tdTask, 3, 10*time.Second, 100*time.Millisecond)
	<-lateReady
	if err != nil {
		t.Fatalf("DeliverOneShotTask: %v", err)
	}
	if !mgr.TaskDelivered(sess.FullID) {
		t.Error("TaskDelivered = false after a successful delivery")
	}
	if got := taskSends(fake, sess.TmuxSession, tdTask); got != 1 {
		t.Errorf("task sent %d times, want exactly 1; calls: %+v", got, fake.Calls)
	}
}

// (2) The ready main prompt is visible behind a banner, an update notice and a
// doctor hint. The prompt must be recognised and the task delivered once.
func TestTaskDelivery_MainPromptBehindBanner(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := tdrSaveWaiting(t, mgr, "tdr2")
	tdrSetPane(fake, sess.TmuxSession, tdrBannerPane)
	if !mgr.MainPromptReady(sess.FullID) {
		t.Fatal("MainPromptReady = false behind passive banner/notice/doctor lines")
	}

	if !mgr.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("ClaimTaskDelivery lost the claim")
	}
	if err := mgr.DeliverOneShotTask(sess.FullID, tdTask, 2, 5*time.Second, 100*time.Millisecond); err != nil {
		t.Fatalf("DeliverOneShotTask: %v", err)
	}
	if !mgr.TaskDelivered(sess.FullID) {
		t.Error("TaskDelivered = false after a successful delivery")
	}
	if got := taskSends(fake, sess.TmuxSession, tdTask); got != 1 {
		t.Errorf("task sent %d times, want exactly 1; calls: %+v", got, fake.Calls)
	}
}

// (3) The session stays on the startup disclaimer for the whole window. Every
// attempt must fail, the task must never be typed, and the claim must be
// released so a later ready can win it again.
func TestTaskDelivery_MainPromptNeverAppears(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := tdrSaveWaiting(t, mgr, "tdr3")
	tdrSetPane(fake, sess.TmuxSession, tdDisclaimerPane)

	if !mgr.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("ClaimTaskDelivery lost the claim")
	}
	err := mgr.DeliverOneShotTask(sess.FullID, tdTask, 2, 2*time.Second, 100*time.Millisecond)
	if !errors.Is(err, ErrTaskUndelivered) {
		t.Fatalf("DeliverOneShotTask error = %v, want ErrTaskUndelivered", err)
	}
	if mgr.TaskDelivered(sess.FullID) {
		t.Error("TaskDelivered = true although the task was never typed")
	}
	if got := taskSends(fake, sess.TmuxSession, tdTask); got != 0 {
		t.Errorf("task sent %d times, want 0", got)
	}
	if !mgr.ClaimTaskDelivery(sess.FullID) {
		t.Error("claim was not released; a later ready cannot deliver the task")
	}
}
