package session

import (
	"errors"
	"os"
	"path/filepath"
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
