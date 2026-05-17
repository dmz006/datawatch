// BL311 (partial) — HTTP-layer integration tests for /api/llms CRUD
// and llmEnabled unit coverage. These are the highest-value gaps from
// the 2026-05-16 LLM audit that can be covered with unit tests.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/session"
)

func newLLMServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	sm, err := session.NewManager("h", dir, "echo", 30*time.Second)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	hub := NewHub()
	srv := NewServer(hub, sm, "h", "", nil, nil, "")
	srv.inferenceReg = inference.NewRegistry()
	return srv
}

// TestHandleLLMsCRUD verifies the full create-read-update-delete cycle
// for the /api/llms HTTP surface.
func TestHandleLLMsCRUD(t *testing.T) {
	srv := newLLMServer(t)

	// POST — create
	body, _ := json.Marshal(map[string]any{"name": "test-ollama", "kind": "ollama"})
	req := httptest.NewRequest(http.MethodPost, "/api/llms", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create: status %d body: %s", w.Code, w.Body.String())
	}

	// GET — fetch by name
	req = httptest.NewRequest(http.MethodGet, "/api/llms/test-ollama", nil)
	req.URL.Path = "/api/llms/test-ollama"
	w = httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get: status %d body: %s", w.Code, w.Body.String())
	}
	var got inference.LLM
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if got.Name != "test-ollama" {
		t.Errorf("get: name got %q, want %q", got.Name, "test-ollama")
	}

	// GET all — should include the newly created LLM
	req = httptest.NewRequest(http.MethodGet, "/api/llms", nil)
	req.URL.Path = "/api/llms"
	w = httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: status %d", w.Code)
	}
	var list struct {
		LLMs []inference.LLM `json:"llms"`
	}
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	found := false
	for _, l := range list.LLMs {
		if l.Name == "test-ollama" {
			found = true
			break
		}
	}
	if !found {
		t.Error("list: test-ollama not found in /api/llms response")
	}

	// PUT — update (add a model)
	body, _ = json.Marshal(map[string]any{"name": "test-ollama", "kind": "ollama", "model": "llama3"})
	req = httptest.NewRequest(http.MethodPut, "/api/llms/test-ollama", bytes.NewReader(body))
	req.URL.Path = "/api/llms/test-ollama"
	w = httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update: status %d body: %s", w.Code, w.Body.String())
	}

	// GET — verify update
	req = httptest.NewRequest(http.MethodGet, "/api/llms/test-ollama", nil)
	req.URL.Path = "/api/llms/test-ollama"
	w = httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode after update: %v", err)
	}
	if got.Model != "llama3" {
		t.Errorf("update: model got %q, want %q", got.Model, "llama3")
	}

	// DELETE
	req = httptest.NewRequest(http.MethodDelete, "/api/llms/test-ollama", nil)
	req.URL.Path = "/api/llms/test-ollama"
	w = httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: status %d body: %s", w.Code, w.Body.String())
	}

	// GET — should be 404 after delete
	req = httptest.NewRequest(http.MethodGet, "/api/llms/test-ollama", nil)
	req.URL.Path = "/api/llms/test-ollama"
	w = httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get after delete: expected 404, got %d", w.Code)
	}
}

// TestHandleLLMs_ConflictOnDuplicate verifies 409 when creating the same LLM twice.
func TestHandleLLMs_ConflictOnDuplicate(t *testing.T) {
	srv := newLLMServer(t)

	body, _ := json.Marshal(map[string]any{"name": "dup-llm", "kind": "ollama"})
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/llms", bytes.NewReader(body))
		w := httptest.NewRecorder()
		srv.handleLLMs(w, req)
		if i == 0 && w.Code != http.StatusOK {
			t.Fatalf("first create: status %d", w.Code)
		}
		if i == 1 && w.Code != http.StatusConflict {
			t.Fatalf("duplicate create: expected 409, got %d", w.Code)
		}
		body, _ = json.Marshal(map[string]any{"name": "dup-llm", "kind": "ollama"}) // re-create body
	}
}

// TestLLMEnabled_KnownAdapters documents the llmEnabled switch: only YAML-flagged
// adapter kinds return true; named registry LLMs (any string not in the switch)
// always return false. This test pins the behavior so regressions are caught.
func TestLLMEnabled_KnownAdapters(t *testing.T) {
	srv := &Server{
		cfg: &config.Config{},
	}

	// All adapters disabled by default (zero config).
	knownAdapters := []string{
		"claude-code", "aider", "goose", "gemini", "ollama",
		"opencode", "opencode-acp", "opencode-prompt", "openwebui", "shell",
	}
	for _, name := range knownAdapters {
		if srv.llmEnabled(name) {
			t.Errorf("llmEnabled(%q) = true with zero config, want false", name)
		}
	}

	// Named LLMs from the inference registry are not handled by the switch.
	namedLLMs := []string{"ollama-datawatch", "my-claude", "custom-openwebui", ""}
	for _, name := range namedLLMs {
		if srv.llmEnabled(name) {
			t.Errorf("llmEnabled(%q) = true, want false (named LLMs not in switch)", name)
		}
	}

	// Enable one adapter and verify.
	srv.cfg.Ollama.Enabled = true
	if !srv.llmEnabled("ollama") {
		t.Error("llmEnabled(ollama) = false after enabling, want true")
	}
	if srv.llmEnabled("claude-code") {
		t.Error("llmEnabled(claude-code) = true, should still be false")
	}
}

// TestHandleLLMs_DisabledRegistryReturns503 verifies the guard clause.
func TestHandleLLMs_DisabledRegistryReturns503(t *testing.T) {
	dir := t.TempDir()
	sm, _ := session.NewManager("h", dir, "echo", 30*time.Second)
	srv := NewServer(NewHub(), sm, "h", "", nil, nil, "")
	// No inferenceReg — simulates a pre-migration or degraded install.

	req := httptest.NewRequest(http.MethodGet, "/api/llms", nil)
	w := httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without registry, got %d", w.Code)
	}
}
