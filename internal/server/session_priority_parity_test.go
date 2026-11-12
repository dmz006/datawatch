// Session priority over REST: the value given on create and on update is
// stored on the session, returned by get and list, and a non-integer value is
// rejected with the shared validation error.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

func startSessionWithBody(t *testing.T, srv *Server, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/start", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleStartSession(w, req)
	return w
}

func TestSessionPriorityParityRESTCreateGetList(t *testing.T) {
	srv := newLLMServer(t)
	// In-memory tmux fake: handleStartSession then never touches a real
	// multiplexer, so nothing real can leak on this host.
	srv.manager.WithFakeTmux()
	w := startSessionWithBody(t, srv, map[string]any{"task": "hello", "project_dir": t.TempDir(), "priority": 7})
	if w.Code != http.StatusOK {
		t.Fatalf("start: status %d body: %s", w.Code, w.Body.String())
	}
	cleanupStartedSession(t, srv, w)

	var started session.Session
	if err := json.Unmarshal(w.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode start response: %v", err)
	}
	if started.Priority != 7 {
		t.Fatalf("start response priority = %d, want 7", started.Priority)
	}
	stored, ok := srv.manager.GetSession(started.FullID)
	if !ok || stored.Priority != 7 {
		t.Fatalf("stored priority = %+v, want 7", stored)
	}

	// get
	gw := httptest.NewRecorder()
	srv.handleSessionSubpath(gw, httptest.NewRequest(http.MethodGet, "/api/sessions/"+started.FullID, nil))
	var got session.Session
	if err := json.Unmarshal(gw.Body.Bytes(), &got); err != nil || got.Priority != 7 {
		t.Fatalf("get priority = %d (err %v), want 7; body %s", got.Priority, err, gw.Body.String())
	}

	// list
	lw := httptest.NewRecorder()
	srv.handleSessions(lw, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	var list []session.Session
	if err := json.Unmarshal(lw.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	found := false
	for _, s := range list {
		if s.FullID == started.FullID {
			found = true
			if s.Priority != 7 {
				t.Fatalf("list priority = %d, want 7", s.Priority)
			}
		}
	}
	if !found {
		t.Fatal("started session missing from list")
	}
}

func TestSessionPriorityParityRESTUpdate(t *testing.T) {
	srv := newLLMServer(t)
	// In-memory tmux fake: handleStartSession then never touches a real
	// multiplexer, so nothing real can leak on this host.
	srv.manager.WithFakeTmux()
	w := startSessionWithBody(t, srv, map[string]any{"task": "hello", "project_dir": t.TempDir()})
	if w.Code != http.StatusOK {
		t.Fatalf("start: status %d body: %s", w.Code, w.Body.String())
	}
	cleanupStartedSession(t, srv, w)
	var started session.Session
	_ = json.Unmarshal(w.Body.Bytes(), &started)
	if started.Priority != 0 {
		t.Fatalf("default priority = %d, want 0", started.Priority)
	}

	body, _ := json.Marshal(map[string]any{"id": started.FullID, "priority": 7})
	uw := httptest.NewRecorder()
	srv.handleSetSessionPriority(uw, httptest.NewRequest(http.MethodPost, "/api/sessions/set_priority", bytes.NewReader(body)))
	if uw.Code != http.StatusOK {
		t.Fatalf("set_priority: status %d body: %s", uw.Code, uw.Body.String())
	}
	if s, _ := srv.manager.GetSession(started.FullID); s == nil || s.Priority != 7 {
		t.Fatalf("stored priority after update = %+v, want 7", s)
	}
}

func TestSessionPriorityRESTRejectsNonInteger(t *testing.T) {
	srv := newLLMServer(t)
	for _, bad := range []any{1.5, "abc", true, []int{1}, map[string]any{}} {
		w := startSessionWithBody(t, srv, map[string]any{"task": "hello", "project_dir": t.TempDir(), "priority": bad})
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), session.ErrPriorityNotInteger.Error()) {
			t.Errorf("start with priority %v: status %d body %q", bad, w.Code, w.Body.String())
		}
		body, _ := json.Marshal(map[string]any{"id": "x", "priority": bad})
		uw := httptest.NewRecorder()
		srv.handleSetSessionPriority(uw, httptest.NewRequest(http.MethodPost, "/api/sessions/set_priority", bytes.NewReader(body)))
		if uw.Code != http.StatusBadRequest || !strings.Contains(uw.Body.String(), session.ErrPriorityNotInteger.Error()) {
			t.Errorf("set_priority with %v: status %d body %q", bad, uw.Code, uw.Body.String())
		}
	}
	if n := len(srv.manager.ListSessions()); n != 0 {
		t.Fatalf("rejected requests created %d sessions", n)
	}
}
