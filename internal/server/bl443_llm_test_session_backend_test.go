// BL443 — handleLLMTest must reject session-backend kinds (claude-code,
// opencode*, aider, goose, gemini, shell) with a clear, correct message
// instead of falling through to dispatcher.Call and surfacing a generic
// "no adapter for kind X" 502 that reads like a missing feature rather
// than the intentional design (#46, see inference.IsSessionBackendKind).
package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/inference"
)

func TestHandleLLMTest_SessionBackendKindRejectedClearly(t *testing.T) {
	srv := newLLMServer(t)
	srv.inferenceDisp = inference.NewDispatcher(srv.inferenceReg, func(name string) (*compute.Node, error) {
		return nil, nil
	})

	for _, kind := range []string{"goose", "aider", "claude-code"} {
		name := "test-" + kind
		if err := srv.inferenceReg.Add(&inference.LLM{Name: name, Kind: inference.Kind(kind), Binary: "/bin/true"}); err != nil {
			t.Fatalf("register %s: %v", kind, err)
		}

		body, _ := json.Marshal(map[string]any{"prompt": "hi"})
		req := httptest.NewRequest(http.MethodPost, "/api/llms/"+name+"/test", bytes.NewReader(body))
		w := httptest.NewRecorder()
		srv.handleLLMTest(w, req, name)

		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want %d; body: %s", kind, w.Code, http.StatusBadRequest, w.Body.String())
		}
		if !bytes.Contains(w.Body.Bytes(), []byte("session-backend kind")) {
			t.Errorf("%s: body %q does not explain the real reason", kind, w.Body.String())
		}
		if bytes.Contains(w.Body.Bytes(), []byte("no adapter for kind")) {
			t.Errorf("%s: still returning the misleading generic adapter error: %q", kind, w.Body.String())
		}
	}
}

// Sanity check the fix doesn't swallow real adapter kinds: ollama still
// goes through dispatcher.Call (and fails there, for an unrelated reason
// — no live node — not at the new session-backend guard).
func TestHandleLLMTest_RealAdapterKindStillDispatches(t *testing.T) {
	srv := newLLMServer(t)
	srv.inferenceDisp = inference.NewDispatcher(srv.inferenceReg, func(name string) (*compute.Node, error) {
		return nil, nil
	})
	if err := srv.inferenceReg.Add(&inference.LLM{Name: "test-ollama-real", Kind: inference.KindOllama}); err != nil {
		t.Fatalf("register: %v", err)
	}

	body, _ := json.Marshal(map[string]any{"prompt": "hi"})
	req := httptest.NewRequest(http.MethodPost, "/api/llms/test-ollama-real/test", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleLLMTest(w, req, "test-ollama-real")

	if bytes.Contains(w.Body.Bytes(), []byte("session-backend kind")) {
		t.Errorf("ollama should not hit the session-backend guard; body: %s", w.Body.String())
	}
}
