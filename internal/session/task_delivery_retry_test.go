// Task delivery timing against the main input prompt.
//
// DeliverOneShotTask types a task only once the claude-code main prompt is
// visible, retrying a missed prompt with a linear backoff and releasing the
// claim after the last attempt. These tests drive the pane of the fake
// terminal multiplexer through several timings: the prompt appears late, the
// prompt appears well past the first per-attempt window, the prompt is drawn
// behind passive banner / update / doctor lines, the prompt never appears, or
// the session exits before (or during) the wait.
//
// The budget-burning cases run on a fake clock (tdrFakeClock below): the
// whole retry budget is driven with virtual time, so none of them burn wall
// time. Only TestTaskDelivery_MainPromptAppearsLate still uses the one real
// 2s sleep its harness needs to change the pane from another goroutine; every
// other test here and in task_delivery_failure_test.go is fake-clock.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/session -run TestTaskDelivery_

package session

import (
	"errors"
	"sync"
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

// tdrFakeClock lets a test drive DeliverOneShotTask's Manager.now()/sleep
// seams with virtual time: mgr.clock reads its current time and mgr.sleepFn
// jumps the clock forward by the requested duration without blocking, firing
// any callback scheduled to run at or before the new time (used to change the
// pane at a fixed point of the budget, e.g. the main prompt appearing at
// t+70s or the session exiting at t+10s).
type tdrFakeClock struct {
	mu   sync.Mutex
	cur  time.Time
	pend []tdrPending
}

type tdrPending struct {
	at time.Time
	fn func()
}

func newTdrFakeClock(start time.Time) *tdrFakeClock {
	return &tdrFakeClock{cur: start}
}

func (c *tdrFakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cur
}

// elapsed reports how far past start the clock has advanced.
func (c *tdrFakeClock) elapsed(start time.Time) time.Duration {
	return c.now().Sub(start)
}

// schedule registers fn to run (from the clock's own sleep) when the virtual
// time reaches or passes at.
func (c *tdrFakeClock) schedule(at time.Time, fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pend = append(c.pend, tdrPending{at: at, fn: fn})
}

// sleep advances the virtual clock by d and fires every callback whose
// threshold has been reached, in earliest-deadline order, each at its own
// scheduled time.
func (c *tdrFakeClock) sleep(d time.Duration) {
	c.mu.Lock()
	target := c.cur.Add(d)
	for {
		idx := -1
		for i, p := range c.pend {
			if !p.at.After(target) && (idx < 0 || p.at.Before(c.pend[idx].at) || (p.at.Equal(c.pend[idx].at) && i < idx)) {
				idx = i
			}
		}
		if idx < 0 {
			break
		}
		run := c.pend[idx]
		c.pend = append(c.pend[:idx], c.pend[idx+1:]...)
		c.cur = run.at
		c.mu.Unlock()
		run.fn()
		c.mu.Lock()
	}
	c.cur = target
	c.mu.Unlock()
}

// tdrInstallFakeClock wires a fresh fake clock into the manager's now/sleep
// seams and returns it.
func tdrInstallFakeClock(t *testing.T, mgr *Manager, start time.Time) *tdrFakeClock {
	t.Helper()
	c := newTdrFakeClock(start)
	mgr.clock = c.now
	mgr.sleepFn = c.sleep
	t.Cleanup(func() {
		mgr.clock = nil
		mgr.sleepFn = nil
	})
	return c
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
	start := time.Unix(0, 0).UTC()
	c := tdrInstallFakeClock(t, mgr, start)
	err := mgr.DeliverOneShotTask(sess.FullID, tdTask, 2, 3*time.Second, 100*time.Millisecond)
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
	// No real waiting: with a 3s window, 2 attempts and a 100ms first backoff
	// the full budget is 3 + 0.1 + 3 = 6.1s of virtual time.
	if elapsed := c.elapsed(start); elapsed < 6*time.Second || elapsed > 7*time.Second {
		t.Errorf("virtual time consumed = %v, want ~6.1s (3s window x2 + 100ms backoff)", elapsed)
	}
}

// The ready main prompt only appears after virtual time passes 70s — past the
// first 60s per-attempt window, so attempt one times out, the linear backoff
// fires, and attempt two picks the prompt up at 70s. Delivery succeeds, the
// task is typed exactly once, and the virtual time burned stays far under the
// full 3-attempt budget.
func TestTaskDelivery_PromptAfterSeventySecondsDelivered(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := tdrSaveWaiting(t, mgr, "tdr70")
	start := time.Unix(0, 0).UTC()
	c := tdrInstallFakeClock(t, mgr, start)
	tdrSetPane(fake, sess.TmuxSession, tdDisclaimerPane)
	// The ready main prompt appears once virtual time passes 70s.
	c.schedule(start.Add(70*time.Second), func() {
		tdrSetPane(fake, sess.TmuxSession, tdIdlePane)
	})

	// Default budget: 60s window (pass 0 for the default), 3 attempts, 5s
	// linear backoff.
	if !mgr.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("ClaimTaskDelivery lost the claim")
	}
	err := mgr.DeliverOneShotTask(sess.FullID, tdTask, 3, 0, 5*time.Second)
	if err != nil {
		t.Fatalf("DeliverOneShotTask: %v", err)
	}
	if !mgr.TaskDelivered(sess.FullID) {
		t.Error("TaskDelivered = false after a successful delivery")
	}
	if got := taskSends(fake, sess.TmuxSession, tdTask); got != 1 {
		t.Errorf("task sent %d times, want exactly 1; calls: %+v", got, fake.Calls)
	}
	// Full budget = 3 x 60s windows + 5s x 1 + 5s x 2 backoff = 195s. The
	// prompt is picked up at 70s (60s + 5s backoff + 5s), well under budget.
	elapsed := c.elapsed(start)
	if elapsed >= 195*time.Second {
		t.Errorf("delivery burned %v of virtual time, want under the full 195s budget", elapsed)
	}
	if elapsed < 70*time.Second {
		t.Errorf("delivery finished at %v, but the prompt only appears at 70s", elapsed)
	}
}

// The ready main prompt never appears, so every attempt times out. The full
// budget is burned (>= 180s of virtual time, not much more), the task is never
// typed, the claim is released, and the failure callback fires exactly once.
func TestTaskDelivery_PromptNeverAppearsFailsAfterFullBudget(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := tdrSaveWaiting(t, mgr, "tdrnever")
	start := time.Unix(0, 0).UTC()
	c := tdrInstallFakeClock(t, mgr, start)
	tdrSetPane(fake, sess.TmuxSession, tdDisclaimerPane)

	failures := 0
	mgr.SetOnTaskDeliveryFailed(func(string, string) { failures++ })

	if !mgr.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("ClaimTaskDelivery lost the claim")
	}
	err := mgr.DeliverOneShotTask(sess.FullID, tdTask, 3, 60*time.Second, 5*time.Second)
	if !errors.Is(err, ErrTaskUndelivered) {
		t.Fatalf("DeliverOneShotTask error = %v, want ErrTaskUndelivered", err)
	}
	if mgr.TaskDelivered(sess.FullID) {
		t.Error("TaskDelivered = true although the task was never typed")
	}
	if got := taskSends(fake, sess.TmuxSession, tdTask); got != 0 {
		t.Errorf("task sent %d times, want 0", got)
	}
	if failures != 1 {
		t.Errorf("onTaskDeliveryFailed fired %d times, want exactly 1", failures)
	}
	if !mgr.ClaimTaskDelivery(sess.FullID) {
		t.Error("claim was not released; a later ready cannot deliver the task")
	}
	elapsed := c.elapsed(start)
	// Full budget = 3 x 60s windows + 5s x 1 + 5s x 2 backoff = 195s; the last
	// attempt must burn its whole window, so >= 180s (three full windows) and
	// not much more than the budget total.
	if elapsed < 180*time.Second || elapsed > 200*time.Second {
		t.Errorf("virtual time consumed = %v, want between 180s (full budget) and ~195s", elapsed)
	}
}

// The worker session has already exited — killed outright before delivery
// starts (covering both a terminal session state and the fake multiplexer
// session being gone), or killed mid-wait after 10 virtual seconds. In every
// case DeliverOneShotTask fails immediately: no backoff sleep, no further
// attempts, the task is never typed, the claim is released, and the virtual
// time burned stays far below even a single per-attempt window.
func TestTaskDelivery_ExitedSessionFailsImmediately(t *testing.T) {
	t.Run("killed_before_delivery", func(t *testing.T) {
		mgr, fake := newTestManagerWithFake(t)
		sess := tdrSaveWaiting(t, mgr, "tdrx1")
		start := time.Unix(0, 0).UTC()
		c := tdrInstallFakeClock(t, mgr, start)
		tdrSetPane(fake, sess.TmuxSession, tdDisclaimerPane)
		// The session is already terminal before delivery starts.
		if cur, ok := mgr.GetSession(sess.FullID); ok {
			cur.State = StateKilled
			if err := mgr.SaveSession(cur); err != nil {
				t.Fatalf("SaveSession: %v", err)
			}
		}
		if !mgr.ClaimTaskDelivery(sess.FullID) {
			t.Fatal("ClaimTaskDelivery lost the claim")
		}
		err := mgr.DeliverOneShotTask(sess.FullID, tdTask, 3, 60*time.Second, 5*time.Second)
		if !errors.Is(err, ErrTaskUndelivered) {
			t.Fatalf("DeliverOneShotTask error = %v, want ErrTaskUndelivered", err)
		}
		if got := taskSends(fake, sess.TmuxSession, tdTask); got != 0 {
			t.Errorf("task sent %d times, want 0", got)
		}
		// Immediate fail: no wait, no backoff, no further attempts.
		if elapsed := c.elapsed(start); elapsed >= 5*time.Second {
			t.Errorf("consumed %v of virtual time, want under 5s for a pre-exited session", elapsed)
		}
		if !mgr.ClaimTaskDelivery(sess.FullID) {
			t.Error("claim was not released; a later ready cannot deliver the task")
		}
	})

	t.Run("tmux_session_gone", func(t *testing.T) {
		mgr, fake := newTestManagerWithFake(t)
		sess := tdrSaveWaiting(t, mgr, "tdrx2")
		start := time.Unix(0, 0).UTC()
		c := tdrInstallFakeClock(t, mgr, start)
		tdrSetPane(fake, sess.TmuxSession, tdDisclaimerPane)
		// The state is still waiting, but the tmux session itself is gone.
		if err := fake.KillSession(sess.TmuxSession); err != nil {
			t.Fatalf("KillSession: %v", err)
		}
		if !mgr.ClaimTaskDelivery(sess.FullID) {
			t.Fatal("ClaimTaskDelivery lost the claim")
		}
		err := mgr.DeliverOneShotTask(sess.FullID, tdTask, 3, 60*time.Second, 5*time.Second)
		if !errors.Is(err, ErrTaskUndelivered) {
			t.Fatalf("DeliverOneShotTask error = %v, want ErrTaskUndelivered", err)
		}
		if got := taskSends(fake, sess.TmuxSession, tdTask); got != 0 {
			t.Errorf("task sent %d times, want 0", got)
		}
		if elapsed := c.elapsed(start); elapsed >= 5*time.Second {
			t.Errorf("consumed %v of virtual time, want under 5s for a vanished tmux session", elapsed)
		}
		if !mgr.ClaimTaskDelivery(sess.FullID) {
			t.Error("claim was not released; a later ready cannot deliver the task")
		}
	})

	t.Run("killed_mid_wait", func(t *testing.T) {
		mgr, fake := newTestManagerWithFake(t)
		sess := tdrSaveWaiting(t, mgr, "tdrx3")
		start := time.Unix(0, 0).UTC()
		c := tdrInstallFakeClock(t, mgr, start)
		tdrSetPane(fake, sess.TmuxSession, tdDisclaimerPane)
		// The worker dies 10s into the wait and its tmux session goes with it.
		c.schedule(start.Add(10*time.Second), func() {
			if cur, ok := mgr.GetSession(sess.FullID); ok {
				cur.State = StateKilled
				_ = mgr.SaveSession(cur)
			}
			_ = fake.KillSession(sess.TmuxSession)
		})
		if !mgr.ClaimTaskDelivery(sess.FullID) {
			t.Fatal("ClaimTaskDelivery lost the claim")
		}
		err := mgr.DeliverOneShotTask(sess.FullID, tdTask, 3, 60*time.Second, 5*time.Second)
		if !errors.Is(err, ErrTaskUndelivered) {
			t.Fatalf("DeliverOneShotTask error = %v, want ErrTaskUndelivered", err)
		}
		if got := taskSends(fake, sess.TmuxSession, tdTask); got != 0 {
			t.Errorf("task sent %d times, want 0", got)
		}
		// Stopped at the 10s exit — reached the failure point but never burned
		// into a full second window or a backoff.
		elapsed := c.elapsed(start)
		if elapsed < 10*time.Second || elapsed >= 60*time.Second {
			t.Errorf("virtual time consumed = %v, want ~10s (early exit) and far under a full 60s window", elapsed)
		}
		if !mgr.ClaimTaskDelivery(sess.FullID) {
			t.Error("claim was not released; a later ready cannot deliver the task")
		}
	})
}
