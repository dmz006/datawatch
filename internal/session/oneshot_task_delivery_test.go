// One-shot claude-code task delivery on the fake tmux.
//
// Reproduces the "task delivery early exit" described in
// docs/plans/task-delivery-early-exit-root-cause.md: a PRD-owned one-shot
// session is created, the dev-channels disclaimer is auto-accepted, the
// channel bridge signals ready, the task is claimed and typed, and the
// typed task (which ends with a wrapped "DATAWATCH_COMPLETE: <one-line
// plain-text summary ...>" instruction) is echoed by the claude input box
// before the delivery bookkeeping (MarkTaskDelivered) runs. The screen
// watcher then reads the echo as a completion and ends the session without
// the model having done anything.
//
// The cmd/datawatch auto-accept goroutines and the server's
// handleChannelReady are not importable from this package, so
// taskDeliveryHarness replays the same manager calls in the same order
// (see cmd/datawatch/main.go auto-accept-disclaimer and
// internal/server/api.go handleChannelReady).
//
// TestOneShotTaskDelivery_TypedTaskEchoDoesNotComplete and
// TestOneShotTaskDelivery_OverlappingLaunchesEachDeliverOnce fail against the
// current code (defects 1 and 2 of the root-cause doc); the planned fix in
// that doc ("Planned fix" items 1 and 2: ClaimTaskDelivery arms the echo
// suppression, shared placeholder guard) makes them pass.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/session -run TestOneShotTaskDelivery

package session

import (
	"context"
	"sync"
	"testing"
	"time"
)

const (
	tdDisclaimerPane = "WARNING: Loading development channels outside local development is unsupported\n" +
		"and may leak prompts and tool output.\n\n" +
		"❯ 1. I am using this for local development\n  2. No, exit\n\nEnter to confirm · Esc to cancel"
	tdIdlePane = "▐▛███▜▌  Claude Code v2.0.14\n\n❯ Try \"how does this work?\"\n  ? for shortcuts"
	// tdTask mirrors a PRD worker spec: the harness-appended instruction uses
	// the "<one-line plain-text summary ...>" wording, which the existing
	// "<one-sentence summary" placeholder guard does not match.
	tdTask = "Implement the widget.\nFinish with a line: DATAWATCH_COMPLETE: <one-line plain-text summary of what was done>"
	// tdEchoed is how the claude input box shows tdTask while it is typed:
	// the long final line wraps and a wrapped line begins with the marker.
	tdEchoed = "▐▛███▜▌  Claude Code v2.0.14\n\n" +
		"> Implement the widget.\n" +
		"  Finish with a line:\n" +
		"DATAWATCH_COMPLETE: <one-line plain-text summary of what was done>\n"
)

// typingTmux wraps FakeTmux so that sending the task is not instantaneous:
// the echoed task appears on the pane first and the send call only returns
// after typeDelay, exactly the window in which the real claude box shows the
// task but delivery bookkeeping has not yet run.
type typingTmux struct {
	*FakeTmux
	task      string
	typeDelay time.Duration
}

func (t *typingTmux) typed(session, keys string) {
	if keys != t.task {
		return
	}
	t.mu.Lock()
	t.Pane[session] = tdEchoed
	t.mu.Unlock()
	time.Sleep(t.typeDelay)
}

func (t *typingTmux) SendKeys(session, keys string) error {
	t.typed(session, keys)
	return t.FakeTmux.SendKeys(session, keys)
}

func (t *typingTmux) SendKeysWithSettle(session, keys string, settle time.Duration) error {
	t.typed(session, keys)
	return t.FakeTmux.SendKeysWithSettle(session, keys, settle)
}

// taskSends counts sends of exactly the task text to one tmux session.
func taskSends(f *FakeTmux, tmuxSession, task string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.Calls {
		if c.Session == tmuxSession && (c.Op == "send-keys" || c.Op == "send-keys-settle") && len(c.Args) > 0 && c.Args[0] == task {
			n++
		}
	}
	return n
}

// disclaimerSends counts auto-accept-disclaimer keystrokes to one session.
func disclaimerSends(f *FakeTmux, tmuxSession string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.Calls {
		if c.Session == tmuxSession && (c.Op == "send-keys" || c.Op == "send-keys-settle") && len(c.Args) > 0 && (c.Args[0] == "1\n" || c.Args[0] == "\n") { // the startup classifier answers the development-channels dialog with Enter alone
			n++
		}
	}
	return n
}

type taskDeliveryHarness struct {
	t    *testing.T
	mgr  *Manager
	fake *FakeTmux
	wg   sync.WaitGroup
}

func newTaskDeliveryHarness(t *testing.T) *taskDeliveryHarness {
	t.Helper()
	mgr, fake := newTestManagerWithFake(t)
	mgr.tmux = &typingTmux{FakeTmux: fake, task: tdTask, typeDelay: 1500 * time.Millisecond}
	h := &taskDeliveryHarness{t: t, mgr: mgr, fake: fake}
	t.Cleanup(h.wg.Wait)
	return h
}

// launch starts a one-shot claude-code session whose pane shows the
// dev-channels disclaimer, through the real Manager.Start path (which starts
// the screen-capture watcher for one-shot sessions).
func (h *taskDeliveryHarness) launch() *Session {
	h.t.Helper()
	sess, err := h.mgr.Start(context.Background(), tdTask, "", h.t.TempDir(), &StartOptions{
		Backend: "claude-code",
		OneShot: true,
		PRDID:   "prd-task-delivery",
		LaunchFn: func(_ context.Context, _, tmuxSession, _, _ string) error {
			_ = h.fake.NewSessionWithSize(tmuxSession, 80, 24)
			_ = h.fake.PipeOutput(tmuxSession, "/dev/null")
			h.fake.mu.Lock()
			h.fake.Pane[tmuxSession] = tdDisclaimerPane
			h.fake.mu.Unlock()
			return nil
		},
	})
	if err != nil {
		h.t.Fatalf("Start: %v", err)
	}
	return sess
}

// waitCaptureStarted blocks until the manager's own watcher has scanned the
// session's pane at least once.
func (h *taskDeliveryHarness) waitCaptureStarted(sess *Session) {
	h.t.Helper()
	pollUntil(h.t, 15*time.Second, func() bool { return paneCaptures(h.fake, sess.TmuxSession) >= 1 },
		"manager screen-capture watcher never scanned the pane")
}

// acceptDisclaimer replays the auto-accept-disclaimer goroutine: when the
// manager reports a startup prompt, send its response once, then the pane
// shows the idle prompt and the session is waiting for input.
func (h *taskDeliveryHarness) acceptDisclaimer(sess *Session) {
	h.t.Helper()
	var resp string
	pollUntil(h.t, 10*time.Second, func() bool {
		resp = h.mgr.GetActiveStartupPromptInput(sess.FullID)
		return resp != ""
	}, "startup disclaimer never recognised on the pane")
	if err := h.mgr.SendInput(sess.FullID, resp, "auto-accept-disclaimer"); err != nil {
		h.t.Fatalf("auto-accept SendInput: %v", err)
	}
	h.fake.mu.Lock()
	h.fake.Pane[sess.TmuxSession] = tdIdlePane
	h.fake.mu.Unlock()
	if cur, ok := h.mgr.GetSession(sess.FullID); ok && cur.State != StateComplete && cur.State != StateKilled && cur.State != StateFailed {
		cur.State = StateWaitingInput
		_ = h.mgr.SaveSession(cur)
	}
}

// signalReady replays handleChannelReady: record ChannelReady, win the claim
// and start the poll goroutine that types the task once the prompt is idle.
// It returns whether this caller won the claim.
func (h *taskDeliveryHarness) signalReady(sess *Session) bool {
	h.t.Helper()
	if cur, ok := h.mgr.GetSession(sess.FullID); ok {
		cur.ChannelReady = true
		_ = h.mgr.SaveSession(cur)
	}
	if !h.mgr.ClaimTaskDelivery(sess.FullID) {
		return false
	}
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		for i := 0; i < 20; i++ {
			time.Sleep(200 * time.Millisecond)
			cur, ok := h.mgr.GetSession(sess.FullID)
			if !ok || cur.State == StateComplete || cur.State == StateKilled || cur.State == StateFailed {
				h.mgr.ReleaseTaskDelivery(sess.FullID)
				return
			}
			if h.mgr.GetActiveStartupPromptInput(sess.FullID) == "" {
				if err := h.mgr.SendInput(sess.FullID, tdTask, "channel-task"); err != nil {
					h.mgr.ReleaseTaskDelivery(sess.FullID)
					return
				}
				h.mgr.MarkTaskDelivered(sess.FullID)
				return
			}
		}
		h.mgr.ReleaseTaskDelivery(sess.FullID)
	}()
	return true
}

// settle lets the watcher run for a few more ticks after the delivery so a
// false completion has time to land.
func (h *taskDeliveryHarness) settle(sess *Session, ticks int) {
	h.t.Helper()
	base := paneCaptures(h.fake, sess.TmuxSession)
	pollUntil(h.t, 15*time.Second, func() bool {
		if cur, ok := h.mgr.GetSession(sess.FullID); !ok || cur.State == StateComplete || cur.State == StateKilled || cur.State == StateFailed {
			return true // ended; the caller's assertions report it
		}
		return paneCaptures(h.fake, sess.TmuxSession) >= base+ticks
	}, "watcher stopped scanning")
}

// sentOrEnded polls until the task was typed into sess or the session ended.
func (h *taskDeliveryHarness) sentOrEnded(sess *Session) {
	h.t.Helper()
	pollUntil(h.t, 20*time.Second, func() bool {
		if taskSends(h.fake, sess.TmuxSession, tdTask) >= 1 {
			return true
		}
		cur, ok := h.mgr.GetSession(sess.FullID)
		return !ok || cur.State == StateComplete || cur.State == StateKilled || cur.State == StateFailed
	}, "task never sent to "+sess.FullID)
	// A send that is still being typed records its call only when it returns;
	// give an in-flight one time to land so the send counts are final.
	deadline := time.Now().Add(3 * time.Second)
	for taskSends(h.fake, sess.TmuxSession, tdTask) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
}

func (h *taskDeliveryHarness) assertAlive(sess *Session, when string) {
	h.t.Helper()
	cur, ok := h.mgr.GetSession(sess.FullID)
	if !ok {
		h.t.Fatalf("%s: session %s vanished", when, sess.FullID)
	}
	switch cur.State {
	case StateComplete, StateKilled, StateFailed:
		h.t.Errorf("%s: session %s ended with state %q before the model did any work (typed-task echo read as completion)", when, sess.FullID, cur.State)
	}
}

// (1) Full happy path: disclaimer auto-accepted, channel ready, the task is
// typed exactly once and the echo of the typed task is not a completion.
func TestOneShotTaskDelivery_DisclaimerThenReadySendsTaskOnce(t *testing.T) {
	h := newTaskDeliveryHarness(t)
	sess := h.launch()
	h.waitCaptureStarted(sess)

	h.acceptDisclaimer(sess)
	if !h.signalReady(sess) {
		t.Fatal("first ready lost the task claim")
	}
	// A repeated ready heartbeat must not re-send.
	if h.signalReady(sess) {
		t.Error("second ready also won the claim; task would be typed twice")
	}

	h.sentOrEnded(sess)
	h.settle(sess, 4)

	if got := taskSends(h.fake, sess.TmuxSession, tdTask); got != 1 {
		t.Errorf("task sent %d times, want exactly 1; calls: %+v", got, h.fake.Calls)
	}
	if got := disclaimerSends(h.fake, sess.TmuxSession); got != 1 {
		t.Errorf("disclaimer accepted %d times, want exactly 1", got)
	}
}

// The typed task ends with a wrapped DATAWATCH_COMPLETE instruction line. Until
// the model replies, that echo must never end the session. RED against the
// current code: ClaimTaskDelivery does not arm the task-echo suppression and
// the placeholder guard misses the "<one-line plain-text summary" wording, so
// the watcher completes the session while the task is still being typed.
func TestOneShotTaskDelivery_TypedTaskEchoDoesNotComplete(t *testing.T) {
	h := newTaskDeliveryHarness(t)
	sess := h.launch()
	h.waitCaptureStarted(sess)

	h.acceptDisclaimer(sess)
	h.signalReady(sess)
	h.sentOrEnded(sess)
	h.settle(sess, 6)

	h.assertAlive(sess, "after task delivery")
	if got := taskSends(h.fake, sess.TmuxSession, tdTask); got != 1 {
		t.Errorf("task sent %d times, want exactly 1", got)
	}
}

// (2) Two launches of the same task a few seconds apart (retry overlapping a
// not-yet-reaped first attempt). Each surviving session must receive the task
// exactly once, and the first launch must not die after only the disclaimer.
// RED against the current code for the same reason as the echo test.
func TestOneShotTaskDelivery_OverlappingLaunchesEachDeliverOnce(t *testing.T) {
	h := newTaskDeliveryHarness(t)
	first := h.launch()
	h.waitCaptureStarted(first)
	h.acceptDisclaimer(first)

	time.Sleep(3 * time.Second)
	second := h.launch()
	h.waitCaptureStarted(second)

	// The first launch got only the disclaimer so far; it must still be alive.
	h.assertAlive(first, "first launch after disclaimer only")

	h.acceptDisclaimer(second)
	h.signalReady(first)
	h.signalReady(second)

	for _, s := range []*Session{first, second} {
		h.sentOrEnded(s)
	}
	h.settle(second, 6)

	for name, s := range map[string]*Session{"first": first, "second": second} {
		if got := taskSends(h.fake, s.TmuxSession, tdTask); got != 1 {
			t.Errorf("%s launch: task sent %d times, want exactly 1", name, got)
		}
		h.assertAlive(s, name+" launch after delivery")
	}
}

// (3) The session exits after the disclaimer but before the task is typed.
// Nothing may be typed into the dead session, and the claim must be released
// so TaskDelivered does not report a delivery that never happened (the
// executor treats "terminal and not delivered" as a retryable stall).
func TestOneShotTaskDelivery_ExitAfterDisclaimerBeforeDelivery(t *testing.T) {
	h := newTaskDeliveryHarness(t)
	sess := h.launch()
	h.waitCaptureStarted(sess)

	h.acceptDisclaimer(sess)
	if !h.signalReady(sess) {
		t.Fatal("ready lost the task claim")
	}
	// The process dies right after the claim, before the poll types the task.
	if err := h.mgr.Kill(sess.FullID); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	pollUntil(t, 10*time.Second, func() bool { return !h.mgr.TaskDelivered(sess.FullID) },
		"claim was not released after the session died before delivery")
	h.wg.Wait()

	if got := taskSends(h.fake, sess.TmuxSession, tdTask); got != 0 {
		t.Errorf("task typed %d times into a dead session, want 0", got)
	}
	if got := disclaimerSends(h.fake, sess.TmuxSession); got != 1 {
		t.Errorf("disclaimer accepted %d times, want exactly 1", got)
	}
	cur, _ := h.mgr.GetSession(sess.FullID)
	if cur == nil || cur.State != StateKilled {
		st := StateKilled
		if cur != nil {
			st = cur.State
		}
		t.Errorf("session state = %q, want %q", st, StateKilled)
	}
	if err := h.mgr.SendInput(sess.FullID, tdTask, "channel-task"); err == nil {
		t.Error("SendInput to an ended session succeeded; it must be refused")
	}
}

func TestIsCompletionPlaceholder(t *testing.T) {
	for l, want := range map[string]bool{
		"DATAWATCH_COMPLETE: <one-line plain-text summary of what was done>":   true,
		"DATAWATCH_COMPLETE: <one-sentence summary of what was done, used as>": true,
		"DATAWATCH_COMPLETE: added the widget":                                 false,
	} {
		if got := isCompletionPlaceholder(l); got != want {
			t.Errorf("isCompletionPlaceholder(%q) = %v, want %v", l, got, want)
		}
	}
}

func TestClaimTaskDeliveryArmsEchoGuardAndReleaseClears(t *testing.T) {
	h := newTaskDeliveryHarness(t)
	sess := h.launch()
	if !h.mgr.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("claim lost")
	}
	h.mgr.mu.Lock()
	_, armed := h.mgr.taskDeliveredAt[sess.FullID]
	h.mgr.mu.Unlock()
	if !armed {
		t.Error("claim did not arm the task-echo suppression")
	}
	h.mgr.ReleaseTaskDelivery(sess.FullID)
	h.mgr.mu.Lock()
	_, armed = h.mgr.taskDeliveredAt[sess.FullID]
	h.mgr.mu.Unlock()
	if armed || h.mgr.TaskDelivered(sess.FullID) {
		t.Error("release did not clear the guard")
	}
}
