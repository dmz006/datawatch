package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func startWithProfile(t *testing.T, srv *Server) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"task": "hello", "project_dir": t.TempDir(), "launch_role": "supervisor",
		"worker_profile": map[string]any{"name": "lean"},
		"model":          "m1",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/start", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleStartSession(w, req)
	return w
}

// A profiled start admits exactly like a plain one (one admission, same
// arguments) and succeeds when the fake ledger grants at once.
func TestProfiledStart_AdmitsOnceAndSucceeds(t *testing.T) {
	srv := newLLMServer(t)
	srv.manager.WithFakeTmux()
	var calls int
	var gotModel string
	srv.SetCapacityAdmit(func(_ context.Context, _, model, _ string, _ int) (string, error) {
		calls++
		gotModel = model
		return "", nil
	})
	srv.SetCapacityBind(func(string, string) {})
	w := startWithProfile(t, srv)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	cleanupStartedSession(t, srv, w)
	if calls != 1 || gotModel != "m1" {
		t.Fatalf("admit calls=%d model=%q, want 1 and m1", calls, gotModel)
	}
}

// A deadline from the ledger is refused with 503 and a message that names the
// model, node and wait timeout, never a bare "context deadline exceeded".
func TestProfiledStart_DeadlineNamesCause(t *testing.T) {
	srv := newLLMServer(t)
	srv.manager.WithFakeTmux()
	srv.SetCapacityAdmit(func(context.Context, string, string, string, int) (string, error) {
		return "", fmt.Errorf("%w: waiting on llm:x full", context.DeadlineExceeded)
	})
	w := startWithProfile(t, srv)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", w.Code)
	}
	b := w.Body.String()
	for _, want := range []string{"capacity: ", "model=m1", "node=", "wait_timeout=8s", "llm:x full", "capacity_wait_seconds"} {
		if !strings.Contains(b, want) {
			t.Errorf("body %q missing %q", b, want)
		}
	}
}

func TestCapacityRefusalMessage_BareDeadline(t *testing.T) {
	m := capacityRefusalMessage(context.DeadlineExceeded, "", "", "", 5)
	if !strings.Contains(m, "waited 5s") || !strings.Contains(m, "llm=(none)") {
		t.Fatalf("message not descriptive: %q", m)
	}
}
