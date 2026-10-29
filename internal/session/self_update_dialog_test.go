// Claude Code self-update notice ("✔ Update installed · /restart to apply").
//
// Found live 2026-10-14 (sessions 348b/258d): one-shot workers sat with the
// CLI's update notice in the footer while datawatch kept re-pasting their
// task. docs/plans/2026-10-14-cli-self-update-dialog-recovery.md established
// that the notice is a passive footer, not a modal: it takes no focus and the
// CLI keeps working, so the correct "dismiss" is no keystroke at all (Enter
// would submit the input box, Esc would interrupt a running turn). These
// tests pin that: the notice is recognised, never sent a key, never treated
// as a prompt, never moves the session to a terminal/failed/rate-limited
// state, and counts as activity.

package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func loadSelfUpdateFixture(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("testdata/claude_self_update_dialog.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return strings.Split(string(raw), "\n")
}

// sentKeystrokes returns every fake-tmux call that would have typed into the pane.
func sentKeystrokes(f *FakeTmux) []FakeTmuxCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []FakeTmuxCall
	for _, c := range f.Calls {
		if strings.HasPrefix(c.Op, "send-keys") || c.Op == "SendTrustPromptAccept" {
			out = append(out, c)
		}
	}
	return out
}

func TestSelfUpdatePatterns_MatchRealFixtureWording(t *testing.T) {
	m := &Manager{}
	var notice []string
	for _, l := range loadSelfUpdateFixture(t) {
		if strings.Contains(strings.ToLower(l), "update installed") {
			notice = append(notice, l)
		}
	}
	if len(notice) == 0 {
		t.Fatal("fixture has no update-notice line — fixture changed?")
	}
	// Every shape the notice was captured in, plus the segment on its own
	// and case/spacing variants the TUI redraw can produce.
	notice = append(notice,
		"✔ Update installed · /restart to apply",
		"  ✔ UPDATE INSTALLED · /RESTART TO APPLY  ",
		"Update installed · Restart to apply",
	)
	for _, l := range notice {
		if !m.matchesSelfUpdateDialog(l) {
			t.Errorf("selfUpdatePatterns missed real notice line %q", l)
		}
	}
}

func TestSelfUpdatePatterns_DoNotMatchOtherOutput(t *testing.T) {
	m := &Manager{}
	others := []string{
		// ordinary claude-code output
		"● I'll start by reading the plan doc and the fixture.",
		"⎿  Read 120 lines",
		"❯ ",
		"  ⏵⏵ bypass permissions on (shift+tab to cycle)",
		"  paste again to expand",
		"[Pasted text #55 +81 lines]",
		"The installer updated go.mod; restart the daemon to apply the config change.",
		"Run `claude update` to install the latest version.",
		// rate-limit dialog
		"5-hour limit reached ∙ resets 2pm",
		"❯ 1. Stop and wait for limit to reset",
		"You're out of extra usage · resets 11:50am (America/New_York)",
		// folder-trust dialog
		"Quick safety check: Is this a project you created or one you trust?",
		"❯ 1. Yes, I trust this folder",
		"  2. No, exit",
		// development-channels dialog (the real "Enter to confirm" source)
		"WARNING: Loading development channels",
		"❯ 1. I am using this for local development",
		"Enter to confirm · Esc to cancel",
		// permission prompt
		"Do you want to proceed?",
		"❯ 1. Yes",
		"  2. Yes, and don't ask again for go test commands",
		"  3. No, and tell Claude what to do differently (esc)",
	}
	for _, l := range others {
		if m.matchesSelfUpdateDialog(l) {
			t.Errorf("selfUpdatePatterns false-positive on %q", l)
		}
	}
}

func TestSelfUpdatePatterns_NotInPromptOrRateLimitLists(t *testing.T) {
	// The notice must stay non-blocking: it must not have been added to the
	// prompt or rate-limit lists, and those lists must not match it.
	m := &Manager{}
	line := "paste again to expand ✔ Update installed · /restart to apply"
	if matched, _ := m.matchPromptInLines([]string{line}, 10); matched != "" {
		t.Errorf("update notice classified as a prompt (matched %q)", matched)
	}
	for family := range builtinRateLimitPhrases {
		if matchesBuiltinRateLimitLines(family, line) {
			t.Errorf("update notice matches the %s rate-limit table", family)
		}
	}
}

func TestSelfUpdateDialog_FixtureIsNotAPrompt(t *testing.T) {
	m := &Manager{}
	if matched, _ := m.matchPromptInLines(loadSelfUpdateFixture(t), 10); matched != "" {
		t.Errorf("fixture pane classified as waiting-for-input via line %q", matched)
	}
}

func newSelfUpdateSession(t *testing.T, mgr *Manager, state State) *Session {
	t.Helper()
	sess := &Session{
		ID: "348b", FullID: "testhost-348b", Hostname: "testhost",
		TmuxSession: "cs-348b", BackendFamily: "claude-code",
		OneShot: true, State: state, Task: "verify the thing",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
		LastChannelEventAt: time.Now().Add(-10 * time.Minute),
	}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatal(err)
	}
	return sess
}

func TestSelfUpdateDialog_ProcessOutputLineSendsNoKeyAndKeepsSessionAlive(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := newSelfUpdateSession(t, mgr, StateRunning)
	before := sess.LastChannelEventAt

	var lastOutputTime, lastPromptMatchTime time.Time
	var pendingLines []string
	lines := loadSelfUpdateFixture(t)
	// The TUI redraws the footer constantly; feed the block several times
	// back-to-back, all inside the debounce window.
	for pass := 0; pass < 3; pass++ {
		for _, l := range lines {
			mgr.processOutputLine(context.Background(), sess, nil, l, &lastOutputTime, &pendingLines, &lastPromptMatchTime, func() *Tracker { return nil })
		}
	}
	// processOutputLine is synchronous and the manager exposes no observable
	// "pending async work" counter, so a bounded settle is the only way to
	// give a hypothetical stray key-sending goroutine a chance to fire before
	// asserting that none did. Kept short; a regression would send within ms.
	time.Sleep(100 * time.Millisecond)

	if keys := sentKeystrokes(fake); len(keys) != 0 {
		t.Errorf("passive update notice must not be sent any keystroke, got %d: %+v", len(keys), keys)
	}
	got, _ := mgr.GetSession(sess.FullID)
	switch got.State {
	case StateComplete, StateFailed, StateKilled, StateRateLimited, StateWaitingInput:
		t.Errorf("update notice moved session to %s; want it left Running", got.State)
	}
	if !got.LastChannelEventAt.After(before) {
		t.Errorf("activity timestamp not refreshed by update notice (still %v)", got.LastChannelEventAt)
	}
	mgr.mu.Lock()
	_, noted := mgr.selfUpdateNotedAt[sess.FullID]
	mgr.mu.Unlock()
	if !noted {
		t.Error("update notice was not recognised by processOutputLine")
	}
}

func TestSelfUpdateDialog_NoteIsDebouncedPerSession(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := newSelfUpdateSession(t, mgr, StateRunning)

	if !mgr.noteSelfUpdateDialog(sess.FullID) {
		t.Fatal("first sighting should be handled")
	}
	first, _ := mgr.GetSession(sess.FullID)
	firstAt := first.LastChannelEventAt
	for i := 0; i < 5; i++ {
		if mgr.noteSelfUpdateDialog(sess.FullID) {
			t.Fatalf("repeat sighting %d within the debounce window was handled again", i+1)
		}
	}
	again, _ := mgr.GetSession(sess.FullID)
	if !again.LastChannelEventAt.Equal(firstAt) {
		t.Error("debounced repeat sightings should not re-handle the notice")
	}
	if !mgr.noteSelfUpdateDialog("testhost-other") {
		t.Error("debounce must be per session")
	}
	if keys := sentKeystrokes(fake); len(keys) != 0 {
		t.Errorf("noteSelfUpdateDialog sent keystrokes: %+v", keys)
	}
}

func TestSelfUpdateDialog_ScreenCaptureSendsNoKeyAndDoesNotFlipToWaiting(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := newSelfUpdateSession(t, mgr, StateRunning)
	fake.Pane[sess.TmuxSession] = strings.Join(loadSelfUpdateFixture(t), "\n")

	ctx, cancel := context.WithCancel(context.Background())
	mgr.StartScreenCapture(ctx, sess.FullID, 20)
	// Change the pane between ticks so detection actually runs repeatedly,
	// until the notice has been recognised by the scan.
	pollUntil(t, 10*time.Second, func() bool {
		mutatePane(fake, sess.TmuxSession)
		mgr.mu.Lock()
		defer mgr.mu.Unlock()
		_, noted := mgr.selfUpdateNotedAt[sess.FullID]
		return noted
	}, "update notice was not recognised by the screen-capture scan")
	cancel()
	waitCaptureLoopsExit(t, 10*time.Second)

	if keys := sentKeystrokes(fake); len(keys) != 0 {
		t.Errorf("screen capture sent keystrokes for the update notice: %+v", keys)
	}
	got, _ := mgr.GetSession(sess.FullID)
	if got.State != StateRunning {
		t.Errorf("state = %s, want Running (notice is not a prompt and not terminal)", got.State)
	}
	mgr.mu.Lock()
	_, noted := mgr.selfUpdateNotedAt[sess.FullID]
	mgr.mu.Unlock()
	if !noted {
		t.Error("update notice was not recognised by the screen-capture scan")
	}
}

func TestTaskDelivery_ClaimedOnceAndSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	allowTestTemp(mgr)
	mgr.WithFakeTmux()
	sess := newSelfUpdateSession(t, mgr, StateWaitingInput)

	if mgr.TaskDelivered(sess.FullID) {
		t.Fatal("fresh session reported as already delivered")
	}
	if !mgr.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("first claim should succeed")
	}
	if mgr.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("second claim must fail — task already delivered")
	}
	// Failed send → released, so a later attempt may deliver.
	mgr.ReleaseTaskDelivery(sess.FullID)
	if !mgr.ClaimTaskDelivery(sess.FullID) {
		t.Fatal("claim after release should succeed")
	}

	// Daemon restart: a fresh Manager on the same data dir must still see it.
	mgr2, err := NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	allowTestTemp(mgr2)
	if _, err := os.Stat(filepath.Join(dir, recordsDirName, recordsLayoutFile)); err != nil {
		t.Fatalf("store not persisted: %v", err)
	}
	if !mgr2.TaskDelivered(sess.FullID) {
		t.Error("task delivery record lost across restart — heartbeat would re-paste once per restart")
	}
	if mgr2.ClaimTaskDelivery(sess.FullID) {
		t.Error("claim after restart must fail")
	}
}
