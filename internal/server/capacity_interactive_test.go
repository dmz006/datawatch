// v8.36.0 — interactive session starts admit on the same node:/llm: capacity
// pools an autonomous task would use for the same backend+model, so a manual
// session against a shared compute node can't silently oversubscribe it.
// Autonomous task spawns (OneShot=true) must skip this entirely — they
// already admit via the executor's own m.admit() before ever reaching
// handleStartSession, and gating them again here would double-acquire the
// same pool.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandleStartSession_CapacityAdmit_Gated verifies a genuine interactive
// start calls capacityAdmit, and on success binds the real session ID so the
// ledger's reaper can find and release it once the session ends.
func TestHandleStartSession_CapacityAdmit_Gated(t *testing.T) {
	srv := newLLMServer(t)

	var admitCalls, bindCalls int
	var boundHolder, boundSessionID string
	srv.SetCapacityAdmit(func(_ context.Context, backend, model, holder string) error {
		admitCalls++
		return nil
	})
	srv.SetCapacityBind(func(holder, sessionID string) {
		bindCalls++
		boundHolder, boundSessionID = holder, sessionID
	})

	body, _ := json.Marshal(map[string]any{"task": "hello", "project_dir": t.TempDir()})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/start", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleStartSession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleStartSession: status %d body: %s", w.Code, w.Body.String())
	}
	cleanupStartedSession(t, srv, w)
	if admitCalls != 1 {
		t.Fatalf("capacityAdmit calls = %d, want 1", admitCalls)
	}
	if bindCalls != 1 {
		t.Fatalf("capacityBind calls = %d, want 1", bindCalls)
	}
	var resp struct {
		FullID string `json:"full_id"`
		ID     string `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	sessID := resp.FullID
	if sessID == "" {
		sessID = resp.ID
	}
	if boundSessionID == "" || boundSessionID != sessID {
		t.Fatalf("capacityBind session ID = %q, want the started session's id %q", boundSessionID, sessID)
	}
	if boundHolder == "" {
		t.Fatal("capacityBind holder was empty")
	}
}

// TestHandleStartSession_CapacityAdmit_SkippedForOneShot verifies an
// autonomous task spawn (OneShot=true) never calls capacityAdmit — it's
// already admitted upstream by the executor's own m.admit(), and gating it
// again here would double-acquire the same node:/llm: pool.
func TestHandleStartSession_CapacityAdmit_SkippedForOneShot(t *testing.T) {
	srv := newLLMServer(t)

	admitCalls := 0
	srv.SetCapacityAdmit(func(_ context.Context, _, _, _ string) error {
		admitCalls++
		return nil
	})

	body, _ := json.Marshal(map[string]any{"task": "hello", "project_dir": t.TempDir(), "one_shot": true})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/start", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleStartSession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleStartSession: status %d body: %s", w.Code, w.Body.String())
	}
	cleanupStartedSession(t, srv, w)
	if admitCalls != 0 {
		t.Fatalf("capacityAdmit calls = %d, want 0 for a OneShot (autonomous) session start", admitCalls)
	}
}

// TestHandleStartSession_CapacityAdmit_DeniedReturns503 verifies a denied
// admission (e.g. capacity.ErrWaitTimeout) surfaces as 503 and never starts
// a session.
func TestHandleStartSession_CapacityAdmit_DeniedReturns503(t *testing.T) {
	srv := newLLMServer(t)

	srv.SetCapacityAdmit(func(_ context.Context, _, _, _ string) error {
		return errCapacityDenied
	})
	releaseCalls := 0
	srv.SetCapacityRelease(func(string) { releaseCalls++ })

	body, _ := json.Marshal(map[string]any{"task": "hello", "project_dir": t.TempDir()})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/start", bytes.NewReader(body))
	w := httptest.NewRecorder()
	before := len(srv.manager.ListSessions())
	srv.handleStartSession(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("handleStartSession: status %d, want 503; body: %s", w.Code, w.Body.String())
	}
	if releaseCalls != 0 {
		t.Fatalf("capacityRelease calls = %d, want 0 (admission itself was denied, nothing to release)", releaseCalls)
	}
	if got := len(srv.manager.ListSessions()); got != before {
		t.Fatalf("session count changed from %d to %d — a session was started despite denied admission", before, got)
	}
}

var errCapacityDenied = &capacityDeniedErr{}

// cleanupStartedSession (v8.36.12) tears down a session spawned via a raw
// handleStartSession call in a test, once the test finishes. Found live:
// internal/session.Manager.Start spawns a REAL tmux session (named
// cs-<hostname>-<id>) even in these unit tests — nothing about the test
// environment mocks the tmux layer — and none of this package's
// handleStartSession-calling tests ever killed what they started. Every
// `go test ./...` run leaked one tmux session per uncleaned call site,
// accumulating (confirmed live: 197 such orphaned sessions going back to
// May, none present in the session store since Manager.Delete's kill-first
// logic was simply never reached for them). w is the httptest.ResponseRecorder
// handleStartSession wrote its JSON response to.
func cleanupStartedSession(t *testing.T, srv *Server, w *httptest.ResponseRecorder) {
	t.Helper()
	t.Cleanup(func() {
		var r struct{ FullID, ID string }
		_ = json.Unmarshal(w.Body.Bytes(), &r)
		id := r.FullID
		if id == "" {
			id = r.ID
		}
		if id != "" {
			_ = srv.manager.Delete(id, false)
		}
	})
}

type capacityDeniedErr struct{}

func (e *capacityDeniedErr) Error() string { return "pool fully utilized" }
