package session

import (
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// Escalation tests: drive the same shared harness as ui_prompt_dismiss_test
// (FakeTmux + controllable clock + real capture loop) and check the two
// trigger conditions, the once-per-occurrence dedupe and the withdrawals.

type uiPromptEscalateHooks struct {
	mu        sync.Mutex
	raised    []UIPromptEscalateEvent
	withdrawn []UIPromptEscalateWithdrawEvent
	// raiseErr, when non-nil, is returned by the raise hook to simulate the
	// daemon failing to carry the escalation (it has then already sent its
	// one fallback alert); the hook still answers the question id it stored.
	raiseErr error
}

var errEscalateRaiseFailed = errors.New("fake: question store raise failed")

// raise stands in for the daemon-side raise: on success it records the
// event and returns a stable question id for it so the tests can assert
// on the round-trip into the UIPrompt record; on error it answers the way
// the daemon does — failure, and no stored question (empty id).
func (h *uiPromptEscalateHooks) raise(e UIPromptEscalateEvent) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.raised = append(h.raised, e)
	if h.raiseErr != nil {
		return "", h.raiseErr
	}
	return "q-escalate-" + e.EscalateToken[:8], nil
}

func (h *uiPromptEscalateHooks) addWithdrawn(e UIPromptEscalateWithdrawEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.withdrawn = append(h.withdrawn, e)
}

func (h *uiPromptEscalateHooks) raisedCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.raised)
}

func (h *uiPromptEscalateHooks) firstRaised() (UIPromptEscalateEvent, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.raised) == 0 {
		return UIPromptEscalateEvent{}, false
	}
	return h.raised[0], true
}

func (h *uiPromptEscalateHooks) allWithdrawn() []UIPromptEscalateWithdrawEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]UIPromptEscalateWithdrawEvent, len(h.withdrawn))
	copy(out, h.withdrawn)
	return out
}

// install wires the raise + withdraw hooks on the harness manager.
func (h *uiPromptDismissHarness) installEscalateHooks(hk *uiPromptEscalateHooks) {
	h.mgr.SetUIPromptQuestionHook(hk.raise)
	h.mgr.SetUIPromptEscalateWithdrawHook(hk.addWithdrawn)
}

// escalationQuestionID snapshots the question id the manager currently
// reports for this occurrence ("" when none was booked).
func (h *uiPromptDismissHarness) escalationQuestionID() string {
	info := h.mgr.UIPromptFor(h.sess.FullID)
	if info == nil {
		return ""
	}
	return info.QuestionID
}

// TestUIPromptEscalate_FiresAfterMaxAttemptsThenStillBlocked: the dismissal
// uses up every safe attempt and the pane stays blocked — the escalation
// fires exactly once (trigger attempts_exhausted), only after the clear
// bound following the last key, carrying the session identity, the prompt,
// what the daemon did, a clean screen tail and the stable token.
func TestUIPromptEscalate_FiresAfterMaxAttemptsThenStillBlocked(t *testing.T) {
	h := newUIPromptDismissHarness(t)
	hk := &uiPromptEscalateHooks{}
	h.installEscalateHooks(hk)
	h.mgr.SetUIPromptSettings(func() UIPromptSettings {
		return UIPromptSettings{Enabled: true, DelaySeconds: 1, ClearBoundSeconds: 2, MaxAttempts: 2}
	})
	h.park()

	// Both attempts go out (delay, then clear bound after key 1).
	*h.clockNow = h.clockNow.Add(2 * time.Second)
	h.tickUntil(10*time.Second, func() bool { return h.fake.Count("send-keys-literal") == 1 },
		"first safe dismiss key sent")
	*h.clockNow = h.clockNow.Add(3 * time.Second)
	h.tickUntil(10*time.Second, func() bool { return h.fake.Count("send-keys-literal") == 2 },
		"second safe dismiss key sent")

	// After the clear bound following the last key the occurrence is
	// exhausted: the escalation fires.
	*h.clockNow = h.clockNow.Add(3 * time.Second)
	h.tickUntil(10*time.Second, func() bool { return hk.raisedCount() == 1 },
		"escalation event fired once")

	ev, ok := hk.firstRaised()
	if !ok {
		t.Fatal("no escalation event")
	}
	if ev.Trigger != UIPromptEscalationTriggerAttemptsExhausted {
		t.Errorf("trigger = %q, want %q", ev.Trigger, UIPromptEscalationTriggerAttemptsExhausted)
	}
	if ev.SettingEnabled != true || ev.DismissAttempts != 2 || ev.LastKey != "Esc" || ev.LastKeyAt == nil {
		t.Errorf("daemon-so-far fields = %+v, want enabled, 2 attempts, last key Esc with a time", ev)
	}
	if ev.SessionFullID != h.sess.FullID || ev.SessionID != h.sess.ID {
		t.Errorf("session identity = %q/%q, want %q/%q", ev.SessionFullID, ev.SessionID, h.sess.FullID, h.sess.ID)
	}
	if ev.PromptKind != "autocompact-threshold" || !strings.Contains(ev.SelectedOption, "Autocompact at 400k") {
		t.Errorf("prompt = %q selected %q, want autocompact-threshold / Autocompact at 400k", ev.PromptKind, ev.SelectedOption)
	}
	if want := UIPromptEscalateToken(h.sess.FullID, "autocompact-threshold", ev.SelectedOption); ev.EscalateToken != want {
		t.Errorf("token = %q, want %q", ev.EscalateToken, want)
	}
	if len(ev.EscalateToken) != 64 {
		t.Errorf("token %q must be 64 hex chars", ev.EscalateToken)
	}
	if _, err := hex.DecodeString(ev.EscalateToken); err != nil {
		t.Errorf("token %q is not hex: %v", ev.EscalateToken, err)
	}
	if !strings.Contains(ev.ScreenTail, "Autocompact at 400k") {
		t.Errorf("screen tail does not contain the blocked prompt line:\n%s", ev.ScreenTail)
	}
	if strings.Contains(ev.ScreenTail, "DATAWATCH_QUESTION:") {
		t.Errorf("screen tail must not contain raw marker lines:\n%s", ev.ScreenTail)
	}

	// Stable across further ticks: never a second raise.
	*h.clockNow = h.clockNow.Add(120 * time.Second)
	time.Sleep(500 * time.Millisecond)
	if n := hk.raisedCount(); n != 1 {
		t.Fatalf("escalation fired %d times, want exactly 1", n)
	}
}

// TestUIPromptEscalate_SettingOff: dismissal is disallowed — no key is ever
// sent, and after the delay plus one clear bound the escalation fires with
// the setting_off trigger and an empty "daemon did" section.
func TestUIPromptEscalate_SettingOff(t *testing.T) {
	h := newUIPromptDismissHarness(t)
	hk := &uiPromptEscalateHooks{}
	h.installEscalateHooks(hk)
	h.mgr.SetUIPromptSettings(func() UIPromptSettings {
		return UIPromptSettings{Enabled: false, DelaySeconds: 5, ClearBoundSeconds: 3, MaxAttempts: 10}
	})
	h.park()

	// Five in past the delay but short of delay+clearbound: still nothing.
	*h.clockNow = h.clockNow.Add(5 * time.Second)
	time.Sleep(300 * time.Millisecond)
	if n := hk.raisedCount(); n != 0 {
		t.Fatalf("escalation fired inside the delay window (%d times)", n)
	}

	// Past delay + one clearbound: fires once, setting_off, zero attempts.
	*h.clockNow = h.clockNow.Add(4 * time.Second)
	h.tickUntil(10*time.Second, func() bool { return hk.raisedCount() == 1 },
		"setting_off escalation fired once")
	ev, _ := hk.firstRaised()
	if ev.Trigger != UIPromptEscalationTriggerSettingOff {
		t.Errorf("trigger = %q, want %q", ev.Trigger, UIPromptEscalationTriggerSettingOff)
	}
	if ev.SettingEnabled != false || ev.DismissAttempts != 0 || ev.LastKey != "" || ev.LastKeyAt != nil {
		t.Errorf("daemon-so-far = %+v, want disabled with zero attempts and no key", ev)
	}
	if n, _ := h.keySends(); n != 0 {
		t.Errorf("keys sent with dismissal disallowed: %d", n)
	}
}

// TestUIPromptEscalate_NilHooksAreSilent: with no escalation hook installed
// the manager parks, dismisses and gives up exactly as before — nothing
// panics, nothing is fired.
func TestUIPromptEscalate_NilHooksAreSilent(t *testing.T) {
	h := newUIPromptDismissHarness(t)
	h.park()
	*h.clockNow = h.clockNow.Add(120 * time.Second)
	time.Sleep(500 * time.Millisecond) // extra ticks across the whole lifecycle
	if h.sess.State == StateRunning {
		t.Fatal("pane still blocked: session must stay parked")
	}
}

// TestUIPromptEscalate_WithdrawOnClearAndOnEnd: once an occurrence is
// escalated, the clear path withdraws it (same token, reason prompt_cleared,
// exactly once), and the end path withdraws session-wide (empty token,
// reason session_ended).
func TestUIPromptEscalate_WithdrawOnClearAndOnEnd(t *testing.T) {
	h := newUIPromptDismissHarness(t)
	hk := &uiPromptEscalateHooks{}
	h.installEscalateHooks(hk)
	h.mgr.SetUIPromptSettings(func() UIPromptSettings {
		return UIPromptSettings{Enabled: false, DelaySeconds: 2, ClearBoundSeconds: 2, MaxAttempts: 10}
	})
	h.park()

	*h.clockNow = h.clockNow.Add(5 * time.Second)
	h.tickUntil(10*time.Second, func() bool { return hk.raisedCount() == 1 },
		"escalation fired")
	raised, _ := hk.firstRaised()

	// Clear path: the pane returns to the idle prompt → one withdrawal for
	// this occurrence.
	setFakePane(h.fake, h.sess.TmuxSession, uiPromptDismissFixture(t, "idle-prompt.txt"))
	h.tickUntil(10*time.Second, func() bool { return len(hk.allWithdrawn()) == 1 },
		"withdrawal fired on the clear path")
	evs := hk.allWithdrawn()
	if evs[0].SessionFullID != h.sess.FullID ||
		evs[0].EscalateToken != raised.EscalateToken ||
		evs[0].Reason != UIPromptEscalationReasonPromptCleared {
		t.Fatalf("withdrawal = %+v, want session %q token %q reason %q",
			evs[0], h.sess.FullID, raised.EscalateToken, UIPromptEscalationReasonPromptCleared)
	}

	// End path: session over → session-wide withdrawal, no occurrence token.
	h.mgr.emitUIPromptEscalationEnd(h.sess.FullID)
	evs = hk.allWithdrawn()
	if len(evs) != 2 {
		t.Fatalf("withdrawals = %d, want 2", len(evs))
	}
	if evs[1].SessionFullID != h.sess.FullID || evs[1].EscalateToken != "" ||
		evs[1].Reason != UIPromptEscalationReasonSessionEnded {
		t.Fatalf("end withdrawal = %+v, want empty token + reason session_ended", evs[1])
	}
}

// TestUIPromptEscalate_TokenIsStablePerOccurrence: the token is derived
// only from the session, the prompt kind and the selected option — a daemon
// restart over the same blocked pane must derive the SAME token so the
// question store dedupes it, while a different option (a different prompt
// occurrence) must derive a different one.
func TestUIPromptEscalate_TokenIsStablePerOccurrence(t *testing.T) {
	a := UIPromptEscalateToken("host-s1", InterstitialAutocompactThreshold, "Autocompact at 400k")
	b := UIPromptEscalateToken("host-s1", InterstitialAutocompactThreshold, "Autocompact at 400k")
	c := UIPromptEscalateToken("host-s1", InterstitialAutocompactThreshold, "Autocompact at 200k")
	d := UIPromptEscalateToken("host-s2", InterstitialAutocompactThreshold, "Autocompact at 400k")
	if a != b {
		t.Fatalf("same inputs derive different tokens: %q vs %q", a, b)
	}
	if a == c || a == d {
		t.Fatalf("different occurrences share the token %q", a)
	}
	if len(a) != 64 {
		t.Fatalf("token %q must be 64 hex chars", a)
	}
	if _, err := hex.DecodeString(a); err != nil {
		t.Fatalf("token %q must be lowercase hex: %v", a, err)
	}
}

// TestUIPromptEscalate_RecordsQuestionIDOnTheUIPromptRecord: the hook's
// answer — the question id the daemon raised — lands on the session's
// UIPrompt record (UIPromptInfo.QuestionID, persisted) and on the
// in-memory occurrence snapshot (UIPromptFor), so consumers can name the
// open question this escalation became.
func TestUIPromptEscalate_RecordsQuestionIDOnTheUIPromptRecord(t *testing.T) {
	h := newUIPromptDismissHarness(t)
	hk := &uiPromptEscalateHooks{}
	h.installEscalateHooks(hk)
	h.mgr.SetUIPromptSettings(func() UIPromptSettings {
		return UIPromptSettings{Enabled: false, DelaySeconds: 1, ClearBoundSeconds: 1, MaxAttempts: 10}
	})
	h.park()

	*h.clockNow = h.clockNow.Add(3 * time.Second)
	h.tickUntil(10*time.Second, func() bool { return hk.raisedCount() == 1 },
		"escalation fired once")

	if got := h.sess.UIPrompt; got == nil || got.QuestionID == "" {
		t.Fatalf("session UIPrompt record = %+v, want a non-empty QuestionID", h.sess.UIPrompt)
	}
	if got := h.escalationQuestionID(); got != h.sess.UIPrompt.QuestionID {
		t.Errorf("UIPromptFor snapshot QuestionID = %q, want %q (record value)", got, h.sess.UIPrompt.QuestionID)
	}
}

// TestUIPromptEscalate_HookErrorIsLoggedAndStaysOneShot: the hook reports
// the daemon could not carry the escalation (its one alert fallback already
// went out). The manager records no question id, logs the failure, and —
// critically — never re-raises the occurrence on later ticks.
func TestUIPromptEscalate_HookErrorIsLoggedAndStaysOneShot(t *testing.T) {
	h := newUIPromptDismissHarness(t)
	hk := &uiPromptEscalateHooks{raiseErr: errEscalateRaiseFailed}
	h.installEscalateHooks(hk)
	h.mgr.SetUIPromptSettings(func() UIPromptSettings {
		return UIPromptSettings{Enabled: false, DelaySeconds: 1, ClearBoundSeconds: 1, MaxAttempts: 10}
	})
	h.park()

	*h.clockNow = h.clockNow.Add(3 * time.Second)
	h.tickUntil(10*time.Second, func() bool { return hk.raisedCount() == 1 },
		"escalation fired once")

	if h.sess.UIPrompt != nil && h.sess.UIPrompt.QuestionID != "" {
		t.Errorf("QuestionID = %q after a failed raise, want empty", h.sess.UIPrompt.QuestionID)
	}
	if got := h.escalationQuestionID(); got != "" {
		t.Errorf("UIPromptFor snapshot QuestionID = %q after a failed raise, want empty", got)
	}

	// Well past any further window: the occurrence is still exactly once.
	*h.clockNow = h.clockNow.Add(120 * time.Second)
	time.Sleep(500 * time.Millisecond)
	if n := hk.raisedCount(); n != 1 {
		t.Fatalf("escalation fired %d times after a failed raise, want exactly 1", n)
	}
}

// TestUIPromptEscalate_ScreenTailIsLast40NonEmptyAndNeutralisesMarkers: the
// tail is exactly the last 40 non-empty lines (ANSI stripped, empty lines
// dropped), and marker lines are defused with a leading space while ordinary
// lines are untouched.
func TestUIPromptEscalate_ScreenTailIsLast40NonEmptyAndNeutralisesMarkers(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := &Session{
		ID: "e100", FullID: "testhost-e100", Hostname: "testhost",
		TmuxSession: "cs-e100", BackendFamily: "claude-code", State: StateRunning,
	}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	var lines []string
	lines = append(lines, "DATAWATCH_QUESTION: must not survive in the tail")
	lines = append(lines, "DATAWATCH_ANSWER: neither must this")
	lines = append(lines, "DATAWATCH_NEEDS_INPUT: or this one")
	for i := 0; i < 120; i++ {
		if i%3 == 0 {
			lines = append(lines, "") // blanks must be dropped
		} else {
			lines = append(lines, "line content "+string(rune('a'+i%26)))
		}
	}
	setFakePane(fake, sess.TmuxSession, "\x1b[1m"+strings.Join(lines, "\n")+"\x1b[0m")

	tail := mgr.escalationScreenTail(sess)
	got := strings.Split(tail, "\n")
	if len(got) != uiPromptEscalationTailLines {
		t.Fatalf("tail has %d lines, want %d", len(got), uiPromptEscalationTailLines)
	}
	// The tail must be the LAST 40 of the non-empty lines: it ends at the
	// very last content line and contains none of the three marker lines
	// raw. The non-empty lines are 80 "line content ..." lines, so lines 1..3
	// (markers) are NOT in the tail — their defusing is checked below.
	if !strings.Contains(tail, "line content") {
		t.Fatalf("tail lost its content lines: %q", tail)
	}
	for _, m := range []string{"DATAWATCH_QUESTION:", "DATAWATCH_ANSWER:", "DATAWATCH_NEEDS_INPUT:"} {
		if strings.Contains(tail, "\n"+m) || strings.HasPrefix(tail, m) {
			t.Errorf("tail still carries a raw %q line", m)
		}
	}

	// Direct check of the neutraliser: marker lines get a leading space,
	// everything else is untouched.
	if got := neutralizePromptMarkerLines("DATAWATCH_QUESTION: x"); got != " DATAWATCH_QUESTION: x" {
		t.Errorf("neutralise marker = %q", got)
	}
	if got := neutralizePromptMarkerLines("DATAWATCH_ANSWER: full"); got != " DATAWATCH_ANSWER: full" {
		t.Errorf("neutralise marker = %q", got)
	}
	if got := neutralizePromptMarkerLines("DATAWATCH_NEEDS_INPUT: disk full"); got != " DATAWATCH_NEEDS_INPUT: disk full" {
		t.Errorf("neutralise marker = %q", got)
	}
	if got := neutralizePromptMarkerLines("plain line"); got != "plain line" {
		t.Errorf("neutralise plain = %q", got)
	}
	if got := neutralizePromptMarkerLines("  DATAWATCH_QUESTION: indented"); got != "  DATAWATCH_QUESTION: indented" {
		t.Errorf("indented marker must stay untouched (not a line-start marker): %q", got)
	}
}
