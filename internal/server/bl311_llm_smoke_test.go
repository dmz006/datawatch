// BL311 — Gaps #1, #2, #3, #4, #8: smoke-style tests for the /api/llms
// and /api/backends HTTP surfaces plus session-start LLM resolution.
//
// These complement the per-verb CRUD tests in bl311_llm_crud_test.go
// with shape-validation, disconnect-proof, and session-routing tests.

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

// ---------------------------------------------------------------------------
// Gap #1 — /api/llms smoke round-trip with JSON shape validation
// ---------------------------------------------------------------------------

// TestLLMsHTTPRoundTrip_ShapeValidation walks the full POST→GET→PUT→DELETE→GET
// cycle and asserts that the JSON shape at each step contains the expected
// top-level fields (name, kind, model).
func TestLLMsHTTPRoundTrip_ShapeValidation(t *testing.T) {
	srv := newLLMServer(t)

	// --- POST (create) ---
	// The POST response shape is {"name": ..., "ok": true} (not the full LLM object).
	createBody, _ := json.Marshal(map[string]any{
		"name":  "smoke-ollama",
		"kind":  "ollama",
		"model": "llama3",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/llms", bytes.NewReader(createBody))
	w := httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST create: status %d body: %s", w.Code, w.Body.String())
	}
	var createResp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&createResp); err != nil {
		t.Fatalf("POST: decode response: %v", err)
	}
	// POST returns {"name": ..., "ok": true} — confirm both keys present.
	for _, field := range []string{"name", "ok"} {
		if _, ok := createResp[field]; !ok {
			t.Errorf("POST response missing field %q", field)
		}
	}
	if createResp["name"] != "smoke-ollama" {
		t.Errorf("POST name: got %v, want smoke-ollama", createResp["name"])
	}

	// --- GET single — verify shape ---
	req = httptest.NewRequest(http.MethodGet, "/api/llms/smoke-ollama", nil)
	req.URL.Path = "/api/llms/smoke-ollama"
	w = httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET single: status %d body: %s", w.Code, w.Body.String())
	}
	var getResp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&getResp); err != nil {
		t.Fatalf("GET: decode response: %v", err)
	}
	for _, field := range []string{"name", "kind", "model"} {
		if _, ok := getResp[field]; !ok {
			t.Errorf("GET response missing field %q", field)
		}
	}
	if getResp["name"] != "smoke-ollama" {
		t.Errorf("GET name: got %v, want smoke-ollama", getResp["name"])
	}
	if getResp["kind"] != "ollama" {
		t.Errorf("GET kind: got %v, want ollama", getResp["kind"])
	}
	if getResp["model"] != "llama3" {
		t.Errorf("GET model: got %v, want llama3", getResp["model"])
	}

	// --- PUT (update model) — PUT also returns {"name": ..., "ok": true} ---
	updateBody, _ := json.Marshal(map[string]any{
		"name":  "smoke-ollama",
		"kind":  "ollama",
		"model": "llama3.1",
	})
	req = httptest.NewRequest(http.MethodPut, "/api/llms/smoke-ollama", bytes.NewReader(updateBody))
	req.URL.Path = "/api/llms/smoke-ollama"
	w = httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT update: status %d body: %s", w.Code, w.Body.String())
	}
	var putResp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&putResp); err != nil {
		t.Fatalf("PUT: decode response: %v", err)
	}
	for _, field := range []string{"name", "ok"} {
		if _, ok := putResp[field]; !ok {
			t.Errorf("PUT response missing field %q", field)
		}
	}

	// GET after update — verify model field changed
	req = httptest.NewRequest(http.MethodGet, "/api/llms/smoke-ollama", nil)
	req.URL.Path = "/api/llms/smoke-ollama"
	w = httptest.NewRecorder()
	srv.handleLLMs(w, req)
	var afterUpdate map[string]any
	if err := json.NewDecoder(w.Body).Decode(&afterUpdate); err != nil {
		t.Fatalf("GET after update: decode: %v", err)
	}
	if afterUpdate["model"] != "llama3.1" {
		t.Errorf("PUT: model after update got %v, want llama3.1", afterUpdate["model"])
	}

	// --- DELETE ---
	req = httptest.NewRequest(http.MethodDelete, "/api/llms/smoke-ollama", nil)
	req.URL.Path = "/api/llms/smoke-ollama"
	w = httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE: status %d body: %s", w.Code, w.Body.String())
	}

	// --- GET after delete — must be 404 ---
	req = httptest.NewRequest(http.MethodGet, "/api/llms/smoke-ollama", nil)
	req.URL.Path = "/api/llms/smoke-ollama"
	w = httptest.NewRecorder()
	srv.handleLLMs(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET after delete: expected 404, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Gap #2 — Named LLM does NOT appear in /api/backends
// ---------------------------------------------------------------------------

// TestNamedLLMNotInBackends proves that a named LLM added to the inference
// registry via /api/llms does NOT bleed into /api/backends when the registry
// branch is NOT wired.  The two systems are intentionally disconnected;
// autonomous _prdBackends must call /api/llms, not /api/backends.
func TestNamedLLMNotInBackends(t *testing.T) {
	// Build a server with a fresh registry, add a named LLM, but do NOT
	// wire inferenceReg to the server (simulating the disconnected path).
	srv := newLLMServer(t)

	// Detach the registry from the server — only the /api/llms handler
	// uses inferenceReg; handleBackends must use the old availableBackends
	// list when inferenceReg is nil.
	detachedReg := srv.inferenceReg
	_ = detachedReg.Add(&inference.LLM{Name: "named-datawatch", Kind: inference.KindOllama})
	srv.inferenceReg = nil // disconnect — handleBackends falls back to availableBackends

	// /api/backends must NOT show the named LLM.
	req := httptest.NewRequest(http.MethodGet, "/api/backends", nil)
	w := httptest.NewRecorder()
	srv.handleBackends(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleBackends: status %d", w.Code)
	}

	var body struct {
		LLM []struct {
			Name string `json:"name"`
		} `json:"llm"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode /api/backends: %v", err)
	}
	for _, b := range body.LLM {
		if b.Name == "named-datawatch" {
			t.Error("/api/backends listed named-datawatch — the two systems must be disconnected; use /api/llms for named LLMs")
		}
	}
}

// TestNamedLLMInBackendsWhenRegistryWired is the complementary positive test:
// when the registry IS wired to the server, named LLMs DO appear in
// /api/backends (via the BL305 registry-first branch).
func TestNamedLLMInBackendsWhenRegistryWired(t *testing.T) {
	srv := newLLMServer(t)
	_ = srv.inferenceReg.Add(&inference.LLM{Name: "wired-ollama", Kind: inference.KindOllama})

	req := httptest.NewRequest(http.MethodGet, "/api/backends", nil)
	w := httptest.NewRecorder()
	srv.handleBackends(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleBackends: status %d", w.Code)
	}

	var body struct {
		LLM []struct {
			Name string `json:"name"`
		} `json:"llm"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, b := range body.LLM {
		if b.Name == "wired-ollama" {
			found = true
		}
	}
	if !found {
		t.Error("/api/backends did not list wired-ollama even though registry is wired")
	}
}

// ---------------------------------------------------------------------------
// Gap #3 — Session start with named LLM resolves through inference registry
// ---------------------------------------------------------------------------

// TestSessionStart_WithNamedLLM verifies the v7 session-start path: when
// req.LLM is set to a name that exists in the inference registry, the handler
// resolves it (GET from registry) and proceeds without returning 400.
// When the name is absent from the registry, a 400 is returned immediately.
func TestSessionStart_WithNamedLLM(t *testing.T) {
	srv := newLLMServer(t)

	// Register a named session-backend LLM (claude-code kind is a session backend).
	_ = srv.inferenceReg.Add(&inference.LLM{Name: "named-test", Kind: inference.KindClaudeCode})

	// POST with llm:"named-test" — registry lookup must succeed, handler
	// proceeds to manager.Start which uses "echo" binary in test setup
	// (via newLLMServer → session.NewManager with "echo" as the shell).
	body, _ := json.Marshal(map[string]any{
		"task":        "hello",
		"llm":         "named-test",
		"project_dir": t.TempDir(),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/start", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleStartSession(w, req)

	// The registry lookup must NOT return 400 (which would mean the named
	// LLM was not found). 200 or 500 are both fine — 500 means the
	// session manager couldn't exec the backend, which is expected in a
	// test environment without a real binary.
	if w.Code == http.StatusBadRequest {
		t.Errorf("handleStartSession with known named LLM returned 400: %s", w.Body.String())
	}
}

// TestSessionStart_UnknownNamedLLM verifies that a request with an llm field
// that doesn't exist in the registry is rejected with 400 immediately.
func TestSessionStart_UnknownNamedLLM(t *testing.T) {
	srv := newLLMServer(t)
	// Registry is empty — "does-not-exist" is unknown.

	body, _ := json.Marshal(map[string]any{
		"task":        "hello",
		"llm":         "does-not-exist",
		"project_dir": t.TempDir(),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/start", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleStartSession(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown LLM, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Gap #4 — Named LLM vs adapter-type code path distinction
// ---------------------------------------------------------------------------

// TestSessionStart_NamedVsAdapterType sends two requests:
//   - one with backend:"ollama" (legacy adapter-type path — no registry lookup)
//   - one with llm:"ollama-named" (named-LLM path — registry lookup required)
//
// The named-LLM path must 400 when the name is absent from the registry;
// the backend path must NOT 400 on registry grounds (it may 500 from exec).
func TestSessionStart_NamedVsAdapterType(t *testing.T) {
	srv := newLLMServer(t)

	// --- Request A: legacy backend field (adapter-type path) ---
	bodyA, _ := json.Marshal(map[string]any{
		"task":        "hello",
		"backend":     "ollama",
		"project_dir": t.TempDir(),
	})
	reqA := httptest.NewRequest(http.MethodPost, "/api/sessions/start", bytes.NewReader(bodyA))
	wA := httptest.NewRecorder()
	srv.handleStartSession(wA, reqA)

	// The legacy backend path should NOT 400 on registry grounds.
	// (It may 500 because "ollama" binary isn't installed in tests.)
	if wA.Code == http.StatusBadRequest {
		// A 400 from the legacy path would mean unexpected registry gate.
		// Allow it only if the body mentions "compute_node" (different gate).
		if bodyStr := wA.Body.String(); !strings.Contains(bodyStr, "compute_node") {
			t.Errorf("backend path returned 400 unexpectedly: %s", bodyStr)
		}
	}

	// --- Request B: named LLM path (registry lookup required) ---
	bodyB, _ := json.Marshal(map[string]any{
		"task":        "hello",
		"llm":         "ollama-named", // NOT in the empty registry
		"project_dir": t.TempDir(),
	})
	reqB := httptest.NewRequest(http.MethodPost, "/api/sessions/start", bytes.NewReader(bodyB))
	wB := httptest.NewRecorder()
	srv.handleStartSession(wB, reqB)

	// Named-LLM path with an unregistered name must 400.
	if wB.Code != http.StatusBadRequest {
		t.Errorf("named-LLM path for absent name: expected 400, got %d (body: %s)", wB.Code, wB.Body.String())
	}

	// Verify the two paths produced different outcomes: A was not 400
	// on registry grounds, B was 400 on registry grounds.
	if wA.Code == wB.Code && wA.Code == http.StatusBadRequest {
		t.Error("both paths returned 400 — they should take distinct code paths")
	}
}

// ---------------------------------------------------------------------------
// Gap #8 — /api/backends response shape smoke
// ---------------------------------------------------------------------------

// TestBackendsResponseShape calls handleBackends and verifies the response
// has the {"llm":[...],"active":"..."} shape that the PWA and automata
// pickers depend on.
func TestBackendsResponseShape(t *testing.T) {
	srv := newBackendsServer(t)
	// Wire a registry so the BL305 branch runs (more interesting shape).
	reg := inference.NewRegistry()
	_ = reg.Add(&inference.LLM{Name: "shape-ollama", Kind: inference.KindOllama})
	srv.inferenceReg = reg

	req := httptest.NewRequest(http.MethodGet, "/api/backends", nil)
	w := httptest.NewRecorder()
	srv.handleBackends(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("handleBackends: status %d body: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}

	var body map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Must have "llm" key as an array.
	llmRaw, ok := body["llm"]
	if !ok {
		t.Fatal("response missing 'llm' key")
	}
	var llmList []map[string]any
	if err := json.Unmarshal(llmRaw, &llmList); err != nil {
		t.Fatalf("'llm' is not an array: %v", err)
	}

	// Must have "active" key.
	if _, ok := body["active"]; !ok {
		t.Error("response missing 'active' key")
	}

	// Each entry in the llm array must have at least a "name" field.
	for i, entry := range llmList {
		if _, ok := entry["name"]; !ok {
			t.Errorf("llm[%d] missing 'name' field", i)
		}
	}

	// shape-ollama must appear.
	found := false
	for _, entry := range llmList {
		if entry["name"] == "shape-ollama" {
			found = true
		}
	}
	if !found {
		t.Error("shape-ollama not found in /api/backends llm array")
	}
}

// TestBackendsResponseShape_NoRegistry verifies that the no-registry fallback
// path also produces the same {"llm":[...],"active":"..."} shape.
func TestBackendsResponseShape_NoRegistry(t *testing.T) {
	srv := newBackendsServer(t)
	// No inferenceReg — old path.

	req := httptest.NewRequest(http.MethodGet, "/api/backends", nil)
	w := httptest.NewRecorder()
	srv.handleBackends(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d body: %s", w.Code, w.Body.String())
	}

	var body map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["llm"]; !ok {
		t.Error("no-registry response missing 'llm' key")
	}
	if _, ok := body["active"]; !ok {
		t.Error("no-registry response missing 'active' key")
	}
}
