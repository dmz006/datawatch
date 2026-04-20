// BL34 — read-only `ask` endpoint tests.
// Mocks ollama via a local httptest server and asserts the wire
// contract end-to-end through handleAsk.

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBL34_Ask_RejectsGet(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/ask", nil)
	rr := httptest.NewRecorder()
	s.handleAsk(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", rr.Code)
	}
}

func TestBL34_Ask_EmptyQuestionRejected(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodPost, "/api/ask",
		strings.NewReader(`{"question":""}`))
	rr := httptest.NewRecorder()
	s.handleAsk(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rr.Code)
	}
}

func TestBL34_Ask_UnsupportedBackend(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodPost, "/api/ask",
		strings.NewReader(`{"question":"hi","backend":"junk"}`))
	rr := httptest.NewRecorder()
	s.handleAsk(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400 for unsupported backend, got %d", rr.Code)
	}
}

func TestBL34_Ask_OllamaNotConfigured(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodPost, "/api/ask",
		strings.NewReader(`{"question":"hi"}`))
	rr := httptest.NewRecorder()
	s.handleAsk(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("want 500 when ollama.host empty, got %d", rr.Code)
	}
}

func TestBL34_Ask_OllamaHappyPath(t *testing.T) {
	// Stand up a fake Ollama server.
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] == nil || body["prompt"] == nil {
			http.Error(w, "missing fields", http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"response":"hello back"}`)
	}))
	defer mock.Close()

	s := bl90Server(t)
	s.cfg.Ollama.Host = mock.URL
	s.cfg.Ollama.Model = "llama3.2:1b"

	req := httptest.NewRequest(http.MethodPost, "/api/ask",
		strings.NewReader(`{"question":"are you there?"}`))
	rr := httptest.NewRecorder()
	s.handleAsk(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got AskResponse
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.Answer != "hello back" {
		t.Errorf("answer=%q want 'hello back'", got.Answer)
	}
	if got.Backend != "ollama" {
		t.Errorf("backend=%q want ollama", got.Backend)
	}
	if got.DurationMs < 0 {
		t.Errorf("duration_ms = %d, want >= 0", got.DurationMs)
	}
}
