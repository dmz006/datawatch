package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/inference"
)

// startWithNode posts a session start naming the LLM entry and a compute node.
func startWithNode(t *testing.T, srv *Server, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body["task"] = "hello"
	body["project_dir"] = t.TempDir()
	raw, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	srv.handleStartSession(w, httptest.NewRequest(http.MethodPost, "/api/sessions/start", bytes.NewReader(raw)))
	cleanupStartedSession(t, srv, w)
	return w
}

func newPoolServer(t *testing.T, nodes ...string) *Server {
	t.Helper()
	srv := newLLMServer(t)
	if err := srv.inferenceReg.Add(&inference.LLM{Name: "pool", Kind: inference.KindOllama, ComputeNodes: nodes}); err != nil {
		t.Fatalf("Add llm: %v", err)
	}
	return srv
}

// A scheduler-recorded node that was removed from the entry's list is dropped
// and the start is not refused.
func TestStartSessionDropsAdvisoryNodeOutsideList(t *testing.T) {
	srv := newPoolServer(t, "b")
	for _, key := range []string{"llm", "backend"} {
		w := startWithNode(t, srv, map[string]any{key: "pool", "compute_node": "a", "compute_node_advisory": true})
		if strings.Contains(w.Body.String(), "compute_nodes list") {
			t.Fatalf("%s path: advisory node refused: %d %s", key, w.Code, w.Body.String())
		}
	}
}

// An operator-supplied node outside the list is still refused.
func TestStartSessionRefusesExplicitNodeOutsideList(t *testing.T) {
	srv := newPoolServer(t, "b")
	for _, key := range []string{"llm", "backend"} {
		w := startWithNode(t, srv, map[string]any{key: "pool", "compute_node": "a"})
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "not in llm pool compute_nodes list") {
			t.Fatalf("%s path: got %d %q, want 400 naming the compute_nodes list", key, w.Code, w.Body.String())
		}
	}
}

// An advisory node still in the list is kept, not replaced.
func TestStartSessionKeepsAdvisoryNodeInList(t *testing.T) {
	srv := newPoolServer(t, "a", "b")
	w := startWithNode(t, srv, map[string]any{"llm": "pool", "compute_node": "b", "compute_node_advisory": true})
	if strings.Contains(w.Body.String(), "compute_nodes list") {
		t.Fatalf("listed node refused: %d %s", w.Code, w.Body.String())
	}
}
