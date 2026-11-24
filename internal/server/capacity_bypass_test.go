package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/capacity"
)

// TestHandleStartSession_OneShotFlagDoesNotSkipAdmission proves a client cannot
// skip pool admission by sending one_shot true: with no daemon marker on the
// request the start still asks the ledger for a lease.
func TestHandleStartSession_OneShotFlagDoesNotSkipAdmission(t *testing.T) {
	srv := newLLMServer(t)
	srv.manager.WithFakeTmux()

	admitCalls := 0
	srv.SetCapacityAdmit(func(_ context.Context, _, _, _ string, _ int) (string, error) {
		admitCalls++
		return "", nil
	})

	body, _ := json.Marshal(map[string]any{"task": "hello", "project_dir": t.TempDir(), "one_shot": true})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/start", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleStartSession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleStartSession: status %d body: %s", w.Code, w.Body.String())
	}
	cleanupStartedSession(t, srv, w)
	if admitCalls != 1 {
		t.Fatalf("capacityAdmit calls = %d, want 1: one_shot is a client flag and must not skip admission", admitCalls)
	}
}

func startWith(t *testing.T, srv *Server, header string) (admitClass capacity.Class, calls int) {
	t.Helper()
	srv.SetCapacityAdmit(func(ctx context.Context, _, _, _ string, _ int) (string, error) {
		calls++
		admitClass = capacity.ClassFrom(ctx)
		return "", nil
	})
	body, _ := json.Marshal(map[string]any{"task": "hello", "project_dir": t.TempDir(), "one_shot": true})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/start", bytes.NewReader(body))
	if header != "" {
		req.Header.Set(InternalLaunchHeader, header)
	}
	w := httptest.NewRecorder()
	srv.handleStartSession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body: %s", w.Code, w.Body.String())
	}
	cleanupStartedSession(t, srv, w)
	return admitClass, calls
}

// A start with no marker is an operator start, even with one_shot true.
func TestStartAdmissionClass_UnmarkedIsOperator(t *testing.T) {
	srv := newLLMServer(t)
	srv.manager.WithFakeTmux()
	if c, n := startWith(t, srv, ""); n != 1 || c != capacity.ClassOperator {
		t.Fatalf("class %q calls %d, want operator, 1", c, n)
	}
}

// A forged marker (wrong token, or a class a client may not claim) is operator.
func TestStartAdmissionClass_ForgedMarkerIsOperator(t *testing.T) {
	srv := newLLMServer(t)
	srv.manager.WithFakeTmux()
	for _, h := range []string{"worker:not-the-token", "supervisor:guess", "worker", "operator:" + internalLaunchToken, ":" + internalLaunchToken} {
		if c, n := startWith(t, srv, h); n != 1 || c != capacity.ClassOperator {
			t.Errorf("header %q: class %q calls %d, want operator, 1", h, c, n)
		}
	}
}

// The daemon's supervisor marker admits in the supervisor class.
func TestStartAdmissionClass_SupervisorMarker(t *testing.T) {
	srv := newLLMServer(t)
	srv.manager.WithFakeTmux()
	if c, n := startWith(t, srv, InternalLaunchValue(capacity.ClassSupervisor)); n != 1 || c != capacity.ClassSupervisor {
		t.Fatalf("class %q calls %d, want supervisor, 1", c, n)
	}
}
