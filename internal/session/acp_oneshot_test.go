package session

import (
	"testing"
	"time"
)

func timeNowForTest() time.Time { return time.Now() }

func TestACP_HeartbeatAndHousekeepingEventsDoNotResurrectWaitingSession(t *testing.T) {
	mgr := newTestManager(t)
	sess := newTestSession(t, mgr, "acp-hb-1")
	sess.State = StateWaitingInput
	_ = mgr.store.Save(sess)

	for _, ev := range []string{"server.heartbeat", "server.connected", "message.updated", "session.updated", "session.diff"} {
		mgr.MarkACPEvent(sess.FullID, ev, "")
	}
	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateWaitingInput {
		t.Fatalf("unmapped events must not move a waiting session to %s", got.State)
	}
}

func TestACP_UnmappedEventKeepsRunningSessionAlive(t *testing.T) {
	mgr := newTestManager(t)
	sess := newTestSession(t, mgr, "acp-hb-2")
	mgr.MarkACPEvent(sess.FullID, "message.updated", "")
	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateRunning || got.LastChannelEventAt.IsZero() {
		t.Fatalf("running session should stay running and be touched: %+v", got.State)
	}
	mgr2 := newTestManager(t)
	s2 := newTestSession(t, mgr2, "acp-hb-3")
	mgr2.MarkACPEvent(s2.FullID, "server.heartbeat", "")
	g2, _ := mgr2.store.Get(s2.FullID)
	if !g2.LastChannelEventAt.IsZero() {
		t.Fatal("heartbeat must not count as activity")
	}
}

func TestACP_OneShotCompletesWhenWorkingTurnGoesIdle(t *testing.T) {
	mgr := newTestManager(t)
	sess := newTestSession(t, mgr, "acp-os-1")
	sess.OneShot = true
	_ = mgr.store.Save(sess)

	mgr.MarkACPEvent(sess.FullID, "session.status", "busy")
	mgr.MarkACPEvent(sess.FullID, "session.status", "idle")
	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateComplete {
		t.Fatalf("one-shot ACP session must complete after its turn, got %s", got.State)
	}
}

func TestACP_OneShotIdleBeforeAnyTurnDoesNotComplete(t *testing.T) {
	mgr := newTestManager(t)
	sess := newTestSession(t, mgr, "acp-os-2")
	sess.OneShot = true
	_ = mgr.store.Save(sess)

	mgr.MarkACPEvent(sess.FullID, "session.idle", "")
	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateWaitingInput {
		t.Fatalf("initial idle before any work must only wait, got %s", got.State)
	}
}

func TestACP_InteractiveSessionStillWaitsAfterTurn(t *testing.T) {
	mgr := newTestManager(t)
	sess := newTestSession(t, mgr, "acp-int-1")
	mgr.MarkACPEvent(sess.FullID, "session.status", "busy")
	mgr.MarkACPEvent(sess.FullID, "session.idle", "")
	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateWaitingInput {
		t.Fatalf("non one-shot session must wait for the next message, got %s", got.State)
	}
}

func TestACP_WatcherUsesLongSilenceWindow(t *testing.T) {
	mgr := newTestManager(t)
	sess := newTestSession(t, mgr, "acp-gap-1")
	sess.BackendFamily = "opencode-acp"
	t0 := timeNowForTest()
	sess.LastChannelEventAt = t0
	_ = mgr.store.Save(sess)

	mgr.runChannelStateWatcherTick(t0.Add(2*time.Minute), 15*time.Second)
	got, _ := mgr.store.Get(sess.FullID)
	if got.State != StateRunning {
		t.Fatalf("ACP session silent for 2 min mid-turn must stay running, got %s", got.State)
	}
	mgr.runChannelStateWatcherTick(t0.Add(6*time.Minute), 15*time.Second)
	got, _ = mgr.store.Get(sess.FullID)
	if got.State != StateWaitingInput {
		t.Fatalf("dropped-stream fallback must still fire after 5 min, got %s", got.State)
	}
}
