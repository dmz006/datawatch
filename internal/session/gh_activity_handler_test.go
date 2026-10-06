// Operator-reported 2026-10-06 — "any sort of activity should make the
// session know... is there anything doing that sort of stuff?" Investigation
// found SendInput only notified connected WS clients (via onStateChange ->
// NotifyStateChange -> BroadcastSessions/BroadcastSessionState) when the
// session transitioned OUT of WaitingInput/RateLimited. A follow-up message
// sent to an already-StateRunning session updated the live tmux pane (any
// client actively subscribed to it sees it via the unrelated screen-capture
// poll) but never touched the sessions LIST for every other connected
// client. onActivity is the fix: a separate hook, deliberately not a call
// to onStateChange with old==new, since that handler's own body (wired in
// cmd/datawatch/main.go) has state-transition-specific side effects
// (oscillation detection, "X -> Y" alert/log strings) that assume a real
// transition happened.

package session

import (
	"testing"
	"time"
)

func TestSendInput_AlreadyRunning_FiresOnActivityNotOnStateChange(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)

	var activityCalls []string
	var stateChangeCalls int
	mgr.SetActivityHandler(func(sess *Session) { activityCalls = append(activityCalls, sess.FullID) })
	mgr.SetStateChangeHandler(func(*Session, State) { stateChangeCalls++ })

	_ = mgr.SaveSession(&Session{
		ID: "rr01", FullID: "testhost-rr01", TmuxSession: "cs-rr01",
		State: StateRunning, UpdatedAt: time.Now(),
	})

	if err := mgr.SendInput("testhost-rr01", "a follow-up while still running", "web"); err != nil {
		t.Fatalf("SendInput: %v", err)
	}

	if len(activityCalls) != 1 || activityCalls[0] != "testhost-rr01" {
		t.Fatalf("expected exactly one onActivity call for testhost-rr01, got %v", activityCalls)
	}
	if stateChangeCalls != 0 {
		t.Fatalf("onStateChange must NOT fire when there is no real transition (old==new==running), got %d calls", stateChangeCalls)
	}

	sess, ok := mgr.store.Get("testhost-rr01")
	if !ok {
		t.Fatal("session vanished")
	}
	if sess.State != StateRunning {
		t.Fatalf("state must stay Running (no transition), got %q", sess.State)
	}
	if sess.LastInput != "a follow-up while still running" {
		t.Fatalf("LastInput not recorded: got %q", sess.LastInput)
	}
}

func TestSendInput_WaitingToRunning_FiresOnStateChangeNotOnActivity(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)

	var activityCalls int
	var stateChangeCalls []State
	mgr.SetActivityHandler(func(*Session) { activityCalls++ })
	mgr.SetStateChangeHandler(func(sess *Session, old State) { stateChangeCalls = append(stateChangeCalls, old) })

	_ = mgr.SaveSession(&Session{
		ID: "ww01", FullID: "testhost-ww01", TmuxSession: "cs-ww01",
		State: StateWaitingInput, UpdatedAt: time.Now(),
	})

	if err := mgr.SendInput("testhost-ww01", "answering the prompt", "web"); err != nil {
		t.Fatalf("SendInput: %v", err)
	}

	if len(stateChangeCalls) != 1 || stateChangeCalls[0] != StateWaitingInput {
		t.Fatalf("expected exactly one onStateChange call with old=waiting_input, got %v", stateChangeCalls)
	}
	if activityCalls != 0 {
		t.Fatalf("onActivity must NOT fire for a real transition (that's onStateChange's job), got %d calls", activityCalls)
	}

	sess, ok := mgr.store.Get("testhost-ww01")
	if !ok {
		t.Fatal("session vanished")
	}
	if sess.State != StateRunning {
		t.Fatalf("expected transition to Running, got %q", sess.State)
	}
}

func TestSendInput_AlreadyRunning_NoActivityHandlerSet_StillSucceeds(t *testing.T) {
	// A nil onActivity (daemon not wired, or an embedding caller that never
	// sets one) must not panic or break the send itself.
	mgr, _ := newTestManagerWithFake(t)
	_ = mgr.SaveSession(&Session{
		ID: "rr02", FullID: "testhost-rr02", TmuxSession: "cs-rr02",
		State: StateRunning, UpdatedAt: time.Now(),
	})
	if err := mgr.SendInput("testhost-rr02", "x", "web"); err != nil {
		t.Fatalf("SendInput with no activity handler set: %v", err)
	}
}
