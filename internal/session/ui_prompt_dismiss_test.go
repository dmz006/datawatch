package session

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// These tests drive the UI prompt dismissal path (ui_prompt_dismiss.go)
// through the real screen-capture loop (20 ms ticks) on a FakeTmux, with a
// controllable Manager.clock so the delay / clear-bound / attempts-cap
// gates are exact. The 400k autocompact fixtures in testdata/interstitial
// are the canonical blocked pane (the cursor sits on the setting-changing
// "Autocompact at 400k" row, so Escape is the ONLY safe dismiss).

// uiPromptDismissHooks captures the audit events the manager emits through
// SetUIPromptAuditHook.
type uiPromptDismissHooks struct {
	mu     sync.Mutex
	events []UIPromptAuditEvent
}

func (h *uiPromptDismissHooks) add(e UIPromptAuditEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, e)
}

func (h *uiPromptDismissHooks) count(outcome string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, e := range h.events {
		if e.Outcome == outcome {
			n++
		}
	}
	return n
}

func (h *uiPromptDismissHooks) first(outcome string) (UIPromptAuditEvent, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, e := range h.events {
		if e.Outcome == outcome {
			return e, true
		}
	}
	return UIPromptAuditEvent{}, false
}

func uiPromptDismissFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "interstitial", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return string(b)
}

// setFakePane replaces the fake tmux's pane content for a session (same
// mechanism the existing interstitial tests use).
func setFakePane(fake *FakeTmux, session, content string) {
	fake.mu.Lock()
	fake.Pane[session] = content
	fake.mu.Unlock()
}

// testUIPromptDismissHarness boots the shared pieces: manager + fake, a
// running session, the blocked 400k pane, the audit hook, and the capture
// loop. clockNow starts at t0; tests advance it to make the dismissal
// gates pass deterministically (the manager's m.now() feeds both the
// classification clock on scanInterstitialTick and the send timing).
type uiPromptDismissHarness struct {
	t        *testing.T
	mgr      *Manager
	sess     *Session
	fake     *FakeTmux
	hooks    *uiPromptDismissHooks
	clockNow *time.Time
	fixed    time.Time
	stop     func()
}

func newUIPromptDismissHarness(t *testing.T) *uiPromptDismissHarness {
	t.Helper()
	mgr, fake := newTestManagerWithFake(t)
	sess := &Session{
		ID: "u001", FullID: "testhost-u001", Hostname: "testhost",
		TmuxSession: "cs-u001", BackendFamily: "claude-code",
		State:     StateRunning,
		Task:      "interactive task",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	fixed := time.Now()
	h := &uiPromptDismissHarness{
		t:        t,
		mgr:      mgr,
		sess:     sess,
		fake:     fake,
		hooks:    &uiPromptDismissHooks{},
		clockNow: &fixed,
		fixed:    fixed,
	}
	mgr.clock = func() time.Time { return *h.clockNow }
	mgr.SetUIPromptAuditHook(h.hooks.add)

	setFakePane(fake, sess.TmuxSession, uiPromptDismissFixture(t, "autocompact-enter-would-pick.txt"))
	stop := startCapture(t, mgr, sess)
	h.stop = stop
	t.Cleanup(h.stop)
	return h
}

// tickUntil drives the capture loop forward past its content-changed gate
// (mutatePane on each poll iteration, as scanTicks does) while polling for
// a condition — this is how time + static panes still produce scans.
func (h *uiPromptDismissHarness) tickUntil(timeout time.Duration, cond func() bool, msg string) {
	h.t.Helper()
	pollUntil(h.t, timeout, func() bool {
		mutatePane(h.fake, h.sess.TmuxSession)
		return cond()
	}, msg)
}

func (h *uiPromptDismissHarness) pollPlain(timeout time.Duration, cond func() bool, msg string) {
	h.t.Helper()
	pollUntil(h.t, timeout, cond, msg)
}

// park waits for the capture loop to recognise the blocked pane and move
// the session to waiting_input with the prompt exposed.
func (h *uiPromptDismissHarness) park() {
	h.t.Helper()
	h.tickUntil(10*time.Second, func() bool {
		return h.sess.State == StateWaitingInput && h.mgr.UIPromptFor(h.sess.FullID) != nil
	}, "session parked on the interstitial prompt")
}

// keySends returns the number of send-keys-literal calls and, when
// non-zero, the literal of the last one.
func (h *uiPromptDismissHarness) keySends() (int, string) {
	h.t.Helper()
	n := h.fake.Count("send-keys-literal")
	if n == 0 {
		return 0, ""
	}
	h.fake.mu.Lock()
	defer h.fake.mu.Unlock()
	last := ""
	for i := len(h.fake.Calls) - 1; i >= 0; i-- {
		if h.fake.Calls[i].Op == "send-keys-literal" && len(h.fake.Calls[i].Args) >= 1 {
			last = h.fake.Calls[i].Args[0]
			break
		}
	}
	return n, last
}

// keySent asserts exactly one send happened and it was the single ESC byte
// (the same key DeclineStartupDialog sends — never Enter).
func (h *uiPromptDismissHarness) keySent() {
	h.t.Helper()
	n, last := h.keySends()
	if n != 1 {
		t := h.t
		t.Fatalf("send-keys-literal count = %d, want exactly 1; calls: %+v", n, h.fake.Calls)
	}
	if last != "\x1b" {
		t := h.t
		t.Fatalf("last literal = %q, want single ESC byte (\\x1b)", last)
	}
}

// TestUIPromptDismiss_400kAutocompactFixtureAutoDismissal is the canonical
// happy path: the 400k autocompact pane parks the session; after the 5 s
// delay the manager sends exactly the classifier's SAFE key (Esc); when
// the pane returns to the idle prompt the session restores to running and
// the dismissed outcome is booked once.
func TestUIPromptDismiss_400kAutocompactFixtureAutoDismissal(t *testing.T) {
	h := newUIPromptDismissHarness(t)
	h.park()

	// Still blocked, and nothing sent yet (delay gate).
	if n, _ := h.keySends(); n != 0 {
		t.Fatalf("key sent before the dismiss delay: %d sends", n)
	}

	// Pass the delay (firstSeen + 5 s + margin).
	*h.clockNow = h.clockNow.Add(6 * time.Second)
	h.tickUntil(10*time.Second, func() bool { return h.fake.Count("send-keys-literal") == 1 },
		"one dismiss key sent after the delay")
	h.keySent()

	// The manager re-captured right before the send (Rule 1).
	if h.fake.Count("capture-live-tail") == 0 {
		t.Error("expected a capture-live-tail immediately before the send (re-capture guard)")
	}

	// The pane returns to the idle prompt: the session must restore and the
	// dismissed outcome book exactly once.
	setFakePane(h.fake, h.sess.TmuxSession, uiPromptDismissFixture(t, "idle-prompt.txt"))
	h.tickUntil(10*time.Second, func() bool {
		return h.sess.State == StateRunning && h.mgr.UIPromptFor(h.sess.FullID) == nil
	}, "session back to running and the UI prompt record cleared")
	if h.sess.UIPrompt != nil {
		t.Errorf("sess.UIPrompt should be cleared after the prompt leaves the pane")
	}

	ev, ok := h.hooks.first(UIPromptOutcomeDismissed)
	if !ok {
		t.Fatalf("no dismissed audit event; got %d events: %+v", len(h.hooks.events), h.hooks.events)
	}
	if h.hooks.count(UIPromptOutcomeDismissed) != 1 {
		t.Fatalf("dismissed booked %d times, want exactly 1", h.hooks.count(UIPromptOutcomeDismissed))
	}
	if h.hooks.count(UIPromptOutcomeNotCleared) != 0 || h.hooks.count(UIPromptOutcomeRefused) != 0 {
		t.Fatalf("unexpected other outcomes: %+v", h.hooks.events)
	}
	if ev.SessionFullID != h.sess.FullID || ev.Kind != "autocompact-threshold" ||
		ev.Key != "Esc" || ev.Attempt != 1 || !strings.Contains(ev.SelectedOption, "Autocompact at 400k") {
		t.Fatalf("dismissed event wrong: %+v", ev)
	}
}

// TestUIPromptDismiss_MaxAttemptsKeepsParkedThenNotCleared: the pane stays
// blocked the whole time (a static pane — Esc does not clear it in the
// fake). The manager sends at most ui_prompt_max_attempts keys (2 by
// default), then books the not_cleared outcome exactly once and stays
// PARKED with the prompt still exposed — and never sends or books again.
func TestUIPromptDismiss_MaxAttemptsKeepsParkedThenNotCleared(t *testing.T) {
	h := newUIPromptDismissHarness(t)
	h.park()

	// Attempt 1 clears the delay gate.
	*h.clockNow = h.clockNow.Add(6 * time.Second)
	h.tickUntil(10*time.Second, func() bool { return h.fake.Count("send-keys-literal") == 1 },
		"attempts: first key sent")

	// Attempt 2 clears the 30 s clear-bound after the last key.
	*h.clockNow = h.clockNow.Add(31 * time.Second)
	h.tickUntil(10*time.Second, func() bool { return h.fake.Count("send-keys-literal") == 2 },
		"attempts: second key sent")
	h.tickUntil(10*time.Second, func() bool { return h.hooks.count(UIPromptOutcomeNotCleared) == 1 },
		"attempts cap exhausted: not_cleared booked")

	// Parked, still exposed, and the record knows both attempts.
	if h.sess.State == StateRunning {
		t.Fatal("session must stay parked (waiting_input) after the attempts cap — the prompt is still on screen")
	}
	if info := h.mgr.UIPromptFor(h.sess.FullID); info == nil || info.DismissAttempts != 2 {
		t.Fatalf("UIPromptFor after cap = %+v, want non-nil with DismissAttempts=2", info)
	}

	// Stable: more ticks (clock advances further) must add no sends, no
	// new bookings.
	*h.clockNow = h.clockNow.Add(60 * time.Second)
	time.Sleep(500 * time.Millisecond)
	if n, _ := h.keySends(); n != 2 {
		t.Fatalf("sends after the cap = %d, want 2", n)
	}
	if h.hooks.count(UIPromptOutcomeNotCleared) != 1 {
		t.Fatalf("not_cleared booked %d times, want exactly 1", h.hooks.count(UIPromptOutcomeNotCleared))
	}
	ev, ok := h.hooks.first(UIPromptOutcomeNotCleared)
	if !ok {
		t.Fatal("no not_cleared event")
	}
	if ev.Key != "Esc" || ev.Kind != "autocompact-threshold" ||
		ev.SessionFullID != h.sess.FullID || ev.Attempt != 2 {
		t.Fatalf("not_cleared event wrong: %+v", ev)
	}
}

// TestUIPromptDismiss_ClearedDuringDelayNeverSends: the prompt left the
// pane before the dismiss delay ever elapsed (an operator dismissed it by
// hand, or it timed out). Nothing is ever sent on this occurrence,
// nothing is booked (no key was sent, so there is no dismissed outcome to
// book, and the record is gone so not_cleared cannot fire), and the
// session restores cleanly on the clear path.
func TestUIPromptDismiss_ClearedDuringDelayNeverSends(t *testing.T) {
	h := newUIPromptDismissHarness(t)
	h.park()

	// Still inside the 5 s delay window: the pane comes back to the idle
	// prompt → clear path restores the session and drops the record.
	setFakePane(h.fake, h.sess.TmuxSession, uiPromptDismissFixture(t, "idle-prompt.txt"))
	h.tickUntil(10*time.Second, func() bool {
		return h.sess.State == StateRunning && h.mgr.UIPromptFor(h.sess.FullID) == nil
	}, "session restored to running on the clear path")

	// Far past the delay, clear bound and attempts cap: still nothing
	// sent, nothing booked, the idle pane still classifies as nothing.
	*h.clockNow = h.clockNow.Add(120 * time.Second)
	time.Sleep(500 * time.Millisecond) // extra capture ticks

	if n, literal := h.keySends(); n != 0 {
		t.Fatalf("key sent: %d (literal %q) — the prompt had already cleared; nothing may be sent", n, literal)
	}
	for _, o := range []string{UIPromptOutcomeDismissed, UIPromptOutcomeNotCleared, UIPromptOutcomeRefused} {
		if h.hooks.count(o) != 0 {
			t.Fatalf("outcome %s booked on an occurrence that never sent a key: %+v", o, h.hooks.events)
		}
	}
	if info := h.mgr.UIPromptFor(h.sess.FullID); info != nil {
		t.Fatalf("ui prompt still exposed after clear: %+v", info)
	}
}

// TestUIPromptDismiss_DelayHonored: with the default 5 s delay, ticks in
// [0, 5 s) after firstSeen send nothing; at firstSeen+5 s the first key
// goes out.
func TestUIPromptDismiss_DelayHonored(t *testing.T) {
	h := newUIPromptDismissHarness(t)
	h.park()
	st := h.mgr.UIPromptSettings()
	if st.DelaySeconds != UIPromptDefaultDelaySeconds {
		t.Fatalf("test premise: default delay = %d s, want %d", st.DelaySeconds, UIPromptDefaultDelaySeconds)
	}

	// 3 s after firstSeen: inside the delay window — nothing sent, even
	// over many ticks.
	*h.clockNow = h.clockNow.Add(3 * time.Second)
	time.Sleep(500 * time.Millisecond)
	if n, _ := h.keySends(); n != 0 {
		t.Fatalf("key sent %d s after firstSeen, want nothing before %d s", 3, st.DelaySeconds)
	}

	// 6 s after firstSeen: outside the delay — the first key goes.
	*h.clockNow = h.clockNow.Add(3 * time.Second)
	h.tickUntil(10*time.Second, func() bool { return h.fake.Count("send-keys-literal") == 1 },
		"first key sent once the delay elapsed")
	if n, _ := h.keySends(); n != 1 {
		t.Fatalf("sends = %d, want 1", n)
	}
}

// TestUIPromptDismiss_SettingOffNeverSends: session.dismiss_ui_prompts off
// is the master switch — the manager never sends any key for these
// prompts, whatever the schedule says, and the session stays parked
// (detection only, as before this feature).
func TestUIPromptDismiss_SettingOffNeverSends(t *testing.T) {
	h := newUIPromptDismissHarness(t)
	h.mgr.SetUIPromptSettings(func() UIPromptSettings {
		return UIPromptSettings{Enabled: false, DelaySeconds: 0, ClearBoundSeconds: 1, MaxAttempts: 10}
	})
	h.park()

	// Advance far past any delay/bound/cap.
	*h.clockNow = h.clockNow.Add(120 * time.Second)
	time.Sleep(500 * time.Millisecond)

	if n, literal := h.keySends(); n != 0 {
		t.Fatalf("key sent with dismissal disabled: %d (literal %q)", n, literal)
	}
	for _, o := range []string{UIPromptOutcomeDismissed, UIPromptOutcomeNotCleared, UIPromptOutcomeRefused} {
		if h.hooks.count(o) != 0 {
			t.Fatalf("outcome %s booked with dismissal disabled: %+v", o, h.hooks.events)
		}
	}
	if h.sess.State == StateRunning {
		t.Fatal("session should still be parked with dismissal off")
	}
	if h.mgr.UIPromptFor(h.sess.FullID) == nil {
		t.Fatal("UI prompt must stay exposed with dismissal off")
	}
}

// TestUIPromptDismiss_UnsafeKeyRefusedCountsOnce: an occurrence whose
// classifier list holds only unsafe keys (Enter, C-m) is refused by the
// send-site guard — NOTHING is sent, the refusal is counted and audited
// once (first unsafe candidate named), and the occurrence ends not_cleared
// when the cap is used up.
func TestUIPromptDismiss_UnsafeKeyRefusedCountsOnce(t *testing.T) {
	h := newUIPromptDismissHarness(t)
	// HOLD every send while the occurrence is parked (the capture loop
	// recognises the pane immediately): the dismiss delay stays far in the
	// future, so nothing can send before the dismiss list is turned into an
	// unsafe one.
	h.mgr.SetUIPromptSettings(func() UIPromptSettings {
		return UIPromptSettings{Enabled: true, DelaySeconds: 120, ClearBoundSeconds: 1, MaxAttempts: 1}
	})
	h.park()

	// Simulate a classifier entry whose only dismiss keys are unsafe.
	h.mgr.uiPromptMu.Lock()
	rec := h.mgr.uiPromptState[h.sess.FullID]
	if rec == nil {
		h.mgr.uiPromptMu.Unlock()
		t.Fatal("no UI prompt record after parking")
	}
	rec.dismiss = []string{"enter", "c-m"}
	h.mgr.uiPromptMu.Unlock()

	// Release the hold: the next scans see only unsafe keys → one refusal
	// (consumes the cap), then the not_cleared booking.
	h.mgr.SetUIPromptSettings(func() UIPromptSettings {
		return UIPromptSettings{Enabled: true, DelaySeconds: 0, ClearBoundSeconds: 1, MaxAttempts: 1}
	})
	h.pollPlain(10*time.Second, func() bool {
		return h.hooks.count(UIPromptOutcomeRefused) == 1 && h.hooks.count(UIPromptOutcomeNotCleared) == 1
	}, "refused (once) then not_cleared booked")

	if n, literal := h.keySends(); n != 0 {
		t.Fatalf("guard-refused key was sent: %d sends (literal %q)", n, literal)
	}
	ev, ok := h.hooks.first(UIPromptOutcomeRefused)
	if !ok {
		t.Fatal("no refused audit event")
	}
	if ev.Key != "enter" || ev.Attempt != 1 { // first unsafe candidate, the consumed attempt
		t.Fatalf("refused event Key = %q, want the first unsafe candidate %q", ev.Key, "enter")
	}
	if ev.SessionFullID != h.sess.FullID || ev.Kind != "autocompact-threshold" {
		t.Fatalf("refused event fields wrong: %+v", ev)
	}
	if n := h.hooks.count(UIPromptOutcomeRefused); n != 1 {
		t.Fatalf("refused booked %d times, want exactly 1 (no audit spam)", n)
	}
	// Still parked — the pane still shows the prompt and dismissal gave up.
	if h.sess.State == StateRunning {
		t.Fatal("session must remain parked after refusal + cap")
	}
}

// TestUIPromptDismiss_GuardRejectsDangerousKeys is the send-site guard
// itself, table-driven: the ONLY name it maps to a literal is Esc (and
// only to the single ESC byte); Enter / C-m / newline / CR / TAB / SPACE /
// digits / anything unknown are refused. The guard is independent of the
// classifier's allowlist — a broken table entry cannot break out of it.
func TestUIPromptDismiss_GuardRejectsDangerousKeys(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"esc", "\x1b"},
		{"ESC", "\x1b"},
		{" Esc ", "\x1b"},
		{"enter", ""},
		{"Enter", ""},
		{"return", ""},
		{"c-m", ""},
		{"C-M", ""},
		{"ctrl+m", ""},
		{"ctrl-m", ""},
		{"newline", ""},
		{"carriage return", ""},
		{"\r", ""},
		{"\n", ""},
		{"\r\n", ""},
		{"\t", ""},
		{"tab", ""},
		{" ", ""},
		{"space", ""},
		{"0", ""},
		{"1", ""},
		{"5", ""},
		{"9", ""},
		{"42", ""},
		{"", ""},
		{"q", ""},
		{"up", ""},
	}
	for _, c := range cases {
		if got := dismissKeyLiteral(c.in); got != c.want {
			t.Errorf("dismissKeyLiteral(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestUIPromptDismiss_EscIsTheOnlyLiteralSends checks the one literal the
// whole feature can produce at the send site: the ESC byte, exactly as
// DeclineStartupDialog sends it — one byte, not a newline or CR (the keys
// that would CONFIRM the dangerously-cursor-selected setting).
func TestUIPromptDismiss_EscIsTheOnlyLiteralSends(t *testing.T) {
	if got := dismissKeyLiteral("esc"); got != "\x1b" {
		t.Fatalf("Esc must map to the single ESC byte; got %q", got)
	}
	if len("\x1b") != 1 || "\x1b" == "\r" || "\x1b" == "\n" || "\x1b" == "\t" {
		t.Fatalf("the Esc literal must be exactly one ESC byte; got %q", "\x1b")
	}
	// Names that are NOT Esc (even similar ones) must map to nothing.
	for _, name := range []string{"escape", "escaper", "q", "up"} {
		if got := dismissKeyLiteral(name); got != "" {
			t.Fatalf("dismissKeyLiteral(%q) = %q — only Esc may map to a literal", name, got)
		}
	}
}
