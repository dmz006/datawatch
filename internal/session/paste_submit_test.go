package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testClock is the injected clock for confirmPromptSubmitted tests: the
// full 30-second budget elapses in zero real time.
type testClock struct {
	now    time.Time
	slept  time.Duration
	sleeps int
}

func useTestClock(t *testing.T) *testClock {
	t.Helper()
	tc := &testClock{now: time.Unix(0, 0)}
	oldSleep, oldNow := pasteSleep, pasteNow
	t.Cleanup(func() { pasteSleep, pasteNow = oldSleep, oldNow })
	pasteSleep = func(d time.Duration) {
		tc.slept += d
		tc.sleeps++
		tc.now = tc.now.Add(d)
	}
	pasteNow = func() time.Time { return tc.now }
	return tc
}

func loadPaneFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "paste", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

// TestPastePending_Fixtures pins the pure classifier against the two
// recorded panes.
func TestPastePending_Fixtures(t *testing.T) {
	if !pastePending(loadPaneFixture(t, "unsubmitted_84_lines.txt")) {
		t.Error("unsubmitted fixture: pastePending = false, want true")
	}
	if pastePending(loadPaneFixture(t, "submitted_running.txt")) {
		t.Error("submitted fixture: pastePending = true, want false")
	}
	if pastePending("❯ \n⏵⏵ bypass permissions on") {
		t.Error("clean idle prompt: pastePending = true, want false (no marker)")
	}
	// A marker sitting in the middle of the transcript (no prompt
	// affordance within the window) is historical, not pending.
	historic := "some older output\n[Pasted text #9 +12 lines]\nmore of the old transcript"
	if pastePending(historic) {
		t.Error("marker away from the input prompt: pastePending = true, want false")
	}
}

// TestConfirmPromptSubmitted_OneEnterWhenUnsubmitted: a pasted-but-
// unsubmitted pane must cause exactly one Enter, then succeed as soon
// as assistant activity is reported.
func TestConfirmPromptSubmitted_OneEnterWhenUnsubmitted(t *testing.T) {
	useTestClock(t)
	f := NewFakeTmux()
	if err := f.NewSessionWithSize("cs-paste-pending", 80, 24); err != nil {
		t.Fatal(err)
	}
	f.Pane["cs-paste-pending"] = loadPaneFixture(t, "unsubmitted_84_lines.txt")

	if err := confirmPromptSubmitted(f, "cs-paste-pending", func() bool { return true },
		30*time.Second, time.Second); err != nil {
		t.Fatalf("confirmPromptSubmitted: %v", err)
	}
	if got := f.Count("send-keys"); got != 1 {
		t.Fatalf("send-keys calls = %d, want exactly 1", got)
	}
	last := f.LastCall("send-keys")
	if len(last.Args) != 2 || last.Args[0] != "" || last.Args[1] != "Enter" {
		t.Fatalf("send-keys args = %v, want (\"\", Enter) — one bare Enter, no re-pasted text", last.Args)
	}
}

// TestConfirmPromptSubmitted_NoEnterWhenAlreadySubmitted: an already-
// submitted pane must not receive any keys at all.
func TestConfirmPromptSubmitted_NoEnterWhenAlreadySubmitted(t *testing.T) {
	useTestClock(t)
	f := NewFakeTmux()
	if err := f.NewSessionWithSize("cs-paste-done", 80, 24); err != nil {
		t.Fatal(err)
	}
	f.Pane["cs-paste-done"] = loadPaneFixture(t, "submitted_running.txt")

	activityCalls := 0
	if err := confirmPromptSubmitted(f, "cs-paste-done", func() bool {
		activityCalls++
		return true
	}, 30*time.Second, time.Second); err != nil {
		t.Fatalf("confirmPromptSubmitted: %v", err)
	}
	if got := f.Count("send-keys"); got != 0 {
		t.Fatalf("send-keys calls = %d, want 0 (marker already expanded)", got)
	}
	if activityCalls != 0 {
		t.Fatalf("activity check ran %d times, want 0 (nothing to confirm when not pending)", activityCalls)
	}
}

// TestConfirmPromptSubmitted_TimeoutWrapsSentinel: no assistant
// activity within the (injected) 30-second window must fail with an
// error wrapping ErrPromptNotSubmitted.
func TestConfirmPromptSubmitted_TimeoutWrapsSentinel(t *testing.T) {
	tc := useTestClock(t)
	f := NewFakeTmux()
	if err := f.NewSessionWithSize("cs-paste-stuck", 80, 24); err != nil {
		t.Fatal(err)
	}
	f.Pane["cs-paste-stuck"] = loadPaneFixture(t, "unsubmitted_84_lines.txt")

	const timeout = 30 * time.Second
	err := confirmPromptSubmitted(f, "cs-paste-stuck", func() bool { return false },
		timeout, time.Second)
	if err == nil {
		t.Fatal("confirmPromptSubmitted with no activity: nil error, want timeout error")
	}
	if !errors.Is(err, ErrPromptNotSubmitted) {
		t.Fatalf("error = %v, want errors.Is(err, ErrPromptNotSubmitted)", err)
	}
	if tc.sleeps == 0 {
		t.Fatal("polling loop never slept — the timeout budget was never consumed")
	}
	if tc.slept >= timeout {
		t.Fatalf("slept %v, want strictly under the %v budget (no sleep past the deadline)", tc.slept, timeout)
	}
	// Enter was still attempted exactly once before the wait.
	if got := f.Count("send-keys"); got != 1 {
		t.Fatalf("send-keys calls = %d, want exactly 1 (the single submit Enter)", got)
	}
}

// TestConfirmPromptSubmitted_ActivityOnSecondPoll: pending paste, first
// poll sees no activity, second poll sees it — must succeed after
// exactly one interval of polling.
func TestConfirmPromptSubmitted_ActivityOnSecondPoll(t *testing.T) {
	tc := useTestClock(t)
	f := NewFakeTmux()
	if err := f.NewSessionWithSize("cs-paste-slow", 80, 24); err != nil {
		t.Fatal(err)
	}
	f.Pane["cs-paste-slow"] = loadPaneFixture(t, "unsubmitted_84_lines.txt")

	polls := 0
	active := func() bool {
		polls++
		return polls > 1 // silent on the first poll, active on the second
	}
	if err := confirmPromptSubmitted(f, "cs-paste-slow", active,
		30*time.Second, 2*time.Second); err != nil {
		t.Fatalf("confirmPromptSubmitted: %v", err)
	}
	if polls != 2 {
		t.Fatalf("activity poll count = %d, want 2 (succeeded on the second poll)", polls)
	}
	if tc.sleeps != 1 || tc.slept != 2*time.Second {
		t.Fatalf("fake clock slept %dx over %v, want exactly one 2s interval before the second poll", tc.sleeps, tc.slept)
	}
	if got := f.Count("send-keys"); got != 1 {
		t.Fatalf("send-keys calls = %d, want exactly 1", got)
	}
}

func enterCount(f *FakeTmux) int { return f.Count("enter") }

func newSubmitFake(session string) *FakeTmux {
	f := NewFakeTmux()
	f.SubmitFlow = true
	f.Pane[session] = ""
	return f
}

const idleBox = "──────\n❯ \n──────"

// A short single-line message: tail visible, one Enter, no verify wait.
func TestSubmitPastedInput_ShortMessageFastPath(t *testing.T) {
	tc := useTestClock(t)
	f := newSubmitFake("s")
	f.PaneScript["s"] = []string{"──────\n❯ hello world\n──────"}
	if err := f.SendKeysWithSettle("s", "hello world\n", 0); err != nil {
		t.Fatal(err)
	}
	if got := enterCount(f); got != 1 {
		t.Fatalf("Enter count = %d, want 1", got)
	}
	if tc.sleeps != 0 {
		t.Errorf("fast path slept %d times (%v), want 0", tc.sleeps, tc.slept)
	}
}

// Placeholder appears late: poll waits, then exactly one Enter when the
// placeholder is gone after it.
func TestSubmitPastedInput_WaitsForPlaceholderThenOneEnter(t *testing.T) {
	useTestClock(t)
	f := newSubmitFake("s")
	ph := "──────\n❯ [Pasted text #1 +40 lines]\n──────"
	f.PaneScript["s"] = []string{idleBox, idleBox, ph, "⏺ working on it\n✻ Thinking"}
	if err := f.SendKeysWithSettle("s", strings.Repeat("line\n", 40)+"final words here", 0); err != nil {
		t.Fatal(err)
	}
	if got := enterCount(f); got != 1 {
		t.Fatalf("Enter count = %d, want 1 (calls: %v)", got, f.CallsSnapshot())
	}
}

// First Enter swallowed: placeholder still there, one retry, then it clears.
func TestSubmitPastedInput_RetriesWhileUnsent(t *testing.T) {
	useTestClock(t)
	f := newSubmitFake("s")
	ph := "──────\n❯ [Pasted text #1 +40 lines]\n──────"
	f.PaneScript["s"] = []string{ph, ph, "⏺ ok"}
	if err := f.SendKeysWithSettle("s", strings.Repeat("x\n", 40)+"tail text", 0); err != nil {
		t.Fatal(err)
	}
	if got := enterCount(f); got != 2 {
		t.Fatalf("Enter count = %d, want 2", got)
	}
}

// Placeholder never clears: retries are capped.
func TestSubmitPastedInput_RetryCapped(t *testing.T) {
	useTestClock(t)
	f := newSubmitFake("s")
	f.PaneScript["s"] = []string{"──────\n❯ [Pasted text #1 +40 lines]\n──────"}
	if err := f.SendKeysWithSettle("s", strings.Repeat("x\n", 40)+"tail text", 0); err != nil {
		t.Fatal(err)
	}
	if got, want := enterCount(f), 1+pasteMaxEnterRetries; got != want {
		t.Fatalf("Enter count = %d, want %d", got, want)
	}
}

// Assistant activity on screen stops any further Enter.
func TestSubmitPastedInput_NoRetryOnAssistantActivity(t *testing.T) {
	useTestClock(t)
	f := newSubmitFake("s")
	f.PaneScript["s"] = []string{
		"❯ [Pasted text #1 +40 lines]",
		"❯ [Pasted text #1 +40 lines]\n⏺ Reading files\n✻ Thinking",
	}
	if err := f.SendKeysWithSettle("s", strings.Repeat("x\n", 40)+"tail text", 0); err != nil {
		t.Fatal(err)
	}
	if got := enterCount(f); got != 1 {
		t.Fatalf("Enter count = %d, want 1", got)
	}
}

// Multi-line input without a placeholder: text still in the input box
// after Enter triggers a retry; empty input is a bare Enter, no polling.
func TestSubmitPastedInput_TextStillInInputBoxRetries(t *testing.T) {
	tc := useTestClock(t)
	f := newSubmitFake("s")
	stuck := "──────\n❯ second line of text\n──────"
	f.PaneScript["s"] = []string{stuck, stuck, "⏺ done"}
	if err := f.SendKeysWithSettle("s", "first\nsecond line of text", 0); err != nil {
		t.Fatal(err)
	}
	if got := enterCount(f); got != 2 {
		t.Fatalf("Enter count = %d, want 2", got)
	}
	f2 := newSubmitFake("s")
	before := tc.sleeps
	if err := f2.SendKeysWithSettle("s", "\n", 0); err != nil {
		t.Fatal(err)
	}
	if enterCount(f2) != 1 || f2.Count("capture-visible") != 0 || tc.sleeps != before {
		t.Errorf("empty input: want bare Enter with no capture/sleep, calls=%v", f2.CallsSnapshot())
	}
}
