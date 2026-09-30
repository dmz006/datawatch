// v8.36.0 — memory-strategy (keep/purge/archive) on session delete, matching
// the PRD hard-delete BL386/#175 feature. A session's memories were
// previously always silently kept with no operator choice at all — not even
// the backend supported anything else, let alone Android or the PWA.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

func newSessionDeleteTestServer(t *testing.T) (*Server, *scopeTestBackend) {
	t.Helper()
	dir := t.TempDir()
	sm, err := session.NewManager("h", dir, "echo", 30*time.Second)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	b := &scopeTestBackend{}
	srv := NewServer(NewHub(), sm, "h", "", nil, nil, "")
	srv.memoryBackend = b
	return srv, b
}

func startTestSession(t *testing.T, srv *Server, projectDir string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"task": "hello", "project_dir": projectDir})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/start", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleStartSession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleStartSession: status %d body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		FullID string `json:"full_id"`
		ID     string `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.FullID != "" {
		return resp.FullID
	}
	return resp.ID
}

// TestHandleDeleteSession_MemoryStrategyPurge verifies purge removes only
// the deleted session's own memories, leaving another session's untouched —
// exercising the PurgeScope fix (session-local purges were previously
// project-wide, see bl386_phase3_test.go's regression tests).
func TestHandleDeleteSession_MemoryStrategyPurge(t *testing.T) {
	srv, backend := newSessionDeleteTestServer(t)
	projectDir := t.TempDir()
	sessA := startTestSession(t, srv, projectDir)
	sessB := startTestSession(t, srv, projectDir)
	// sessA is deleted by the test itself below; sessB never is (it's the
	// control used to verify the OTHER session's memory survives), so its
	// spawned tmux session would otherwise leak on every run.
	t.Cleanup(func() { _ = srv.manager.Delete(sessB, false) })

	_, _ = backend.Save(projectDir, "session A memory", "", "", sessA, nil)
	_, _ = backend.Save(projectDir, "session B memory", "", "", sessB, nil)

	body, _ := json.Marshal(map[string]any{"id": sessA, "memory_strategy": "purge"})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/delete", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleDeleteSession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleDeleteSession: status %d body: %s", w.Code, w.Body.String())
	}

	remaining, _ := backend.ListRecent(projectDir, 100)
	if len(remaining) != 1 || remaining[0].SessionID != sessB {
		t.Fatalf("expected only session B's memory to remain, got: %+v", remaining)
	}
}

// TestHandleDeleteSession_MemoryStrategyKeep_Default verifies omitting
// memory_strategy (or "keep") leaves memories untouched — the pre-existing
// behavior, unaffected by this change.
func TestHandleDeleteSession_MemoryStrategyKeep_Default(t *testing.T) {
	srv, backend := newSessionDeleteTestServer(t)
	projectDir := t.TempDir()
	sess := startTestSession(t, srv, projectDir)
	_, _ = backend.Save(projectDir, "keep me", "", "", sess, nil)

	body, _ := json.Marshal(map[string]any{"id": sess})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/delete", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleDeleteSession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleDeleteSession: status %d body: %s", w.Code, w.Body.String())
	}

	remaining, _ := backend.ListRecent(projectDir, 100)
	if len(remaining) != 1 {
		t.Fatalf("expected the memory to survive a plain (keep) delete, got: %+v", remaining)
	}
}
