// Test suite for the liveness rule wired into the channel state watcher
// (runChannelStateWatcherTick, state_engine.go) and the busy-probe
// recovery pass.
//
// The classifier under test is ClassifyLiveness (backend_liveness.go), a
// pure decision function driven by caller-supplied facts. This file pins
// its integration for every backend family:
//
//   - alive and writing (fresh activity within the liveness window) keeps
//     a Running session Running and refreshes LastChannelEventAt — for
//     print-mode (pi-prompt) and interactive (pi) backends alike;
//   - a dead process while the record is open is a stalled session, never
//     a waiting one — from the Running gap pass and from the
//     WaitingInput one-shot recovery pass;
//   - a print-mode backend alive but silent beyond the window stalls
//     (default LivenessWindowSeconds off by default in config, the 30 s
//     in-code default without a config);
//   - an interactive backend silent beyond the window with StallOthers
//     off keeps the legacy gap-watcher flip to WaitingInput;
//   - a session at a real prompt is WaitingInput, not busy.
//
// The fake tmux (NewFakeTmux) supplies the pane; the manager's
// paneChildFn (default paneHasChildProcess) is overridden per test to
// answer the process-alive check. No real tmux, no daemon, no version
// bump.
package session

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

var livenessT0 = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

// livenessTickManager returns a test manager with a fake tmux and a
// child-process check that reports dead unless a test overrides it.
// withCfg installs the default config so the per-backend input-idle
// timeout accessors (LivenessInputIdleTimeoutEffective) are live.
func livenessTickManager(t *testing.T, withCfg bool) (*Manager, *FakeTmux) {
	t.Helper()
	mgr, fake := newBusyProbeManager(t)
	if withCfg {
		mgr.cfg = config.DefaultConfig()
	}
	return mgr, fake
}

// livenessRunningSession saves a Running one-shot session with the given
// last-channel-event time and returns it.
func livenessRunningSession(t *testing.T, mgr *Manager, suffix, family string, lce time.Time) *Session {
	t.Helper()
	sess := busyProbeSession(t, mgr, suffix, StateRunning, family)
	sess.LastChannelEventAt = lce
	_ = mgr.store.Save(sess)
	return sess
}

// TestStateEngineLiveness_AliveWritingKeepsRunning pins rule (a)/(e):
// a live backend with fresh activity within the liveness window stays
// Running and refreshes LastChannelEventAt, for a print-mode and an
// interactive family.
func TestStateEngineLiveness_AliveWritingKeepsRunning(t *testing.T) {
	mgr, fake := livenessTickManager(t, false)
	sess := livenessRunningSession(t, mgr, "pi-prompt", "pi-prompt", livenessT0)
	fake.Pane[sess.TmuxSession] = busyPaneFrame
	mgr.paneChildFn = func(string) bool { return true }

	now := livenessT0.Add(16 * time.Second)
	mgr.runChannelStateWatcherTick(now, 15*time.Second)

	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateRunning {
		t.Fatalf("pi-prompt alive+writing: %s (want Running)", got.State)
	}
	if !got.LastChannelEventAt.Equal(now) {
		t.Errorf("pi-prompt alive+writing: LastChannelEventAt = %v, want %v (refreshed)", got.LastChannelEventAt, now)
	}

	mgr2, fake2 := livenessTickManager(t, false)
	sess2 := livenessRunningSession(t, mgr2, "pi", "pi", livenessT0)
	fake2.Pane[sess2.TmuxSession] = busyPaneFrame
	mgr2.paneChildFn = func(string) bool { return true }

	mgr2.runChannelStateWatcherTick(now, 15*time.Second)

	got2, _ := mgr2.store.Get(sess2.FullID)
	if got2.State != StateRunning {
		t.Fatalf("pi alive+writing: %s (want Running)", got2.State)
	}
	if !got2.LastChannelEventAt.Equal(now) {
		t.Errorf("pi alive+writing: LastChannelEventAt = %v, want %v (refreshed)", got2.LastChannelEventAt, now)
	}
}

// TestStateEngineLiveness_DeadProcessStalls pins rule (c) for the gap
// and recovery passes: a session whose process is gone while the record
// is still open is Stalled, never WaitingInput — print-mode and
// interactive alike.
func TestStateEngineLiveness_DeadProcessStalls(t *testing.T) {
	mgr, _ := livenessTickManager(t, false)
	sess := livenessRunningSession(t, mgr, "pi-prompt", "pi-prompt", livenessT0)

	mgr.runChannelStateWatcherTick(livenessT0.Add(16*time.Second), 15*time.Second)

	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateStalled {
		t.Errorf("dead pi-prompt from Running: %s (want Stalled, never WaitingInput)", got.State)
	}
	if got.State == StateWaitingInput {
		t.Errorf("dead session must never be WaitingInput")
	}

	mgr2, _ := livenessTickManager(t, false)
	sess2 := livenessRunningSession(t, mgr2, "pi", "pi", livenessT0)

	mgr2.runChannelStateWatcherTick(livenessT0.Add(16*time.Second), 15*time.Second)

	got2, _ := mgr2.store.Get(sess2.FullID)
	if got2.State != StateStalled {
		t.Errorf("dead pi from Running: %s (want Stalled, never WaitingInput)", got2.State)
	}
}

// TestStateEngineLiveness_PrintSilentBeyondWindowStalls pins rule (b): a
// print-mode backend whose process is alive but which has been silent
// beyond the liveness window is stalled — not misread as waiting for
// input (its output never reaches a pane prompt anyway).
func TestStateEngineLiveness_PrintSilentBeyondWindowStalls(t *testing.T) {
	mgr, _ := livenessTickManager(t, false)
	sess := livenessRunningSession(t, mgr, "pi-prompt", "pi-prompt", livenessT0)
	mgr.paneChildFn = func(string) bool { return true }

	// 40 s is beyond the in-code DefaultLivenessWindow (30 s).
	mgr.runChannelStateWatcherTick(livenessT0.Add(40*time.Second), 15*time.Second)

	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateStalled {
		t.Errorf("pi-prompt alive but silent beyond the window: %s (want Stalled, not WaitingInput)", got.State)
	}
}

// TestStateEngineLiveness_InteractiveSilentBeyondWindowFlips pins rule
// (f) with StallOthers off: an interactive silent backend keeps the
// legacy gap-watcher behaviour — it flips to WaitingInput.
func TestStateEngineLiveness_InteractiveSilentBeyondWindowFlips(t *testing.T) {
	mgr, _ := livenessTickManager(t, false)
	sess := livenessRunningSession(t, mgr, "pi", "pi", livenessT0)
	mgr.paneChildFn = func(string) bool { return true }

	// 40 s is beyond the in-code DefaultLivenessWindow (30 s).
	mgr.runChannelStateWatcherTick(livenessT0.Add(40*time.Second), 15*time.Second)

	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateWaitingInput {
		t.Errorf("interactive silent beyond the window (StallOthers off): %s (want WaitingInput, legacy flip)", got.State)
	}
}

// TestStateEngineLiveness_AtRealPromptIsWaiting pins rule (d): a live
// interactive session whose pane shows a real input prompt is waiting,
// not busy — the Running gap pass flips it to WaitingInput and the
// recovery pass treats it as where it already is (never busy).
func TestStateEngineLiveness_AtRealPromptIsWaiting(t *testing.T) {
	mgr, fake := livenessTickManager(t, false)
	sess := livenessRunningSession(t, mgr, "pi", "pi", livenessT0)
	fake.Pane[sess.TmuxSession] = idleClaudePane
	mgr.paneChildFn = func(string) bool { return true }

	mgr.runChannelStateWatcherTick(livenessT0.Add(16*time.Second), 15*time.Second)

	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateWaitingInput {
		t.Errorf("interactive at a real prompt: %s (want WaitingInput)", got.State)
	}

	// The same session at a prompt is not recovered by the busy probe:
	// a delivered one-shot that is at its prompt stays waiting.
	mgr2, fake2 := livenessTickManager(t, false)
	sess2 := busyProbeSession(t, mgr2, "pi", StateWaitingInput, "pi")
	delivered := livenessT0
	sess2.TaskDeliveredAt = &delivered
	fake2.Pane[sess2.TmuxSession] = idleClaudePane
	mgr2.paneChildFn = func(string) bool { return true }

	mgr2.runChannelStateWatcherTick(livenessT0.Add(16*time.Second), 15*time.Second)

	got2, _ := mgr2.store.Get(sess2.FullID)
	if got2.State != StateWaitingInput {
		t.Errorf("delivered one-shot at a real prompt: %s (want WaitingInput, probe must not mark it busy)", got2.State)
	}
}

// TestStateEngineLiveness_RecoveryStallsDeadOneShot pins the recovery
// pass: a delivered one-shot that reached WaitingInput because its
// process died is Stalled, for a print-mode and an interactive family.
func TestStateEngineLiveness_RecoveryStallsDeadOneShot(t *testing.T) {
	mgr, _ := livenessTickManager(t, false)
	sess := busyProbeSession(t, mgr, "pi-prompt", StateWaitingInput, "pi-prompt")
	livenessT0p := livenessT0
	sess.TaskDeliveredAt = &livenessT0p
	_ = mgr.store.Save(sess)

	mgr.runChannelStateWatcherTick(livenessT0.Add(16*time.Second), 15*time.Second)

	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateStalled {
		t.Errorf("dead delivered pi-prompt one-shot: %s (want Stalled, never WaitingInput)", got.State)
	}

	mgr2, _ := livenessTickManager(t, false)
	sess2 := busyProbeSession(t, mgr2, "pi", StateWaitingInput, "pi")
	sess2.TaskDeliveredAt = &livenessT0p
	_ = mgr2.store.Save(sess2)

	mgr2.runChannelStateWatcherTick(livenessT0.Add(16*time.Second), 15*time.Second)

	got2, _ := mgr2.store.Get(sess2.FullID)
	if got2.State != StateStalled {
		t.Errorf("dead delivered pi one-shot: %s (want Stalled, never WaitingInput)", got2.State)
	}
}

// TestStateEngineLiveness_PrintModeIdleTimeoutDefaultOff pins the
// config wiring: with the default config (global input-idle timeout 10 s,
// default liveness window 120 s) a print-mode backend's timeout is off,
// so 12 s of silence does not flip it — its liveness window still governs
// — and beyond that window it is Stalled, not WaitingInput. An
// interactive family keeps the global 10 s timeout, so it does reach
// WaitingInput once silent beyond the liveness window.
func TestStateEngineLiveness_PrintModeIdleTimeoutDefaultOff(t *testing.T) {
	mgr, _ := livenessTickManager(t, true)
	sess := livenessRunningSession(t, mgr, "pi-prompt", "pi-prompt", livenessT0)
	mgr.paneChildFn = func(string) bool { return true }

	// 12 s is beyond the global InputIdleTimeout (10 s) but within the
	// liveness window (120 s): the print-mode timeout is off by default,
	// and the liveness rule reads the live session as running.
	mgr.runChannelStateWatcherTick(livenessT0.Add(12*time.Second), 15*time.Second)

	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateRunning {
		t.Errorf("pi-prompt past the global idle timeout but within its liveness window: %s (want Running — print-mode idle timeout is off by default)", got.State)
	}

	// Beyond the 120 s liveness window the same live-but-silent
	// print-mode backend is stalled.
	mgr2, _ := livenessTickManager(t, true)
	sess2 := livenessRunningSession(t, mgr2, "pi-prompt", "pi-prompt", livenessT0)
	mgr2.paneChildFn = func(string) bool { return true }

	mgr2.runChannelStateWatcherTick(livenessT0.Add(130*time.Second), 15*time.Second)

	got2, _ := mgr2.store.Get(sess2.FullID)
	if got2.State != StateStalled {
		t.Errorf("pi-prompt silent beyond the 120 s liveness window: %s (want Stalled, not WaitingInput)", got2.State)
	}

	// The interactive family keeps the global input-idle timeout: 16 s is
	// past the 10 s global, and at a real prompt the session is
	// waiting.
	mgr3, fake3 := livenessTickManager(t, true)
	sess3 := livenessRunningSession(t, mgr3, "pi", "pi", livenessT0)
	fake3.Pane[sess3.TmuxSession] = idleClaudePane
	mgr3.paneChildFn = func(string) bool { return true }

	mgr3.runChannelStateWatcherTick(livenessT0.Add(16*time.Second), 15*time.Second)

	got3, _ := mgr3.store.Get(sess3.FullID)
	if got3.State != StateWaitingInput {
		t.Errorf("interactive pi at a real prompt past the global idle timeout: %s (want WaitingInput)", got3.State)
	}
}

// TestStateEngineLiveness_StalledIsNotWaitingAndStaysSticky pins the
// bookkeeping: a stalled session is not waiting for input (no alert, no
// input prompt, no state flip on a later tick) and stays Stalled while a
// busy pane alone cannot revive it — only real output or a state
// override restores it.
func TestStateEngineLiveness_StalledIsNotWaitingAndStaysSticky(t *testing.T) {
	mgr, fake := livenessTickManager(t, false)
	sess := livenessRunningSession(t, mgr, "pi-prompt", "pi-prompt", livenessT0)
	// Dead process → stalled on the first tick.
	mgr.runChannelStateWatcherTick(livenessT0.Add(16*time.Second), 15*time.Second)
	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateStalled {
		t.Fatalf("setup: %s (want Stalled)", got.State)
	}
	if got.State == StateWaitingInput {
		t.Fatalf("stalled session must not be counted as WaitingInput")
	}

	// A later tick with a live, working pane must not flip the stale
	// record to waiting or back to running on its own.
	fake.Pane[sess.TmuxSession] = busyPaneFrame
	mgr.paneChildFn = func(string) bool { return true }
	mgr.runChannelStateWatcherTick(livenessT0.Add(31*time.Second), 15*time.Second)

	got2, _ := mgr.store.Get(sess.FullID)
	if got2.State != StateStalled {
		t.Errorf("stalled session with a live busy pane on the next tick: %s (want Stalled, sticky)", got2.State)
	}
}
