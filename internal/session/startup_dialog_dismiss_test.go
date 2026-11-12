// One-shot startup-dialog dismissal via the screen-capture loop.
//
// One-shot claude-code sessions have no human at the terminal: when Claude
// starts with the workspace-trust dialog ("Quick safety check: ... Yes, I
// trust this folder / No, exit") or the development-channels consent dialog
// ("I am using this for local development"), the daemon must accept them
// automatically or the session sits blocked forever.
//
// Today the only daemon-side dismissal logic lives in processOutputLine,
// where it (a) only fires if the dialog text arrives intact on a single log
// pipe line, (b) shells out to a fresh `tmux send-keys` process (bypassing
// the m.tmux abstraction, so it is not observable through FakeTmux), and
// (c) latches startupDialogSent before the fire-and-forget send is known to
// have succeeded. StartScreenCapture — the path that is actually started
// unconditionally for one-shot sessions at creation time (manager.go,
// ~2209) — never inspects the pane for these dialogs at all.
//
// These tests pin the wanted behaviour through the capture loop the way
// self_update_dialog_test.go drives it:
//
//  1. trust dialog on the pane  → accept (Down, then Enter) via m.tmux
//  2. channels dialog on the pane → Enter via m.tmux
//  3. a failed accept send is retried on the next tick (then sticks)
//  4. no re-send once the dialog is gone; no double-send while it lingers
//  5. interactive (non-one-shot) sessions and other backends unchanged
//
// RED: against the current code these fail for the reason in the header —
// the capture loop does not invoke any startup-dialog handler. See
// docs/plans/2026-10-20-oneshot-startup-dialog-dismiss.md.

package session

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// dialogPanesSeparator is the marker line between the two pane captures in
// testdata/claude_trust_dialog.txt.
const dialogPanesSeparator = "; --- channels-consent pane ---"

// loadStartupDialogPanes reads the fixture and returns (trustPane,
// channelsPane).
func loadStartupDialogPanes(t *testing.T) (trustPane, channelsPane string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/claude_trust_dialog.txt")
	if err != nil {
		t.Fatalf("read fixture testdata/claude_trust_dialog.txt: %v", err)
	}
	parts := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n"+dialogPanesSeparator+"\n")
	if len(parts) != 2 {
		t.Fatalf("fixture must contain exactly two panes separated by the %q marker line, got %d blocks", dialogPanesSeparator, len(parts))
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func newStartupDialogSession(t *testing.T, mgr *Manager, backend string, oneShot bool) *Session {
	t.Helper()
	sess := &Session{
		ID: "d7a1", FullID: "testhost-d7a1", Hostname: "testhost",
		TmuxSession: "cs-d7a1", BackendFamily: backend,
		OneShot:   oneShot,
		State:     StateRunning,
		Task:      "one-shot task",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	return sess
}

// paneCaptures counts the capture-loop's visible-pane captures of the given
// tmux session. The loop captures once per tick before evaluating the pane,
// so an increase proves the loop has since scanned the pane's content.
func paneCaptures(f *FakeTmux, session string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.Calls {
		if c.Op == "capture-visible" && c.Session == session {
			n++
		}
	}
	return n
}

// scanTicks proves the capture loop scanned the current pane at least n more
// times: it polls until n+1 further captures happened (the +1 covers a
// capture already in flight when the pane changed), mutating the pane on each
// poll iteration so the loop's "content changed" gate (capture != lastCapture)
// passes on every tick.
func scanTicks(t *testing.T, sess *Session, fake *FakeTmux, n int) {
	t.Helper()
	base := paneCaptures(fake, sess.TmuxSession)
	pollUntil(t, 10*time.Second, func() bool {
		mutatePane(fake, sess.TmuxSession)
		return paneCaptures(fake, sess.TmuxSession) >= base+n+1
	}, "capture loop did not scan the pane the expected number of times")
}

// startCapture runs StartScreenCapture at a fast interval and returns a stop
// func that cancels the loop and waits for its goroutine to exit, so counts
// are asserted only once nothing can still send.
func startCapture(t *testing.T, mgr *Manager, sess *Session) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	mgr.StartScreenCapture(ctx, sess.FullID, 20)
	stopped := false
	stop = func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		waitCaptureLoopsExit(t, 10*time.Second)
	}
	t.Cleanup(stop)
	return stop
}

// driveCaptureTicks runs StartScreenCapture at a fast interval, scans the
// pane n times, then stops the loop and waits for it to exit.
func driveCaptureTicks(t *testing.T, mgr *Manager, sess *Session, fake *FakeTmux, n int) {
	t.Helper()
	stop := startCapture(t, mgr, sess)
	scanTicks(t, sess, fake, n)
	stop()
}

// trustSends counts trust-dialog accept sends through the m.tmux abstraction.
func trustSends(f *FakeTmux) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.Calls {
		if c.Op == "SendTrustPromptAccept" { // Down, then Enter
			n++
		}
	}
	return n
}

// channelSends counts channels-consent Enter sends through the m.tmux
// abstraction.
func channelSends(f *FakeTmux) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.Calls {
		if c.Op == "send-keys" && len(c.Args) > 0 && c.Args[0] == "Enter" {
			n++
		}
	}
	return n
}

// TestStartupDialogDismiss_OneShotTrustDialogAcceptedByCaptureLoop pins the
// core gap: with the workspace-trust dialog on the pane of a one-shot
// claude-code session, the capture loop must send the accept keystrokes
// (Down, then Enter) via the m.tmux abstraction. Fails against the current
// code: the capture loop never invokes the dialog handlers, so 0 sends.
func TestStartupDialogDismiss_OneShotTrustDialogAcceptedByCaptureLoop(t *testing.T) {
	trustPane, _ := loadStartupDialogPanes(t)
	mgr, fake := newTestManagerWithFake(t)
	sess := newStartupDialogSession(t, mgr, "claude-code", true)
	fake.Pane[sess.TmuxSession] = trustPane

	stop := startCapture(t, mgr, sess)
	pollUntil(t, 10*time.Second, func() bool { return trustSends(fake) >= 1 }, "trust accept never sent")
	scanTicks(t, sess, fake, 3) // a double-send would show up within these ticks
	stop()

	if got := trustSends(fake); got != 1 {
		t.Fatalf("trust dialog accept sent %d time(s) via m.tmux, want exactly 1; capture loop calls: %+v", got, fake.Calls)
	}
}

// TestStartupDialogDismiss_OneShotChannelsDialogAcceptedByCaptureLoop pins
// the same for the development-channels consent dialog, which has even less
// backup coverage than the trust dialog. Fails against the current code: 0
// sends.
func TestStartupDialogDismiss_OneShotChannelsDialogAcceptedByCaptureLoop(t *testing.T) {
	_, channelsPane := loadStartupDialogPanes(t)
	mgr, fake := newTestManagerWithFake(t)
	sess := newStartupDialogSession(t, mgr, "claude-code", true)
	fake.Pane[sess.TmuxSession] = channelsPane

	stop := startCapture(t, mgr, sess)
	pollUntil(t, 10*time.Second, func() bool { return channelSends(fake) >= 1 }, "channels accept never sent")
	scanTicks(t, sess, fake, 3)
	stop()

	if got := channelSends(fake); got != 1 {
		t.Fatalf("channels consent accept sent %d time(s) via m.tmux, want exactly 1; capture loop calls: %+v", got, fake.Calls)
	}
	if got := trustSends(fake); got != 0 {
		t.Fatalf("channels dialog must not trigger the trust accept (Down+Enter); got %d: %+v", got, fake.Calls)
	}
}

// TestStartupDialogDismiss_FailedAcceptIsRetriedOnNextTick: the latch is set
// before success in the current code, and the send errors are ignored, so a
// failed accept is never re-attempted. Wanted: first attempt fails (injected
// through FakeTmux.FailNextOp), the next tick sees the dialog still on the
// pane and retries, and the retry succeeds and sticks (no further attempts
// while the pane lingers). Fails against the current code: 0 attempts.
func TestStartupDialogDismiss_FailedAcceptIsRetriedOnNextTick(t *testing.T) {
	trustPane, _ := loadStartupDialogPanes(t)
	mgr, fake := newTestManagerWithFake(t)
	fake.FailNextOp = map[string]int{"SendTrustPromptAccept": 1} // first attempt fails
	sess := newStartupDialogSession(t, mgr, "claude-code", true)
	fake.Pane[sess.TmuxSession] = trustPane

	stop := startCapture(t, mgr, sess)
	pollUntil(t, 10*time.Second, func() bool {
		mutatePane(fake, sess.TmuxSession) // keep the content-change gate open for the retry tick
		return trustSends(fake) >= 2
	}, "failed accept was never retried")
	scanTicks(t, sess, fake, 3) // retry must stick: no third attempt while the pane lingers
	stop()

	got := trustSends(fake)
	if got != 2 {
		t.Fatalf("failed accept send was retried %d attempt(s) via m.tmux, want 2 (one failed first attempt + one successful retry, then latched); capture loop calls: %+v", got, fake.Calls)
	}
}

// TestStartupDialogDismiss_NotResentOnceDialogGone: after the accept has
// been sent and the dialog disappears from the pane, a later tick must not
// send again (the latch must be "dismissed confirmed", not "fired once" —
// the current processOutputLine latch fires the async send at most once but
// also blocks a legitimate retry of a failed send).
func TestStartupDialogDismiss_NotResentOnceDialogGone(t *testing.T) {
	trustPane, _ := loadStartupDialogPanes(t)
	mgr, fake := newTestManagerWithFake(t)
	sess := newStartupDialogSession(t, mgr, "claude-code", true)
	fake.Pane[sess.TmuxSession] = trustPane

	stop := startCapture(t, mgr, sess)
	// Dialog visible: accept sent.
	pollUntil(t, 10*time.Second, func() bool { return trustSends(fake) >= 1 }, "trust accept never sent")
	// Claude accepted — dialog gone, ordinary prompt on screen. Prove the
	// loop scanned the new pane several times before asserting no re-send.
	fake.mu.Lock()
	fake.Pane[sess.TmuxSession] = "▸ working on the task\n❯ "
	fake.mu.Unlock()
	scanTicks(t, sess, fake, 3)
	stop()

	got := trustSends(fake)
	if got != 1 {
		t.Fatalf("trust accept sent %d time(s), want exactly 1 (sent once, dialog then gone, no re-send); capture loop calls: %+v", got, fake.Calls)
	}
}

// drivePhase holds the pane at the given content until the capture loop has
// scanned it n times (see scanTicks), mutating the pane each poll so the
// content-change gate passes.
func drivePhase(t *testing.T, sess *Session, fake *FakeTmux, pane string, n int) {
	t.Helper()
	fake.mu.Lock()
	fake.Pane[sess.TmuxSession] = pane
	fake.mu.Unlock()
	scanTicks(t, sess, fake, n)
}

// holdPane keeps scanning the given pane until at least d has elapsed,
// proving via scanTicks that the loop kept seeing it throughout.
func holdPane(t *testing.T, sess *Session, fake *FakeTmux, pane string, d time.Duration) {
	t.Helper()
	t0 := time.Now()
	for time.Since(t0) < d {
		drivePhase(t, sess, fake, pane, 2)
	}
}

// TestStartupDialogDismiss_StuckDialogAutoRecovers pins the 2026-10-21
// live failure (one-shot trust dialog unhandled for ~6'41" until operator
// intervention): an accept send whose keystrokes did not take (the dialog
// never leaves the pane) must be re-sent automatically after the settle
// gap — no operator — and a "dismissed" latch that committed on a fleeting
// absence must re-arm the moment the dialog text is visible again.
//
// Phases: visible (send 1) → fleeting absence → visible again (blocked by
// the settle gap, then send 2 — auto-recovery) → sustained absence
// (dismiss confirmed) → visible again (re-arm, send 3) → sustained absence
// (dismiss confirmed again, no further sends).
func TestStartupDialogDismiss_StuckDialogAutoRecovers(t *testing.T) {
	trustPane, _ := loadStartupDialogPanes(t)
	mgr, fake := newTestManagerWithFake(t)
	sess := newStartupDialogSession(t, mgr, "claude-code", true)

	stop := startCapture(t, mgr, sess)

	prompt := "\u25b8 working on the task\n\u276f "

	// Phase 1: dialog visible → first accept send.
	fake.mu.Lock()
	fake.Pane[sess.TmuxSession] = trustPane
	fake.mu.Unlock()
	pollUntil(t, 10*time.Second, func() bool { return trustSends(fake) >= 1 }, "phase 1: trust accept never sent")
	if got := trustSends(fake); got != 1 {
		t.Fatalf("phase 1: trust accept sent %d time(s), want 1; calls: %+v", got, fake.Calls)
	}

	// Phase 2: a fleeting absence (one mid-render frame the dialog was
	// not in) — must NOT commit the dismissed latch on its own.
	drivePhase(t, sess, fake, prompt, 1)

	// Phase 3: dialog still visible. During the settle gap the loop must
	// hold; once the gap has elapsed it must re-send on its own (this is
	// the automatic recovery the live run of 2026-10-21 never achieved).
	fake.mu.Lock()
	fake.Pane[sess.TmuxSession] = trustPane
	fake.mu.Unlock()
	pollUntil(t, 10*time.Second, func() bool {
		mutatePane(fake, sess.TmuxSession)
		return trustSends(fake) >= 2
	}, "phase 3: stuck dialog not re-sent after the settle gap")
	if got := trustSends(fake); got != 2 {
		t.Fatalf("phase 3: stuck dialog not re-sent — accepts total %d, want 2 (auto-recovery after settle gap); calls: %+v", got, fake.Calls)
	}

	// Phase 4: the dialog is finally accepted — sustained absence commits
	// the "dismissed confirmed" latch.
	holdPane(t, sess, fake, prompt, 1500*time.Millisecond)
	if got := trustSends(fake); got != 2 {
		t.Fatalf("phase 4: no re-send expected after sustained absence, got %d accepts; calls: %+v", got, fake.Calls)
	}

	// Phase 5: the dialog text appears again after the confirmed latch
	// (a second trust prompt on the pane, or a stale latch that committed
	// on a mid-render frame) — the budget must re-arm and fire.
	fake.mu.Lock()
	fake.Pane[sess.TmuxSession] = trustPane
	fake.mu.Unlock()
	pollUntil(t, 10*time.Second, func() bool {
		mutatePane(fake, sess.TmuxSession)
		return trustSends(fake) >= 3
	}, "phase 5: re-appearance after dismissed latch not honoured")
	if got := trustSends(fake); got != 3 {
		t.Fatalf("phase 5: re-appearance after dismissed latch not honoured — accepts total %d, want 3; calls: %+v", got, fake.Calls)
	}

	// Phase 6: sustained absence commits the latch again and holds.
	holdPane(t, sess, fake, prompt, 1500*time.Millisecond)
	stop()
	if got := trustSends(fake); got != 3 {
		t.Fatalf("phase 6: re-send after re-dismiss — accepts total %d, want 3; calls: %+v", got, fake.Calls)
	}
}

// TestStartupDialogDismiss_NonOneShotAndOtherBackendsUnaffected pins the
// negative: interactive (non-one-shot) sessions — where an operator is at
// the terminal and the daemon's main-loop pollers already cover auto-accept
// — and non-claude-code backends must not receive capture-loop keystrokes.
// These must stay green when the capture loop grows the dismissal logic.
func TestStartupDialogDismiss_NonOneShotAndOtherBackendsUnaffected(t *testing.T) {
	trustPane, channelsPane := loadStartupDialogPanes(t)
	bothPanels := trustPane + "\n\n" + channelsPane

	cases := []struct {
		name    string
		backend string
		oneShot bool
	}{
		{name: "interactive claude-code", backend: "claude-code", oneShot: false},
		{name: "one-shot opencode", backend: "opencode", oneShot: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr, fake := newTestManagerWithFake(t)
			sess := newStartupDialogSession(t, mgr, tc.backend, tc.oneShot)
			fake.Pane[sess.TmuxSession] = bothPanels

			driveCaptureTicks(t, mgr, sess, fake, 4)

			if got := trustSends(fake); got != 0 {
				t.Fatalf("no trust accept expected, got %d: %+v", got, fake.Calls)
			}
			if got := channelSends(fake); got != 0 {
				t.Fatalf("no channels accept expected, got %d: %+v", got, fake.Calls)
			}
		})
	}
}
