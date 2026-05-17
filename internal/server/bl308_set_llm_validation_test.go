// BL308 — set_llm and set_task_llm handlers reject unknown LLM names
// when the inference registry is present.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/session"
)

func newAutonomousServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	sm, err := session.NewManager("h", dir, "echo", 30*time.Second)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	hub := NewHub()
	srv := NewServer(hub, sm, "h", "", nil, nil, "")
	srv.SetAutonomousAPI(&fakeOrchAutonomous{})
	return srv
}

func TestSetLLM_RejectsUnknownBackend(t *testing.T) {
	srv := newAutonomousServer(t)

	reg := inference.NewRegistry()
	_ = reg.Add(&inference.LLM{Name: "good-llm", Kind: inference.KindOllama})
	srv.inferenceReg = reg

	body, _ := json.Marshal(map[string]string{"backend": "bad-llm"})
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/p1/set_llm", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleAutonomousPRDs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown backend, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSetLLM_AcceptsKnownBackend(t *testing.T) {
	srv := newAutonomousServer(t)

	reg := inference.NewRegistry()
	_ = reg.Add(&inference.LLM{Name: "good-llm", Kind: inference.KindOllama})
	srv.inferenceReg = reg

	body, _ := json.Marshal(map[string]string{"backend": "good-llm"})
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/p1/set_llm", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleAutonomousPRDs(w, req)

	// The fake autonomousMgr returns (nil, nil) from SetPRDLLM; the handler
	// then marshals nil as JSON — any non-400 2xx means validation passed.
	if w.Code == http.StatusBadRequest {
		t.Fatalf("known backend should not be rejected: %s", w.Body.String())
	}
}

func TestSetLLM_EmptyBackendSkipsValidation(t *testing.T) {
	srv := newAutonomousServer(t)

	reg := inference.NewRegistry()
	srv.inferenceReg = reg // empty registry

	// Empty backend means "inherit default" — no validation needed.
	body, _ := json.Marshal(map[string]string{"backend": ""})
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/p1/set_llm", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleAutonomousPRDs(w, req)

	if w.Code == http.StatusBadRequest {
		t.Fatalf("empty backend should not be rejected: %s", w.Body.String())
	}
}

func TestSetLLM_NoRegistryPassesThrough(t *testing.T) {
	srv := newAutonomousServer(t)
	// No inferenceReg — old behavior, any string is accepted.

	body, _ := json.Marshal(map[string]string{"backend": "whatever"})
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/p1/set_llm", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleAutonomousPRDs(w, req)

	if w.Code == http.StatusBadRequest {
		t.Fatalf("without registry any backend string should pass: %s", w.Body.String())
	}
}

func TestSetTaskLLM_RejectsUnknownBackend(t *testing.T) {
	srv := newAutonomousServer(t)

	reg := inference.NewRegistry()
	_ = reg.Add(&inference.LLM{Name: "good-llm", Kind: inference.KindOllama})
	srv.inferenceReg = reg

	body, _ := json.Marshal(map[string]string{"task_id": "t1", "backend": "bad-llm"})
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/p1/set_task_llm", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleAutonomousPRDs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown backend in set_task_llm, got %d: %s", w.Code, w.Body.String())
	}
}
