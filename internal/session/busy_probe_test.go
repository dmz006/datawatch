// Tests for the busy probe that keeps a silently working claude-code session
// Running. All tmux access goes through NewFakeTmux, and the child-process
// check is a fake, so these tests never start a real tmux server.

package session

import (
	"testing"
	"time"
)

const (
	busyPaneFrame = "⏺ Bash(go test ./...)\n" +
		"  ⎿  Running…\n" +
		"\n" +
		"✻ Crunching… (12s · esc to interrupt)\n"
	bareShellPane  = "user@host:~/repo$ "
	idleClaudePane = "⏺ Done.\n\n❯ \n? for shortcuts"
)

// busyProbeSession saves a session whose pane is named cs-<suffix> and
// returns it. The caller sets the pane content on the fake.
func busyProbeSession(t *testing.T, mgr *Manager, suffix string, state State, family string) *Session {
	t.Helper()
	now := time.Now()
	s := &Session{
		ID:            suffix,
		FullID:        "testhost-" + suffix,
		TmuxSession:   "cs-" + suffix,
		BackendFamily: family,
		OneShot:       true,
		State:         state,
		CreatedAt:     now,
		UpdatedAt:     now,
		Hostname:      "testhost",
	}
	if err := mgr.store.Save(s); err != nil {
		t.Fatalf("save: %v", err)
	}
	return s
}

// newBusyProbeManager returns a test manager with a fake tmux and a child
// check that reports no child processes unless a test overrides it.
func newBusyProbeManager(t *testing.T) (*Manager, *FakeTmux) {
	t.Helper()
	mgr := newTestManager(t)
	fake := NewFakeTmux()
	mgr.tmux = fake
	mgr.paneChildFn = func(string) bool { return false }
	return mgr, fake
}

func TestBusyProbe_PaneLooksBusy(t *testing.T) {
	cases := []struct {
		name     string
		capture  string
		child    bool
		wantBusy bool
	}{
		{"tool frame with esc to interrupt", busyPaneFrame, false, true},
		{"spinner line alone", "✻ Crunching… (3s)\n", false, true},
		{"finished turn is not busy", "✻ Crunched for 52s\n❯ \n", false, false},
		{"bare idle shell", bareShellPane, false, false},
		{"shell running a foreground command", "user@host:~/repo$ sleep 300", true, true},
		{"idle shell with no child", "user@host:~/repo$", true, false},
		{"idle claude pane with MCP helper child", idleClaudePane, true, false},
		{"empty capture without child", "", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := paneLooksBusy(tc.capture, tc.child); got != tc.wantBusy {
				t.Errorf("paneLooksBusy = %v, want %v", got, tc.wantBusy)
			}
		})
	}
}

func TestBusyProbe_DeliveryFlipsWaitingInputToRunning(t *testing.T) {
	mgr, fake := newBusyProbeManager(t)
	sess := busyProbeSession(t, mgr, "busy-deliver", StateWaitingInput, "claude-code")
	fake.Pane[sess.TmuxSession] = busyPaneFrame

	ts := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	if !mgr.applyBusyProbe(sess.FullID, ts) {
		t.Fatalf("applyBusyProbe returned false for a busy pane")
	}
	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateRunning {
		t.Errorf("state after busy probe: %s (want Running)", got.State)
	}
	if !got.LastChannelEventAt.Equal(ts) {
		t.Errorf("LastChannelEventAt = %v, want %v", got.LastChannelEventAt, ts)
	}
}

func TestBusyProbe_BareShellPaneStaysWaitingInput(t *testing.T) {
	mgr, fake := newBusyProbeManager(t)
	sess := busyProbeSession(t, mgr, "bare-shell", StateWaitingInput, "claude-code")
	fake.Pane[sess.TmuxSession] = bareShellPane

	if mgr.applyBusyProbe(sess.FullID, time.Time{}) {
		t.Fatalf("applyBusyProbe returned true for a bare shell pane")
	}
	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateWaitingInput {
		t.Errorf("state: %s (want WaitingInput)", got.State)
	}
	if !got.LastChannelEventAt.IsZero() {
		t.Errorf("LastChannelEventAt should stay zero, got %v", got.LastChannelEventAt)
	}
}

func TestBusyProbe_TerminalStatesStaySticky(t *testing.T) {
	mgr, fake := newBusyProbeManager(t)
	for _, st := range []State{StateComplete, StateFailed, StateKilled, StateRateLimited} {
		sess := busyProbeSession(t, mgr, "sticky-"+string(st), st, "claude-code")
		fake.Pane[sess.TmuxSession] = busyPaneFrame

		if mgr.applyBusyProbe(sess.FullID, time.Time{}) {
			t.Errorf("state %s: applyBusyProbe returned true", st)
		}
		got, _ := mgr.store.Get(sess.FullID)
		if got.State != st {
			t.Errorf("state %s with busy pane became %s", st, got.State)
		}
	}
}

func TestBusyProbe_WatcherGapDoesNotFlipBusySession(t *testing.T) {
	mgr, fake := newBusyProbeManager(t)
	sess := busyProbeSession(t, mgr, "gap-busy", StateRunning, "claude-code")
	t0 := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	sess.LastChannelEventAt = t0
	sess.UpdatedAt = t0
	_ = mgr.store.Save(sess)
	fake.Pane[sess.TmuxSession] = busyPaneFrame

	// Silent past the 15 s gap, but the pane shows work: stay Running and
	// restart the gap from this tick.
	mgr.runChannelStateWatcherTick(t0.Add(15*time.Second), 15*time.Second)
	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateRunning {
		t.Fatalf("busy session flipped to %s at the gap", got.State)
	}
	if !got.LastChannelEventAt.Equal(t0.Add(15 * time.Second)) {
		t.Errorf("LastChannelEventAt = %v, want the tick time", got.LastChannelEventAt)
	}

	// A later tick inside the restarted gap leaves it alone.
	mgr.runChannelStateWatcherTick(t0.Add(20*time.Second), 15*time.Second)
	got, _ = mgr.store.Get(sess.FullID)
	if got.State != StateRunning {
		t.Fatalf("busy session flipped to %s inside the restarted gap", got.State)
	}

	// Once the pane goes quiet, the normal gap flip applies again.
	fake.Pane[sess.TmuxSession] = bareShellPane
	mgr.runChannelStateWatcherTick(t0.Add(31*time.Second), 15*time.Second)
	got, _ = mgr.store.Get(sess.FullID)
	if got.State != StateWaitingInput {
		t.Errorf("quiet session at +31s: %s (want WaitingInput)", got.State)
	}
}

func TestBusyProbe_WatcherRecoversWaitingOneShot(t *testing.T) {
	mgr, fake := newBusyProbeManager(t)
	t0 := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

	delivered := busyProbeSession(t, mgr, "recover-delivered", StateWaitingInput, "claude-code")
	delivered.TaskDeliveredAt = &t0
	_ = mgr.store.Save(delivered)
	fake.Pane[delivered.TmuxSession] = busyPaneFrame

	undelivered := busyProbeSession(t, mgr, "recover-undelivered", StateWaitingInput, "claude-code")
	undelivered.TaskDeliveredAt = nil
	_ = mgr.store.Save(undelivered)
	fake.Pane[undelivered.TmuxSession] = busyPaneFrame

	interactive := busyProbeSession(t, mgr, "recover-interactive", StateWaitingInput, "claude-code")
	interactive.OneShot = false
	interactive.TaskDeliveredAt = &t0
	_ = mgr.store.Save(interactive)
	fake.Pane[interactive.TmuxSession] = busyPaneFrame

	mgr.runChannelStateWatcherTick(t0, 15*time.Second)

	got, _ := mgr.store.Get(delivered.FullID)
	if got.State != StateRunning {
		t.Errorf("delivered one-shot with busy pane: %s (want Running)", got.State)
	}
	got, _ = mgr.store.Get(undelivered.FullID)
	if got.State != StateWaitingInput {
		t.Errorf("undelivered one-shot with busy pane: %s (want WaitingInput)", got.State)
	}
	got, _ = mgr.store.Get(interactive.FullID)
	if got.State != StateWaitingInput {
		t.Errorf("interactive session with busy pane: %s (want WaitingInput)", got.State)
	}
}

func TestBusyProbe_WatcherLeavesOtherBackendsAlone(t *testing.T) {
	mgr, fake := newBusyProbeManager(t)
	sess := busyProbeSession(t, mgr, "other-backend", StateRunning, "")
	t0 := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	sess.LastChannelEventAt = t0
	_ = mgr.store.Save(sess)
	fake.Pane[sess.TmuxSession] = busyPaneFrame

	// The probe applies only to claude-code, so the gap still flips this one.
	mgr.runChannelStateWatcherTick(t0.Add(16*time.Second), 15*time.Second)
	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateWaitingInput {
		t.Errorf("non claude-code session with busy pane: %s (want WaitingInput)", got.State)
	}
}

// TestBusyProbe_NonClaudeFamiliesUseLiveness pins the liveness-rule dispatch
// of applyBusyProbe for recorded non-claude-code families: live and
// writing flips a waiting one-shot back to Running, at a real prompt it is
// not busy, dead is not busy, and an unrecorded family is outside the rule
// entirely.
func TestBusyProbe_NonClaudeFamiliesUseLiveness(t *testing.T) {
	ts := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

	// Live and writing (fresh activity): the waiting one-shot returns
	// to Running and the event time refreshes.
	mgr, fake := newBusyProbeManager(t)
	sess := busyProbeSession(t, mgr, "pi-live", StateWaitingInput, "pi-prompt")
	sess.LastChannelEventAt = ts.Add(-2 * time.Second)
	_ = mgr.store.Save(sess)
	fake.Pane[sess.TmuxSession] = busyPaneFrame
	mgr.paneChildFn = func(string) bool { return true }

	if !mgr.applyBusyProbe(sess.FullID, ts) {
		t.Fatalf("live+writing pi-prompt: applyBusyProbe returned false")
	}
	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateRunning {
		t.Errorf("live+writing pi-prompt: %s (want Running)", got.State)
	}
	if !got.LastChannelEventAt.Equal(ts) {
		t.Errorf("live+writing pi-prompt: LastChannelEventAt = %v, want %v", got.LastChannelEventAt, ts)
	}

	// A live interactive session is busy the same way.
	mgr2, fake2 := newBusyProbeManager(t)
	sess2 := busyProbeSession(t, mgr2, "pi-live-i", StateWaitingInput, "pi")
	sess2.LastChannelEventAt = ts.Add(-2 * time.Second)
	_ = mgr2.store.Save(sess2)
	fake2.Pane[sess2.TmuxSession] = busyPaneFrame
	mgr2.paneChildFn = func(string) bool { return true }

	if !mgr2.applyBusyProbe(sess2.FullID, ts) {
		t.Fatalf("live+writing pi: applyBusyProbe returned false")
	}
	got2, _ := mgr2.store.Get(sess2.FullID)
	if got2.State != StateRunning {
		t.Errorf("live+writing pi: %s (want Running)", got2.State)
	}

	// At a real prompt the verdict is WaitingInput, which is not busy:
	// the probe leaves both a waiting and a running session alone.
	mgr3, fake3 := newBusyProbeManager(t)
	atw := busyProbeSession(t, mgr3, "pi-at-prompt", StateWaitingInput, "pi-prompt")
	fake3.Pane[atw.TmuxSession] = idleClaudePane
	mgr3.paneChildFn = func(string) bool { return true }
	if mgr3.applyBusyProbe(atw.FullID, ts) {
		t.Fatalf("at-prompt: applyBusyProbe returned true")
	}
	got3, _ := mgr3.store.Get(atw.FullID)
	if got3.State != StateWaitingInput {
		t.Errorf("at-prompt: %s (want WaitingInput)", got3.State)
	}

	atr := busyProbeSession(t, mgr3, "pi-at-prompt-running", StateRunning, "pi")
	fake3.Pane[atr.TmuxSession] = idleClaudePane
	if mgr3.applyBusyProbe(atr.FullID, ts) {
		t.Errorf("interactive at a real prompt: applyBusyProbe returned true")
	}
	got4, _ := mgr3.store.Get(atr.FullID)
	if got4.State != StateRunning {
		t.Errorf("interactive at a real prompt: %s (want Running, left alone)", got4.State)
	}

	// Dead process: not busy, whatever the pane shows and whatever the
	// state is.
	mgr4, fake4 := newBusyProbeManager(t)
	dead := busyProbeSession(t, mgr4, "pi-dead", StateWaitingInput, "pi-prompt")
	dead.LastChannelEventAt = ts.Add(-2 * time.Second)
	_ = mgr4.store.Save(dead)
	fake4.Pane[dead.TmuxSession] = busyPaneFrame
	// newBusyProbeManager's child check already reports no child process.
	if mgr4.applyBusyProbe(dead.FullID, ts) {
		t.Fatalf("dead pi-prompt: applyBusyProbe returned true")
	}
	got5, _ := mgr4.store.Get(dead.FullID)
	if got5.State != StateWaitingInput {
		t.Errorf("dead pi-prompt: %s (want WaitingInput, unchanged)", got5.State)
	}

	// An unrecorded family is outside the rule entirely: no probe at all.
	mgr5, _ := newBusyProbeManager(t)
	unrec := busyProbeSession(t, mgr5, "unrec", StateWaitingInput, "")
	if mgr5.applyBusyProbe(unrec.FullID, ts) {
		t.Fatalf("unrecorded family: applyBusyProbe returned true")
	}
	got6, _ := mgr5.store.Get(unrec.FullID)
	if got6.State != StateWaitingInput {
		t.Errorf("unrecorded family: %s (want WaitingInput, unchanged)", got6.State)
	}
}
