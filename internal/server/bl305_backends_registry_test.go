// BL305 — handleBackends uses inferenceReg as source of truth when present.
// Verifies that named registry LLMs appear in /api/backends, and that
// disabled entries are excluded.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/session"
)

func newBackendsServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	sm, err := session.NewManager("h", dir, "echo", 30*time.Second)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	hub := NewHub()
	srv := NewServer(hub, sm, "h", "", nil, nil, "")
	return srv
}

func TestHandleBackends_RegistryFirst(t *testing.T) {
	srv := newBackendsServer(t)

	reg := inference.NewRegistry()
	_ = reg.Add(&inference.LLM{Name: "ollama-datawatch", Kind: inference.KindOllama})
	_ = reg.Add(&inference.LLM{Name: "claude-code", Kind: inference.KindClaudeCode})
	disabled := &inference.LLM{Name: "old-openwebui", Kind: inference.KindOpenWebUI, Disabled: true}
	_ = reg.Add(disabled)
	srv.inferenceReg = reg

	req := httptest.NewRequest(http.MethodGet, "/api/backends", nil)
	w := httptest.NewRecorder()
	srv.handleBackends(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}

	var body struct {
		LLM []struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		} `json:"llm"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	names := map[string]bool{}
	for _, b := range body.LLM {
		names[b.Name] = b.Enabled
	}

	// Registry entries appear.
	if _, ok := names["ollama-datawatch"]; !ok {
		t.Error("expected ollama-datawatch in /api/backends")
	}
	if _, ok := names["claude-code"]; !ok {
		t.Error("expected claude-code in /api/backends")
	}
	// Disabled entry is excluded.
	if _, ok := names["old-openwebui"]; ok {
		t.Error("disabled LLM should not appear in /api/backends")
	}
}

func TestHandleBackends_FallsBackWithoutRegistry(t *testing.T) {
	srv := newBackendsServer(t)
	// No inferenceReg wired — old path returns availableBackends (empty here).
	req := httptest.NewRequest(http.MethodGet, "/api/backends", nil)
	w := httptest.NewRecorder()
	srv.handleBackends(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body: %s", w.Code, w.Body.String())
	}
	var body map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["llm"]; !ok {
		t.Error("expected llm key in /api/backends response")
	}
}
