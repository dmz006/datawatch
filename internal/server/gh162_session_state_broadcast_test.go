// GH#162 — session_state WS broadcast.
//
// MsgSessionState ("session_state") was defined in the protocol enum from
// the start but never actually constructed or broadcast anywhere — every
// session-list update went out as the full list via BroadcastSessions,
// even when only one session changed. This adds a lighter-weight
// single-session broadcast alongside the existing full-list one.

package server

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// TestGH162_BroadcastSessionState verifies Hub.BroadcastSessionState emits
// a session_state-typed message carrying the single session's data.
func TestGH162_BroadcastSessionState(t *testing.T) {
	hub := NewHub()
	fakeCh := make(chan []byte, 16)
	fakeC := &client{hub: hub, send: fakeCh, subscribed: make(map[string]bool)}
	go hub.Run()
	hub.register <- fakeC

	sess := &session.Session{ID: "abcd", FullID: "h-abcd", State: session.StateRunning}
	hub.BroadcastSessionState(sess)

	select {
	case msg := <-fakeCh:
		s := string(msg)
		if !strings.Contains(s, `"session_state"`) {
			t.Errorf("expected session_state message, got: %s", s)
		}
		if !strings.Contains(s, `"h-abcd"`) {
			t.Errorf("expected the session's full_id in the payload, got: %s", s)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timeout: BroadcastSessionState did not fire within 500ms")
	}
}

// TestGH162_NotifyStateChange_EmitsBothMessages verifies NotifyStateChange
// emits the new session_state message ADDITIVELY alongside the existing
// full-list sessions broadcast, not as a replacement — any client that
// only understands "sessions" must keep working unchanged.
func TestGH162_NotifyStateChange_EmitsBothMessages(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("h", dir, "echo", 30*time.Second)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	hub := NewHub()
	fakeCh := make(chan []byte, 16)
	fakeC := &client{hub: hub, send: fakeCh, subscribed: make(map[string]bool)}
	go hub.Run()
	hub.register <- fakeC

	srv := &HTTPServer{hub: hub, manager: mgr}
	sess := &session.Session{ID: "abcd", FullID: "h-abcd", State: session.StateRunning}
	srv.NotifyStateChange(sess, session.StateRunning)

	var sawSessions, sawSessionState bool
	deadline := time.After(500 * time.Millisecond)
	for !(sawSessions && sawSessionState) {
		select {
		case msg := <-fakeCh:
			s := string(msg)
			if strings.Contains(s, `"type":"sessions"`) {
				sawSessions = true
			}
			if strings.Contains(s, `"type":"session_state"`) {
				sawSessionState = true
			}
		case <-deadline:
			t.Fatalf("timeout waiting for both broadcasts: sessions=%v session_state=%v", sawSessions, sawSessionState)
		}
	}
}
