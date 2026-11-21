// Dev-channels confirmation + task arrival.
//
// Claude Code's "Loading development channels" confirmation must be answered
// by the launcher even when its first Enter is swallowed and the pane then
// stays static, and a task is only counted as delivered once its text is
// visible in the session. Three launches: with the confirmation (first Enter
// lost), without it, and a task that never appears.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/session -run TestTaskDelivery_DevChannels
package session

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// swallowFirstEnterTmux drops the first Enter keystroke (as when it lands
// while the TUI is still rendering) and clears the dialog on the second.
type swallowFirstEnterTmux struct {
	*FakeTmux
	mu     sync.Mutex
	enters int
}

func (s *swallowFirstEnterTmux) SendKeys(session, keys string) error {
	if keys == "Enter" {
		s.mu.Lock()
		s.enters++
		n := s.enters
		s.mu.Unlock()
		_ = s.record("send-keys", session, keys, "Enter")
		if n >= 2 {
			tdrSetPane(s.FakeTmux, session, tdIdlePane)
		}
		return nil
	}
	return s.FakeTmux.SendKeys(session, keys)
}

func (s *swallowFirstEnterTmux) enterCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enters
}

// With the confirmation on a static pane and the first Enter lost, the
// launcher re-answers it, waits for the input line, then types the task once
// and sees it arrive.
func TestTaskDelivery_DevChannelsConfirmationAnsweredThenTaskTyped(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sw := &swallowFirstEnterTmux{FakeTmux: fake}
	mgr.tmux = sw
	sess := tdrSaveWaiting(t, mgr, "dc1")
	sess.BackendFamily = "claude-code"
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatal(err)
	}
	tdrSetPane(fake, sess.TmuxSession, tdDisclaimerPane)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	capCtx, stopCapture := context.WithCancel(ctx)
	defer stopCapture()
	mgr.StartScreenCapture(capCtx, sess.FullID, 20)

	// Once the dialog is gone the session settles back to waiting_input at the
	// idle prompt; stop the watcher (so it cannot flip the state back) and
	// replay that.
	go func() {
		for ctx.Err() == nil {
			if sw.enterCount() >= 2 {
				time.Sleep(100 * time.Millisecond)
				stopCapture()
				mgr.MarkWaitingInput(sess.FullID, "❯")
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()

	if !mgr.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("ClaimTaskDelivery lost the claim")
	}
	if err := mgr.DeliverOneShotTask(sess.FullID, tdTask, 1, 20*time.Second, 100*time.Millisecond); err != nil {
		cur, _ := mgr.GetSession(sess.FullID)
		pane, _ := fake.CapturePaneVisible(sess.TmuxSession)
		t.Fatalf("DeliverOneShotTask: %v (state %v, enters %d, pane %q)", err, cur.State, sw.enterCount(), pane)
	}
	cancel()
	if got := sw.enterCount(); got != 2 {
		t.Errorf("confirmation answered %d times, want 2 (first swallowed, second re-sent on the static pane)", got)
	}
	if got := taskSends(fake, sess.TmuxSession, tdTask); got != 1 {
		t.Errorf("task sent %d times, want exactly 1", got)
	}
	if !mgr.TaskDelivered(sess.FullID) {
		t.Error("TaskDelivered = false after the task appeared")
	}
}

// With no confirmation the task is typed straight away, once.
func TestTaskDelivery_DevChannelsNoConfirmationTaskTyped(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := tdrSaveWaiting(t, mgr, "dc2")
	tdrSetPane(fake, sess.TmuxSession, tdIdlePane)
	if !mgr.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("ClaimTaskDelivery lost the claim")
	}
	if err := mgr.DeliverOneShotTask(sess.FullID, tdTask, 1, 5*time.Second, 100*time.Millisecond); err != nil {
		t.Fatalf("DeliverOneShotTask: %v", err)
	}
	if got := taskSends(fake, sess.TmuxSession, tdTask); got != 1 {
		t.Errorf("task sent %d times, want exactly 1", got)
	}
	if got := fake.Count("send-keys") + fake.Count("send-keys-settle"); got != 1 {
		t.Errorf("%d keystroke sends, want only the task: %+v", got, fake.Calls)
	}
}

// A task that is typed but never shows up fails the delivery with a stated
// reason instead of leaving the session in waiting_input.
func TestTaskDelivery_DevChannelsTaskNeverAppears(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	fake.NoEcho = true
	sess := tdrSaveWaiting(t, mgr, "dc3")
	tdrInstallFakeClock(t, mgr, time.Unix(0, 0).UTC())
	tdrSetPane(fake, sess.TmuxSession, tdIdlePane)

	var reason string
	mgr.SetOnTaskDeliveryFailed(func(_ string, r string) { reason = r })

	if !mgr.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("ClaimTaskDelivery lost the claim")
	}
	err := mgr.DeliverOneShotTask(sess.FullID, tdTask, 2, 5*time.Second, 100*time.Millisecond)
	if !errors.Is(err, ErrTaskUndelivered) {
		t.Fatalf("error = %v, want ErrTaskUndelivered", err)
	}
	if !strings.Contains(reason, "task text never appeared") {
		t.Errorf("reason = %q, want it to say the task text never appeared", reason)
	}
	got, _ := mgr.GetSession(sess.FullID)
	if got == nil || got.State == StateWaitingInput {
		t.Errorf("session left in %v, want a terminal state", got)
	}
	if mgr.TaskDelivered(sess.FullID) {
		t.Error("TaskDelivered = true for a task that never appeared")
	}
}
