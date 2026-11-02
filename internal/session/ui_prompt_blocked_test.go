// TestUIPromptBlocked_* — interstitial-prompt blocking of claude-code
// sessions through the screen-capture loop.
//
// When a recognised interstitial UI prompt (the auto-compact nudge family)
// is on the pane, the session must be detected and blocked, none of the
// waiting-input consumer side effects may fire (no needs-input callback /
// alert, no response capture or summarisation), no key may be sent (the
// safe dismiss is a later phase), and the prior state must be restored when
// the prompt clears — except when the session reached a terminal state in
// the meantime. These pin that end-to-end through StartScreenCapture with
// FakeTmux panes and the real session store.
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- pane + callback wiring helpers ------------------------------------

// blockingFixture is a real captured pane classification pins to the
// auto-compact threshold prompt with the cursor on the setting-changing row.
const blockingFixture = "autocompact-enter-would-pick.txt"

// clearFixture is a negative fixture: an ordinary idle pane that classifies
// to none and does not trip the needs-input detector.
const clearFixture = "idle-prompt.txt"

// uiPromptCallbacks captures the loop's consumer callbacks so a test can
// assert on what fired while the prompt was on screen.
type uiPromptCallbacks struct {
	mu        sync.Mutex
	needs     int
	oldStates []State
}

func (c *uiPromptCallbacks) wire(m *Manager) {
	m.SetNeedsInputHandler(func(*Session, string) {
		c.mu.Lock()
		c.needs++
		c.mu.Unlock()
	})
	m.SetStateChangeHandler(func(s *Session, old State) {
		c.mu.Lock()
		c.oldStates = append(c.oldStates, old)
		c.mu.Unlock()
	})
}

func (c *uiPromptCallbacks) snapshot() (int, []State) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.needs, append([]State(nil), c.oldStates...)
}

func newUIPromptSession(t *testing.T, mgr *Manager, backend string, state State) *Session {
	t.Helper()
	sess := &Session{
		ID: "aa01", FullID: "testhost-aa01", Hostname: "testhost",
		TmuxSession: "cs-aa01", BackendFamily: backend,
		State:     state,
		Task:      "interactive task",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	return sess
}

// setPaneLocked replaces a fake pane's content under the FakeTmux lock; the
// capture loop may be reading it concurrently.
func setPaneLocked(t *testing.T, fake *FakeTmux, session, content string) {
	t.Helper()
	fake.mu.Lock()
	fake.Pane[session] = content
	fake.mu.Unlock()
}

// liveSnap is a point-in-time copy of the session record, safe to read
// while the capture loop is mutating the shared session object (its writes
// go through store.Mutate, i.e. the store write lock).
func liveSnap(t *testing.T, mgr *Manager, sess *Session) (Session, bool) {
	t.Helper()
	got, ok := mgr.GetSession(sess.FullID)
	if !ok {
		return Session{}, false
	}
	return mgr.store.Snapshot(got), true
}

func keySends(f *FakeTmux, session string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.Calls {
		if strings.Contains(c.Op, "send") || strings.Contains(c.Op, "Send") {
			n++
		}
	}
	return n
}

func loadBlockingPane(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "interstitial", blockingFixture))
	if err != nil {
		t.Fatalf("read fixture %s: %v", blockingFixture, err)
	}
	return string(data)
}

func loadClearPane(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "interstitial", clearFixture))
	if err != nil {
		t.Fatalf("read fixture %s: %v", clearFixture, err)
	}
	return string(data)
}

// parkSession drives capture ticks until the session is parked on the
// blocking prompt (waiting_input), then asserts no needs-input callback
// fired.
func parkSession(t *testing.T, mgr *Manager, sess *Session, cb *uiPromptCallbacks) {
	t.Helper()
	pollUntil(t, 15*time.Second, func() bool {
		snap, ok := liveSnap(t, mgr, sess)
		return ok && snap.State == StateWaitingInput
	}, "running session never parked on the recognised prompt")
	if cb == nil {
		return
	}
	if needs, old := cb.snapshot(); needs != 0 {
		t.Fatalf("onNeedsInput fired %d time(s) while the session was parked on a recognised prompt; want 0 (state transition(s) recorded: %v)", needs, old)
	}
}

// waitParked polls, with a generous bound, until the whole parked condition
// is visible in every consumer view at once: the state-change callback has
// recorded exactly one transition (the old state [running]), the session
// record carries the prompt (snap.UIPrompt != nil) and it stays on
// waiting_input, and UIPromptFor exposes the snapshot. The capture goroutine
// emits these in order — save, then the onStateChange callback, then the
// timeline line — after making the state change itself visible, so a
// fixed-order read right after parkSession is a race: the state can be
// waiting_input while the callback and (on an in-flight tick) the prompt
// exposure have not yet landed. All zero assertions (needs-input count, key
// sends) stay OUTSIDE the wait and are asserted after the positive
// condition is reached.
func waitParked(t *testing.T, mgr *Manager, sess *Session, cb *uiPromptCallbacks, fake *FakeTmux) {
	t.Helper()
	const timeout = 20 * time.Second
	deadline := time.Now().Add(timeout)
	last := "(not read yet)"
	for {
		snap, ok := liveSnap(t, mgr, sess)
		needs, old := cb.snapshot()
		keys := keySends(fake, sess.TmuxSession)
		last = fmt.Sprintf("state=%q UIPrompt set=%v UIPromptFor set=%v needsInput=%d oldStates=%v keySends=%d",
			snap.State, snap.UIPrompt != nil, mgr.UIPromptFor(sess.FullID) != nil, needs, old, keys)
		if ok && snap.State == StateWaitingInput && snap.UIPrompt != nil &&
			mgr.UIPromptFor(sess.FullID) != nil &&
			len(old) == 1 && old[0] == StateRunning {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting %s for the parked condition (exactly one state-change transition recorded from [running], state still waiting_input, snapshot UIPrompt set, UIPromptFor non-nil); last seen: %s",
				timeout, last)
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestUIPromptBlocked_RunningParksSuppressesAndExposes: the core behaviour —
// a recognised interstitial prompt on the pane of a running claude-code
// session parks it on waiting_input, exposes the prompt through both the
// session record and UIPromptFor, fires the state-change transition exactly
// once, and none of the waiting-input consumer side effects fire (no
// needs-input callback, no key sent).
func TestUIPromptBlocked_RunningParksSuppressesAndExposes(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	cb := &uiPromptCallbacks{}
	cb.wire(mgr)
	sess := newUIPromptSession(t, mgr, "claude-code", StateRunning)
	setPaneLocked(t, fake, sess.TmuxSession, loadBlockingPane(t))

	stop := startCapture(t, mgr, sess)
	parkSession(t, mgr, sess, cb)
	waitParked(t, mgr, sess, cb, fake)

	cur, ok := liveSnap(t, mgr, sess)
	if !ok {
		t.Fatalf("session %s missing from the store after parking", sess.FullID)
	}
	if cur.State != StateWaitingInput {
		t.Fatalf("state = %q, want waiting_input (UIPrompt set=%v)", cur.State, cur.UIPrompt != nil)
	}
	if cur.UIPrompt == nil {
		t.Fatalf("session.UIPrompt = nil, want the recognised prompt exposed (state=%q)", cur.State)
	}
	if cur.UIPrompt.Kind != InterstitialAutocompactThreshold {
		t.Errorf("UIPrompt.Kind = %q, want %q", cur.UIPrompt.Kind, InterstitialAutocompactThreshold)
	}
	if !strings.Contains(cur.UIPrompt.SelectedOption, "Autocompact at 400k") {
		t.Errorf("UIPrompt.SelectedOption = %q, want it to contain %q (Enter would pick it)", cur.UIPrompt.SelectedOption, "Autocompact at 400k")
	}
	if cur.UIPrompt.Since.IsZero() {
		t.Errorf("UIPrompt.Since is zero; want the first recognition timestamp")
	}

	// UIPromptFor exposes the snapshot for REST / MCP / server consumers.
	snapInfo := mgr.UIPromptFor(sess.FullID)
	if snapInfo == nil {
		t.Fatalf("UIPromptFor(%s) = nil while blocked; want the prompt snapshot (state=%q, record UIPrompt set=%v)",
			sess.FullID, cur.State, cur.UIPrompt != nil)
	}
	if snapInfo.Kind != InterstitialAutocompactThreshold || !strings.Contains(snapInfo.SelectedOption, "Autocompact at 400k") {
		t.Errorf("UIPromptFor() = %+v, want kind autocompact-threshold with the 400k option", snapInfo)
	}
	if got := mgr.UIPromptFor("testhost-unknown"); got != nil {
		t.Errorf("UIPromptFor(unknown) = %+v, want nil for a session with no tracked prompt", got)
	}

	// Zero / exact-once checks, asserted now that the parked condition is
	// fully visible (waitParked returned), so they can not race the
	// callbacks behind the fixed-order read.
	if needs, old := cb.snapshot(); needs != 0 || len(old) != 1 || old[0] != StateRunning {
		t.Fatalf("side effects while blocked: needsInput=%d oldStates=%v; want 0 and exactly [running]", needs, old)
	}
	if got := keySends(fake, sess.TmuxSession); got != 0 {
		t.Fatalf("loop sent %d key(s) while blocked; detection only, want 0: %+v", got, fake.Calls)
	}
	stop()
}

// TestUIPromptBlocked_StaticPaneTicksKeepParked: a static pane that still
// carries the prompt (no capture change across ticks) must keep the session
// parked, without re-firing the transition or any consumer callback.
func TestUIPromptBlocked_StaticPaneTicksKeepParked(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	cb := &uiPromptCallbacks{}
	cb.wire(mgr)
	sess := newUIPromptSession(t, mgr, "claude-code", StateRunning)
	setPaneLocked(t, fake, sess.TmuxSession, loadBlockingPane(t)) // static: never mutated

	stop := startCapture(t, mgr, sess)
	parkSession(t, mgr, sess, cb)
	waitParked(t, mgr, sess, cb, fake)

	base := paneCaptures(fake, sess.TmuxSession)
	pollUntil(t, 10*time.Second, func() bool { return paneCaptures(fake, sess.TmuxSession) >= base+4 },
		"capture loop stopped ticking on the static pane")
	if got := mgr.UIPromptFor(sess.FullID); got == nil {
		t.Fatalf("UIPromptFor(%s) = nil after static ticks; the park must persist while the prompt is on the pane",
			sess.FullID)
	}
	if needs, old := cb.snapshot(); needs != 0 || len(old) != 1 || old[0] != StateRunning {
		t.Fatalf("static ticks re-fired side effects: needsInput=%d oldStates=%v; want 0 and exactly 1 transition from [running]", needs, old)
	}
	if got := keySends(fake, sess.TmuxSession); got != 0 {
		t.Fatalf("loop sent %d key(s) while the static pane stayed parked; detection only, want 0: %+v", got, fake.Calls)
	}
	stop()
	snap, ok := liveSnap(t, mgr, sess)
	if !ok || snap.State != StateWaitingInput {
		t.Fatalf("state after static ticks = %q, want parked (waiting_input)", snap.State)
	}
}

// TestUIPromptBlocked_PaneClearsRestoresOriginalState: once the prompt
// leaves the pane, the parked session returns to the state it was in before
// the prompt, the exposed UIPrompt clears, and the restore transition fires
// exactly once (no re-firing on later cleared ticks).
func TestUIPromptBlocked_PaneClearsRestoresOriginalState(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	cb := &uiPromptCallbacks{}
	cb.wire(mgr)
	sess := newUIPromptSession(t, mgr, "claude-code", StateRunning)
	setPaneLocked(t, fake, sess.TmuxSession, loadBlockingPane(t))

	stop := startCapture(t, mgr, sess)
	parkSession(t, mgr, sess, cb)
	waitParked(t, mgr, sess, cb, fake)
	if needs, old := cb.snapshot(); needs != 0 {
		t.Fatalf("onNeedsInput fired %d time(s) while parked before the clear; want 0 (state transition(s): %v)", needs, old)
	}

	// The prompt is dismissed (by whatever agent the later phase is) and
	// the pane returns to an ordinary idle view.
	setPaneLocked(t, fake, sess.TmuxSession, loadClearPane(t))
	pollUntil(t, 15*time.Second, func() bool {
		snap, ok := liveSnap(t, mgr, sess)
		return ok && snap.State == StateRunning && snap.UIPrompt == nil
	}, "pane cleared but session never restored to running")

	// Hold the cleared pane for several more ticks: no re-fire.
	base := paneCaptures(fake, sess.TmuxSession)
	pollUntil(t, 10*time.Second, func() bool { return paneCaptures(fake, sess.TmuxSession) >= base+4 },
		"capture loop stopped ticking after restore")
	if mgr.UIPromptFor(sess.FullID) != nil {
		t.Fatal("UIPromptFor non-nil after the prompt cleared; want nil")
	}
	if needs, old := cb.snapshot(); needs != 0 || len(old) != 2 || old[0] != StateRunning || old[1] != StateWaitingInput {
		t.Fatalf("side effects over park+clear: needsInput=%d oldStates=%v; want 0 and [running waiting_input]", needs, old)
	}
	stop()
}

// TestUIPromptBlocked_AdoptedWaitingInputExposesWithoutTransition: a session
// that was already waiting_input (e.g. the daemon restarted over a blocked
// pane, or another signal parked it first) is adopted on that state with
// the recognised prompt exposed — no second state transition fires.
func TestUIPromptBlocked_AdoptedWaitingInputExposesWithoutTransition(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	cb := &uiPromptCallbacks{}
	cb.wire(mgr)
	sess := newUIPromptSession(t, mgr, "claude-code", StateWaitingInput)
	setPaneLocked(t, fake, sess.TmuxSession, loadBlockingPane(t))

	stop := startCapture(t, mgr, sess)
	pollUntil(t, 15*time.Second, func() bool {
		snap, ok := liveSnap(t, mgr, sess)
		return ok && snap.UIPrompt != nil
	}, "adopted waiting_input session never exposed the recognised prompt")

	snap, ok := liveSnap(t, mgr, sess)
	if !ok {
		t.Fatal("session missing")
	}
	if snap.State != StateWaitingInput {
		t.Fatalf("state = %q, want it to stay waiting_input (no second transition)", snap.State)
	}
	if needs, old := cb.snapshot(); needs != 0 || len(old) != 0 {
		t.Fatalf("adoption fired side effects: needsInput=%d oldStates=%v; want 0 and none", needs, old)
	}
	if got := keySends(fake, sess.TmuxSession); got != 0 {
		t.Fatalf("loop sent %d key(s) on an adopted session; detection only, want 0: %+v", got, fake.Calls)
	}
	stop()
}

// TestUIPromptBlocked_NonClaudeCodeIgnored: the same recognised prompt on
// the pane of a non-claude-code session parks nothing and exposes nothing.
// The gate is a backend gate (claude-code), not an "any session" gate.
func TestUIPromptBlocked_NonClaudeCodeIgnored(t *testing.T) {
	for _, backend := range []string{"opencode", "aider"} {
		t.Run(backend, func(t *testing.T) {
			mgr, fake := newTestManagerWithFake(t)
			cb := &uiPromptCallbacks{}
			cb.wire(mgr)
			sess := newUIPromptSession(t, mgr, backend, StateRunning)
			setPaneLocked(t, fake, sess.TmuxSession, loadBlockingPane(t))

			stop := startCapture(t, mgr, sess)
			base := paneCaptures(fake, sess.TmuxSession)
			pollUntil(t, 10*time.Second, func() bool { return paneCaptures(fake, sess.TmuxSession) >= base+4 },
				"capture loop did not scan a non-claude-code pane")
			stop()

			cur, ok := liveSnap(t, mgr, sess)
			if !ok {
				t.Fatal("session missing")
			}
			if cur.State != StateRunning {
				t.Fatalf("%s: state = %q, want running (prompt must not park non-claude-code)", backend, cur.State)
			}
			if cur.UIPrompt != nil {
				t.Fatalf("%s: UIPrompt = %+v, want nil", backend, cur.UIPrompt)
			}
			if info := mgr.UIPromptFor(sess.FullID); info != nil {
				t.Fatalf("%s: UIPromptFor = %+v, want nil", backend, info)
			}
			if needs, old := cb.snapshot(); needs != 0 || len(old) != 0 {
				t.Fatalf("%s: side effects fired: needsInput=%d oldStates=%v; want none", backend, needs, old)
			}
		})
	}
}

// TestUIPromptBlocked_TerminalStateNeverRestored: while the prompt is on
// screen the session is parked; if it then reaches a terminal state
// (complete), the loop must stop tracking it — the parked state is never
// restored and the prompt is never exposed on a terminated session.
func TestUIPromptBlocked_TerminalStateNeverRestored(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	cb := &uiPromptCallbacks{}
	cb.wire(mgr)
	sess := newUIPromptSession(t, mgr, "claude-code", StateRunning)
	setPaneLocked(t, fake, sess.TmuxSession, loadBlockingPane(t))

	stop := startCapture(t, mgr, sess)
	parkSession(t, mgr, sess, cb)
	stop()

	// While still parked the session completes (the daemon's completion
	// path landed first; the prompt is irrelevant now). The save must stop
	// the tracking and drop the exposed prompt.
	got, ok := mgr.GetSession(sess.FullID)
	if !ok {
		t.Fatal("session missing")
	}
	got.State = StateComplete
	if err := mgr.SaveSession(got); err != nil {
		t.Fatalf("SaveSession(complete): %v", err)
	}

	cur, ok := liveSnap(t, mgr, sess)
	if !ok {
		t.Fatal("session missing after completion")
	}
	if cur.State != StateComplete {
		t.Fatalf("state = %q, want complete (restoration must not resurrect a terminal session)", cur.State)
	}
	if cur.UIPrompt != nil {
		t.Fatalf("UIPrompt = %+v on a terminated session, want nil", cur.UIPrompt)
	}
	if info := mgr.UIPromptFor(sess.FullID); info != nil {
		t.Fatalf("UIPromptFor = %+v on a terminated session, want nil", info)
	}
	if _, old := cb.snapshot(); len(old) != 1 || old[0] != StateRunning {
		t.Fatalf("transitions after completion = %v; want exactly the parking transition [running]", old)
	}
}

// TestUIPromptBlocked_SessionJSONByteIdentity: a session without UIPrompt
// must marshal byte-identical to the pre-feature shape (the field is
// omitempty; no key, no empty object), and a blocked one must expose the
// exact snapshot shape — including dismiss_attempts at 0, since this phase
// never sends a key.
func TestUIPromptBlocked_SessionJSONByteIdentity(t *testing.T) {
	// A zero session: identical bytes to any pre-feature Session, i.e. no
	// ui_prompt key at all.
	zero := sessionJSON(t, &Session{})
	const wantZero = `{"id":"","full_id":"","task":"","project_dir":"","tmux_session":"","log_file":"","state":"","created_at":"0001-01-01T00:00:00Z","updated_at":"0001-01-01T00:00:00Z","hostname":"","group_id":"","tracking_dir":"","summary_generated_at":"0001-01-01T00:00:00Z","last_channel_event_at":"0001-01-01T00:00:00Z"}`
	if zero != wantZero {
		t.Fatalf("zero-session JSON changed shape with the new field:\n got:  %s\n want: %s", zero, wantZero)
	}

	// A blocked session: the snapshot is exposed with this phase's contract
	// (dismiss_attempts 0, no last_key_at — only the dismissal phase sets
	// those).
	tt := time.Date(2026, 10, 15, 1, 25, 0, 0, time.UTC)
	s := &Session{ID: "aa01", FullID: "h-aa01"}
	s.UIPrompt = &UIPromptInfo{Kind: InterstitialAutocompactThreshold, Since: tt, SelectedOption: "Autocompact at 400k"}
	got := sessionJSON(t, s)
	const want = `{"id":"aa01","full_id":"h-aa01","task":"","project_dir":"","tmux_session":"","log_file":"","state":"","created_at":"0001-01-01T00:00:00Z","updated_at":"0001-01-01T00:00:00Z","hostname":"","group_id":"","ui_prompt":{"kind":"autocompact-threshold","since":"2026-10-15T01:25:00Z","selected_option":"Autocompact at 400k","dismiss_attempts":0},"tracking_dir":"","summary_generated_at":"0001-01-01T00:00:00Z","last_channel_event_at":"0001-01-01T00:00:00Z"}`
	if got != want {
		t.Fatalf("blocked-session JSON != contract:\n got:  %s\n want: %s", got, want)
	}
}

func sessionJSON(t *testing.T, s *Session) string {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}
	return string(data)
}

// TestUIPromptBlocked_TimelineOneLinePerChange: the tracker's timeline gets
// exactly one entry when the prompt is recognised (the session parks) and
// exactly one when it clears — never one per tick, even across a parked
// interval with repeated capture ticks.
func TestUIPromptBlocked_TimelineOneLinePerChange(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := newUIPromptSession(t, mgr, "claude-code", StateRunning)

	// Give the manager an explicit tracker for this session (normally
	// created by NewSession at creation time; this fixture starts parked).
	trk, err := NewTracker(mgr.DataDir(), sess)
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	mgr.mu.Lock()
	mgr.trackers[sess.FullID] = trk
	mgr.mu.Unlock()

	setPaneLocked(t, fake, sess.TmuxSession, loadBlockingPane(t))
	cb := &uiPromptCallbacks{}
	cb.wire(mgr)
	stop := startCapture(t, mgr, sess)
	parkSession(t, mgr, sess, cb)
	base := paneCaptures(fake, sess.TmuxSession)
	pollUntil(t, 10*time.Second, func() bool { return paneCaptures(fake, sess.TmuxSession) >= base+4 },
		"parked session stopped ticking")

	// Prompt clears: the restore transition — exactly one more timeline line.
	setPaneLocked(t, fake, sess.TmuxSession, loadClearPane(t))
	pollUntil(t, 15*time.Second, func() bool {
		snap, ok := liveSnap(t, mgr, sess)
		return ok && snap.State == StateRunning
	}, "pane cleared but session never restored")
	base = paneCaptures(fake, sess.TmuxSession)
	pollUntil(t, 10*time.Second, func() bool { return paneCaptures(fake, sess.TmuxSession) >= base+3 },
		"restored session stopped ticking")
	stop()

	timeline, err := os.ReadFile(filepath.Join(trk.SessionDir(), "timeline.md"))
	if err != nil {
		t.Fatalf("read timeline.md: %v", err)
	}
	text := string(timeline)
	if got := strings.Count(text, "ui-prompt | recognised"); got != 1 {
		t.Errorf("timeline: %d 'recognised' ui-prompt line(s), want exactly 1:\n%s", got, text)
	}
	if got := strings.Count(text, "ui-prompt | cleared"); got != 1 {
		t.Errorf("timeline: %d 'cleared' ui-prompt line(s), want exactly 1:\n%s", got, text)
	}
	// ui-prompt entries are timeline-only: they must not leak into
	// conversation.md.
	conv, _ := os.ReadFile(filepath.Join(trk.SessionDir(), "conversation.md"))
	if strings.Contains(string(conv), "ui-prompt") {
		t.Fatalf("ui-prompt entries leaked into conversation.md: %s", conv)
	}
}
