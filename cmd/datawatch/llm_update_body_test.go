package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestLLMUpdate_OmitsUnsetKindModelTimeout confirms `llm update <name>
// --compute-nodes ...` (no --kind/--model/--timeout-seconds) sends a body
// that omits those three keys entirely, so the server's merge-onto-existing
// PUT handler (internal/server/inference.go) leaves the stored entry's
// existing kind/model/timeout untouched. Found live 2026-10-16: without
// this, every `llm update` call silently reset kind to "ollama" (the flag's
// own default), which made an autonomous task launch the wrong backend.
func TestLLMUpdate_OmitsUnsetKindModelTimeout(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"pi","ok":true}`))
	}))
	defer srv.Close()

	oldServerURL := serverURL
	serverURL = srv.URL
	defer func() { serverURL = oldServerURL }()

	cmd := newLLMAddCmd(true) // update=true
	cmd.SetArgs([]string{"pi", "--compute-nodes", "local-node"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	for _, key := range []string{"kind", "model", "timeout_seconds"} {
		if _, present := gotBody[key]; present {
			t.Errorf("update body must omit unset %q, got body: %v", key, gotBody)
		}
	}
	if gotBody["name"] != "pi" {
		t.Errorf("expected name=pi, got %v", gotBody["name"])
	}
	nodes, _ := gotBody["compute_nodes"].([]any)
	if len(nodes) != 1 || nodes[0] != "local-node" {
		t.Errorf("expected compute_nodes=[local-node], got %v", gotBody["compute_nodes"])
	}
}

// TestLLMUpdate_IncludesKindWhenExplicitlyPassed confirms the opposite case
// still works: when the operator DOES pass --kind on an update, it's sent.
func TestLLMUpdate_IncludesKindWhenExplicitlyPassed(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"pi","ok":true}`))
	}))
	defer srv.Close()

	oldServerURL := serverURL
	serverURL = srv.URL
	defer func() { serverURL = oldServerURL }()

	cmd := newLLMAddCmd(true)
	cmd.SetArgs([]string{"pi", "--kind", "pi", "--model", "ollama/qwen3.8:27b"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if gotBody["kind"] != "pi" {
		t.Errorf("expected kind=pi when explicitly passed, got %v", gotBody["kind"])
	}
	if gotBody["model"] != "ollama/qwen3.8:27b" {
		t.Errorf("expected model to be passed through, got %v", gotBody["model"])
	}
}

// TestLLMAdd_AlwaysIncludesKindModelTimeout confirms `add` (a brand-new
// entry, where these fields need SOME value) keeps sending kind/model/
// timeout_seconds unconditionally, unaffected by the update-only fix above.
func TestLLMAdd_AlwaysIncludesKindModelTimeout(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"newllm","ok":true}`))
	}))
	defer srv.Close()

	oldServerURL := serverURL
	serverURL = srv.URL
	defer func() { serverURL = oldServerURL }()

	cmd := newLLMAddCmd(false) // update=false
	cmd.SetArgs([]string{"newllm"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	for _, key := range []string{"kind", "model", "timeout_seconds"} {
		if _, present := gotBody[key]; !present {
			t.Errorf("add body must always include %q, got body: %v", key, gotBody)
		}
	}
	if gotBody["kind"] != "ollama" {
		t.Errorf("expected default kind=ollama on add, got %v", gotBody["kind"])
	}
}
