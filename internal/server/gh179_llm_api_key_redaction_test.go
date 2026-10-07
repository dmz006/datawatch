// GH#179 — GET /api/llms and GET /api/llms/{name} must never echo a
// literal api_key_ref in clear text, and PUT must not wipe a stored
// literal key when the client echoes back a redacted (key-omitted) GET
// response on an unrelated field edit.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/inference"
)

func TestHandleLLMs_GetSingle_RedactsLiteralAPIKey(t *testing.T) {
	srv := newLLMServer(t)
	_ = srv.inferenceReg.Add(&inference.LLM{Name: "owui", Kind: inference.KindOpenWebUI, APIKeyRef: "sk-live-secret"})

	req := httptest.NewRequest(http.MethodGet, "/api/llms/owui", nil)
	req.URL.Path = "/api/llms/owui"
	w := httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get: status %d body: %s", w.Code, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("sk-live-secret")) {
		t.Fatalf("GET response leaked the literal api_key_ref: %s", w.Body.String())
	}
	var got inference.LLM
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.APIKeyRefPresent {
		t.Error("api_key_ref_present should be true")
	}
}

func TestHandleLLMs_List_RedactsLiteralAPIKey(t *testing.T) {
	srv := newLLMServer(t)
	_ = srv.inferenceReg.Add(&inference.LLM{Name: "owui", Kind: inference.KindOpenWebUI, APIKeyRef: "sk-live-secret"})

	req := httptest.NewRequest(http.MethodGet, "/api/llms", nil)
	req.URL.Path = "/api/llms"
	w := httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: status %d body: %s", w.Code, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("sk-live-secret")) {
		t.Fatalf("list response leaked the literal api_key_ref: %s", w.Body.String())
	}
}

func TestHandleLLMs_List_LeavesSecretRefVisible(t *testing.T) {
	srv := newLLMServer(t)
	_ = srv.inferenceReg.Add(&inference.LLM{Name: "claude", Kind: inference.KindClaude, APIKeyRef: "${secret:anthropic-key}"})

	req := httptest.NewRequest(http.MethodGet, "/api/llms", nil)
	req.URL.Path = "/api/llms"
	w := httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if !bytes.Contains(w.Body.Bytes(), []byte("${secret:anthropic-key}")) {
		t.Fatalf("a secret reference names a secret, not the secret itself — it should still be visible: %s", w.Body.String())
	}
}

// TestHandleLLMs_Put_PreservesLiteralKey_OnUnrelatedEdit reproduces the
// exact confirmed-fails-without-fix scenario: a client fetches the
// (redacted) GET representation, edits an unrelated field, and PUTs the
// result back without re-adding api_key_ref (since it never saw the real
// value). The real key must survive.
func TestHandleLLMs_Put_PreservesLiteralKey_OnUnrelatedEdit(t *testing.T) {
	srv := newLLMServer(t)
	_ = srv.inferenceReg.Add(&inference.LLM{Name: "owui", Kind: inference.KindOpenWebUI, APIKeyRef: "sk-live-secret", Model: "old-model"})

	// Simulate: client fetched the redacted GET, decoded it, changed Model,
	// re-marshaled — api_key_ref is absent from the outgoing JSON because
	// omitempty dropped it when APIKeyRef was cleared by Redacted().
	body, _ := json.Marshal(map[string]any{
		"name":  "owui",
		"kind":  "openwebui",
		"model": "new-model",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/llms/owui", bytes.NewReader(body))
	req.URL.Path = "/api/llms/owui"
	w := httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("put: status %d body: %s", w.Code, w.Body.String())
	}

	got, err := srv.inferenceReg.Get("owui")
	if err != nil {
		t.Fatalf("get after put: %v", err)
	}
	if got.APIKeyRef != "sk-live-secret" {
		t.Fatalf("literal api_key_ref must survive a PUT that omits it, got %q", got.APIKeyRef)
	}
	if got.Model != "new-model" {
		t.Fatalf("the actual intended edit (model) must still apply, got %q", got.Model)
	}
}

// TestHandleLLMs_Put_ExplicitEmptyClearsKey confirms the merge-onto-
// existing fix doesn't also make the key impossible to clear on purpose.
func TestHandleLLMs_Put_ExplicitEmptyClearsKey(t *testing.T) {
	srv := newLLMServer(t)
	_ = srv.inferenceReg.Add(&inference.LLM{Name: "owui", Kind: inference.KindOpenWebUI, APIKeyRef: "sk-live-secret"})

	body, _ := json.Marshal(map[string]any{
		"name":        "owui",
		"kind":        "openwebui",
		"api_key_ref": "",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/llms/owui", bytes.NewReader(body))
	req.URL.Path = "/api/llms/owui"
	w := httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("put: status %d body: %s", w.Code, w.Body.String())
	}

	got, err := srv.inferenceReg.Get("owui")
	if err != nil {
		t.Fatalf("get after put: %v", err)
	}
	if got.APIKeyRef != "" {
		t.Fatalf("an explicit empty api_key_ref must clear the stored key, got %q", got.APIKeyRef)
	}
}
