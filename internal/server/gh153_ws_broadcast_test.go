// GH#153 — WebSocket hub broadcasts on guardrail approve.
//
// TC-1: handleSessionGuardrailApprove with non-nil hub → BroadcastHookUpdate fires
//        (verified by receiving MsgHookUpdate on a registered fake client)
// TC-2: handleSessionGuardrailApprove with nil hub → no panic (already tested
//        by existing GH153 tests; included here for completeness)

package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeWSClient is a minimal in-process client that records messages sent to it.
type fakeWSClient struct {
	mu       sync.Mutex
	received [][]byte
	ch       chan []byte
}

func newFakeWSClient() *fakeWSClient {
	return &fakeWSClient{ch: make(chan []byte, 16)}
}

// TC-1: hub.BroadcastHookUpdate fires after approve when hub is wired up.
func TestGH153_WS_BroadcastFiresOnApprove(t *testing.T) {
	sid := "test-gh153-ws-broadcast"
	setupGuardrailStore(sid, []HookGuardrailVerdict{
		{Guardrail: "content-safety", Outcome: "block"},
	})

	hub := NewHub()

	// Register a fake client directly on the hub's register channel so
	// it receives any broadcast messages. We use an in-process channel.
	fakeCh := make(chan []byte, 16)
	fakeC := &client{
		hub:        hub,
		send:       fakeCh,
		subscribed: make(map[string]bool),
	}
	// Start hub.Run() to dispatch broadcast messages to clients.
	go hub.Run()
	hub.register <- fakeC

	srv := &Server{hub: hub}
	req := httptest.NewRequest(http.MethodPost,
		"/api/sessions/"+sid+"/guardrail/content-safety/approve",
		strings.NewReader(`{"note":"ws-broadcast test"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.handleSessionGuardrailApprove(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// BroadcastHookUpdate is fired in a goroutine; give it a moment to land.
	select {
	case msg := <-fakeCh:
		// Verify it's a hook_update message
		if !strings.Contains(string(msg), `"hook_update"`) {
			t.Errorf("expected hook_update message, got: %s", string(msg))
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timeout: BroadcastHookUpdate did not fire within 500ms")
	}
}

// TC-2: hub==nil → no panic, normal 200 response.
func TestGH153_WS_NilHub_NoPanic(t *testing.T) {
	sid := "test-gh153-ws-nil"
	setupGuardrailStore(sid, []HookGuardrailVerdict{
		{Guardrail: "content-safety", Outcome: "block"},
	})
	srv := &Server{} // hub is nil
	req := httptest.NewRequest(http.MethodPost,
		"/api/sessions/"+sid+"/guardrail/content-safety/approve", nil)
	rr := httptest.NewRecorder()
	srv.handleSessionGuardrailApprove(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
}
