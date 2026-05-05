// BL264 / v6.11.18 — verify channel activity transitions a session
// out of WaitingInput back to Running. Operator-directed: "Use the
// channel if available, like acp for opencode, it's a clear channel
// it's available for state detection, use it."

package session

import (
	"testing"
	"time"
)

func TestBL264_ChannelActivityWakesWaitingInput(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	_ = mgr.SaveSession(&Session{
		ID: "aa01", FullID: "testhost-aa01", TmuxSession: "cs-aa01",
		Hostname: "testhost", State: StateWaitingInput, UpdatedAt: time.Now(),
	})

	mgr.MarkChannelActivity("testhost-aa01")

	got, ok := mgr.store.Get("testhost-aa01")
	if !ok {
		t.Fatal("session lost")
	}
	if got.State != StateRunning {
		t.Errorf("state: %s (want %s)", got.State, StateRunning)
	}
}

func TestBL264_ChannelActivityRespectsRunning(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	_ = mgr.SaveSession(&Session{
		ID: "aa01", FullID: "testhost-aa01", TmuxSession: "cs-aa01",
		Hostname: "testhost", State: StateRunning, UpdatedAt: time.Now(),
	})

	mgr.MarkChannelActivity("testhost-aa01")
	got, _ := mgr.store.Get("testhost-aa01")
	if got.State != StateRunning {
		t.Errorf("state: %s (want unchanged Running)", got.State)
	}
}

func TestBL264_ChannelActivityRespectsTerminalStates(t *testing.T) {
	for _, st := range []State{StateComplete, StateFailed, StateKilled, StateRateLimited} {
		t.Run(string(st), func(t *testing.T) {
			mgr, _ := newTestManagerWithFake(t)
			_ = mgr.SaveSession(&Session{
				ID: "aa01", FullID: "testhost-aa01", TmuxSession: "cs-aa01",
				Hostname: "testhost", State: st, UpdatedAt: time.Now(),
			})
			mgr.MarkChannelActivity("testhost-aa01")
			got, _ := mgr.store.Get("testhost-aa01")
			if got.State != st {
				t.Errorf("state: %s (want unchanged %s)", got.State, st)
			}
		})
	}
}

func TestBL264_EmitChatMessage_BumpsState(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	_ = mgr.SaveSession(&Session{
		ID: "aa01", FullID: "testhost-aa01", TmuxSession: "cs-aa01",
		Hostname: "testhost", State: StateWaitingInput, UpdatedAt: time.Now(),
	})

	// assistant streaming chunk should bump state
	mgr.EmitChatMessage("testhost-aa01", "assistant", "thinking…", true)
	got, _ := mgr.store.Get("testhost-aa01")
	if got.State != StateRunning {
		t.Errorf("state after assistant chat_message: %s (want Running)", got.State)
	}
}

func TestBL264_EmitChatMessage_SystemDoesNotBump(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	_ = mgr.SaveSession(&Session{
		ID: "aa01", FullID: "testhost-aa01", TmuxSession: "cs-aa01",
		Hostname: "testhost", State: StateWaitingInput, UpdatedAt: time.Now(),
	})

	// system role is transient indicator; should not bump state
	mgr.EmitChatMessage("testhost-aa01", "system", "ready", false)
	got, _ := mgr.store.Get("testhost-aa01")
	if got.State != StateWaitingInput {
		t.Errorf("state after system chat_message: %s (want unchanged WaitingInput)", got.State)
	}
}
