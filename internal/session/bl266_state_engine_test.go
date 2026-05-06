// Tests for BL266 / v6.11.24 — channel-driven state engine.
//
// Two independent paths (Option C, operator-directed 2026-05-05):
//   - Path B: structured ACP events drive state transitions directly,
//     bypassing the natural-language classifier entirely.
//   - Path A: gap-based watcher flips Running → WaitingInput when a session
//     produces no channel events for the configured silence window.
//
// Each path is tested independently per operator request: "ship as one
// release validating both independently with tests".

package session

import (
	"testing"
	"time"
)

// --- Path B: structured ACP events ---

func TestBL266_ACP_BusyTransitionsWaitingToRunning(t *testing.T) {
	mgr := newTestManager(t)
	sess := newTestSession(t, mgr, "acp-busy-1")
	sess.State = StateWaitingInput
	if err := mgr.store.Save(sess); err != nil {
		t.Fatalf("save: %v", err)
	}

	mgr.MarkACPEvent(sess.FullID, "session.status", "busy")

	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateRunning {
		t.Errorf("state after busy: %s (want Running)", got.State)
	}
	if got.LastChannelEventAt.IsZero() {
		t.Errorf("LastChannelEventAt should be set")
	}
}

func TestBL266_ACP_IdleTransitionsRunningToWaiting(t *testing.T) {
	mgr := newTestManager(t)
	sess := newTestSession(t, mgr, "acp-idle-1")
	sess.State = StateRunning
	if err := mgr.store.Save(sess); err != nil {
		t.Fatalf("save: %v", err)
	}

	mgr.MarkACPEvent(sess.FullID, "session.status", "idle")

	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateWaitingInput {
		t.Errorf("state after idle: %s (want WaitingInput)", got.State)
	}
}

func TestBL266_ACP_SessionIdleEventTransitionsRunningToWaiting(t *testing.T) {
	mgr := newTestManager(t)
	sess := newTestSession(t, mgr, "acp-sessionidle-1")
	sess.State = StateRunning
	_ = mgr.store.Save(sess)

	mgr.MarkACPEvent(sess.FullID, "session.idle", "")

	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateWaitingInput {
		t.Errorf("state after session.idle: %s (want WaitingInput)", got.State)
	}
}

func TestBL266_ACP_SessionCompletedTransitionsToComplete(t *testing.T) {
	mgr := newTestManager(t)
	sess := newTestSession(t, mgr, "acp-done-1")
	sess.State = StateRunning
	_ = mgr.store.Save(sess)

	mgr.MarkACPEvent(sess.FullID, "session.completed", "")

	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateComplete {
		t.Errorf("state after session.completed: %s (want Complete)", got.State)
	}
}

func TestBL266_ACP_MessageCompletedTransitionsToComplete(t *testing.T) {
	mgr := newTestManager(t)
	sess := newTestSession(t, mgr, "acp-msgdone-1")
	sess.State = StateRunning
	_ = mgr.store.Save(sess)

	mgr.MarkACPEvent(sess.FullID, "message.completed", "")

	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateComplete {
		t.Errorf("state after message.completed: %s (want Complete)", got.State)
	}
}

func TestBL266_ACP_StepStartCountsAsRunningActivity(t *testing.T) {
	mgr := newTestManager(t)
	sess := newTestSession(t, mgr, "acp-step-1")
	sess.State = StateWaitingInput
	_ = mgr.store.Save(sess)

	mgr.MarkACPEvent(sess.FullID, "step-start", "")

	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateRunning {
		t.Errorf("state after step-start: %s (want Running)", got.State)
	}
}

func TestBL266_ACP_TerminalStateNeverResurrected(t *testing.T) {
	mgr := newTestManager(t)
	sess := newTestSession(t, mgr, "acp-terminal-1")
	sess.State = StateComplete
	_ = mgr.store.Save(sess)

	// Even an explicit busy event must not resurrect a Complete session.
	mgr.MarkACPEvent(sess.FullID, "session.status", "busy")
	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateComplete {
		t.Errorf("Complete should not be resurrected by busy: got %s", got.State)
	}

	// Same for Killed.
	sess.State = StateKilled
	_ = mgr.store.Save(sess)
	mgr.MarkACPEvent(sess.FullID, "session.status", "busy")
	got, _ = mgr.store.Get(sess.FullID)
	if got.State != StateKilled {
		t.Errorf("Killed should not be resurrected by busy: got %s", got.State)
	}
}

func TestBL266_ACP_ClassifierMappings(t *testing.T) {
	cases := []struct {
		evt, status string
		want        ChannelEventKind
		ok          bool
	}{
		{"session.status", "busy", EventRunning, true},
		{"session.status", "idle", EventIdle, true},
		{"session.idle", "", EventIdle, true},
		{"session.completed", "", EventComplete, true},
		{"message.completed", "", EventComplete, true},
		{"step-start", "", EventRunning, true},
		{"message.part.delta", "", EventRunning, true},
		{"message.part.updated", "", EventRunning, true},
		{"unknown.event", "", 0, false},
	}
	for _, c := range cases {
		got, ok := classifyACPEventType(c.evt, c.status)
		if ok != c.ok {
			t.Errorf("classifyACPEventType(%q,%q) ok=%v want %v", c.evt, c.status, ok, c.ok)
		}
		if ok && got != c.want {
			t.Errorf("classifyACPEventType(%q,%q) = %v want %v", c.evt, c.status, got, c.want)
		}
	}
}

// --- Path A: gap-based watcher ---

func TestBL266_Watcher_FlipsRunningToWaitingAfterGap(t *testing.T) {
	mgr := newTestManager(t)
	sess := newTestSession(t, mgr, "watcher-gap-1")
	sess.State = StateRunning
	t0 := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	sess.LastChannelEventAt = t0
	sess.UpdatedAt = t0
	_ = mgr.store.Save(sess)

	// Tick at t+10s with 15s gap → still Running.
	mgr.runChannelStateWatcherTick(t0.Add(10*time.Second), 15*time.Second)
	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateRunning {
		t.Fatalf("at +10s should stay Running, got %s", got.State)
	}

	// Tick at exactly +15s → flipped (gap semantics is now-ref >= gap).
	mgr.runChannelStateWatcherTick(t0.Add(15*time.Second), 15*time.Second)
	got, _ = mgr.store.Get(sess.FullID)
	if got.State != StateWaitingInput {
		t.Fatalf("at +15s should be WaitingInput, got %s", got.State)
	}
}

func TestBL266_Watcher_LeavesNonRunningAlone(t *testing.T) {
	mgr := newTestManager(t)
	for _, st := range []State{StateWaitingInput, StateComplete, StateFailed, StateKilled, StateRateLimited} {
		sess := newTestSession(t, mgr, "watcher-noop-"+string(st))
		sess.State = st
		t0 := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
		sess.LastChannelEventAt = t0
		sess.UpdatedAt = t0
		_ = mgr.store.Save(sess)

		mgr.runChannelStateWatcherTick(t0.Add(time.Hour), 15*time.Second)
		got, _ := mgr.store.Get(sess.FullID)
		if got.State != st {
			t.Errorf("watcher should not transition out of %s, got %s", st, got.State)
		}
	}
}

func TestBL266_Watcher_SkipsZeroLastEventAt(t *testing.T) {
	mgr := newTestManager(t)
	sess := newTestSession(t, mgr, "watcher-zero-1")
	sess.State = StateRunning
	// Zero LastChannelEventAt (fresh session that hasn't produced anything yet).
	_ = mgr.store.Save(sess)

	mgr.runChannelStateWatcherTick(time.Now(), 15*time.Second)
	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateRunning {
		t.Errorf("session with zero LastChannelEventAt should not be flipped: got %s", got.State)
	}
}

func TestBL266_Watcher_FreshUpdatedAtKeepsRunning(t *testing.T) {
	// Operator-driven UpdatedAt (e.g. SendInput just bumped it) should
	// be respected as activity even if LastChannelEventAt is older.
	mgr := newTestManager(t)
	sess := newTestSession(t, mgr, "watcher-updated-1")
	sess.State = StateRunning
	t0 := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	sess.LastChannelEventAt = t0
	sess.UpdatedAt = t0.Add(20 * time.Second) // newer than the channel event
	_ = mgr.store.Save(sess)

	mgr.runChannelStateWatcherTick(t0.Add(30*time.Second), 15*time.Second)
	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateRunning {
		t.Errorf("fresh UpdatedAt should keep session Running: got %s", got.State)
	}
}

// --- v6.11.25 regression: NLP no longer promotes Complete from text ---

// Operator-reported 2026-05-05 (post v6.11.24): claude wraps like
// "Done. Let me know if you want anything else." were promoted to
// EventComplete via the per-sentence suffix matcher. Once Complete
// (sticky), the PWA's pane_capture skip gate dropped every frame for
// 10 s, leaving the loading splash stuck. The fix removes NLP→Complete
// promotion entirely; only structural signals (ACP session.completed,
// MCP DATAWATCH_COMPLETE, operator kill) drive Complete.
func TestBL266Followup_NLP_DoesNotPromoteCompleteFromText(t *testing.T) {
	mgr := newTestManager(t)
	sess := newTestSession(t, mgr, "nlp-nocomplete-1")
	sess.State = StateRunning
	_ = mgr.store.Save(sess)

	// Multi-sentence wrap with completion phrase as an early-sentence suffix.
	mgr.MarkChannelActivityFromText(sess.FullID, "Done. Let me know if you want anything else.")

	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateRunning {
		t.Errorf("NLP completion text must NOT flip state to Complete, got %s", got.State)
	}
	// Single-sentence whole-message completion also stays advisory only —
	// structural ACP session.completed is the authoritative path.
	mgr.MarkChannelActivityFromText(sess.FullID, "Task complete.")
	got, _ = mgr.store.Get(sess.FullID)
	if got.State != StateRunning {
		t.Errorf("NLP 'Task complete.' must NOT flip state, got %s", got.State)
	}
}

func TestBL266Followup_NLP_StillPromotesInputNeeded(t *testing.T) {
	// Keep the conservative NLP→input promotion. Question marks and
	// "should I proceed?" are clear asks.
	mgr := newTestManager(t)
	sess := newTestSession(t, mgr, "nlp-input-1")
	sess.State = StateRunning
	_ = mgr.store.Save(sess)

	mgr.MarkChannelActivityFromText(sess.FullID, "Should I proceed with the migration?")

	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateWaitingInput {
		t.Errorf("NLP input-needed should flip Running → WaitingInput, got %s", got.State)
	}
}

// --- Helpers ---

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(dir + "/sessions.json")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return &Manager{store: store, hostname: "testhost"}
}

func newTestSession(t *testing.T, mgr *Manager, suffix string) *Session {
	t.Helper()
	now := time.Now()
	s := &Session{
		ID:        suffix,
		FullID:    "testhost-" + suffix,
		State:     StateRunning,
		CreatedAt: now,
		UpdatedAt: now,
		Hostname:  "testhost",
	}
	if err := mgr.store.Save(s); err != nil {
		t.Fatalf("save: %v", err)
	}
	return s
}
