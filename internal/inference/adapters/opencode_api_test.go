// BL321 unit tests — OpenCodeAPI adapter.

package adapters

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/inference"
)

func TestOpenCodeAPI_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"content": "response from opencode",
					},
				},
			},
		})
	}))
	defer ts.Close()

	a := &OpenCodeAPI{}
	node := &compute.Node{Name: "oc-node", Address: ts.URL, Kind: compute.KindOpenAICompat}
	llm := &inference.LLM{
		Name:  "opencode-llm",
		Kind:  inference.KindOpenCodeAPI,
		Model: "claude-3-5-sonnet",
	}

	resp, err := a.Infer(context.Background(), node, llm, inference.Request{Prompt: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text != "response from opencode" {
		t.Errorf("expected 'response from opencode', got %q", resp.Text)
	}
	if resp.UsedModel != "claude-3-5-sonnet" {
		t.Errorf("expected model=claude-3-5-sonnet, got %q", resp.UsedModel)
	}
}

func TestOpenCodeAPI_MissingAddress(t *testing.T) {
	a := &OpenCodeAPI{}
	llm := &inference.LLM{
		Name:  "opencode-llm",
		Kind:  inference.KindOpenCodeAPI,
		Model: "claude-3-5-sonnet",
	}

	// nil node
	_, err := a.Infer(context.Background(), nil, llm, inference.Request{Prompt: "hi"})
	if err == nil {
		t.Fatal("expected error for nil node")
	}

	// node with empty address
	node := &compute.Node{Name: "empty-addr", Kind: compute.KindOpenAICompat}
	_, err = a.Infer(context.Background(), node, llm, inference.Request{Prompt: "hi"})
	if err == nil {
		t.Fatal("expected error for empty address")
	}
}

// Credential tests — same contract as the Claude and OpenWebUI adapters:
// ${secret:name} is resolved by the dispatcher into a shallow copy; the
// adapter never transmits an unresolved reference. OpenCodeAPI sends the
// key as the Authorization bearer header, so the 401 gate checks that.
// The fakeSecret and testNode helpers live in openwebui_test.go (same
// package).

// Test 3 — the regression: a fake server that returns 401 unless
// Authorization is exactly the resolved bearer value, driven through
// the DISPATCHER with a ${secret:name} reference. Post-resolution the
// dispatcher hands the adapter the resolved value and the server gets
// 200.
func TestOpenCodeAPI_DispatcherSecretRefSendsResolvedKey(t *testing.T) {
	const stored = "RESOLVED-OPENCODE-KEY-77"
	want := "Bearer " + stored
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != want {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("bad credentials"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"pong"}}]}`))
	}))
	defer ts.Close()

	reg := inference.NewRegistry()
	if err := reg.Add(&inference.LLM{
		Name:         "opencode",
		Kind:         inference.KindOpenCodeAPI,
		Model:        "claude-3-5-sonnet",
		APIKeyRef:    "${secret:opencode-key}",
		ComputeNodes: []string{"opencode-node"},
	}); err != nil {
		t.Fatalf("registry add: %v", err)
	}
	d := inference.NewDispatcher(reg, func(name string) (*compute.Node, error) {
		if name != "opencode-node" {
			return nil, compute.ErrNotFound
		}
		return &compute.Node{Name: "opencode-node", Kind: compute.KindOpenAICompat, Address: ts.URL}, nil
	})
	d.RegisterAdapter(&OpenCodeAPI{})
	d.SetSecretsStore(&fakeSecret{byName: map[string]string{"opencode-key": stored}}, nil)

	resp, err := d.Call(context.Background(), "opencode", inference.Request{Prompt: "ping"})
	if err != nil {
		t.Fatalf("expected 200 with the resolved key, got error: %v", err)
	}
	if resp.Text != "pong" {
		t.Fatalf("unexpected reply: %q", resp.Text)
	}
	// The registry entry must still carry the reference, not the value.
	got, gerr := reg.Get("opencode")
	if gerr != nil || got == nil {
		t.Fatalf("registry get: %v", gerr)
	}
	if got.APIKeyRef != "${secret:opencode-key}" {
		t.Fatalf("registry entry must keep the ${secret: reference, got %q", got.APIKeyRef)
	}
}

// Test 4 — an unresolved ${secret: reference handed straight to the
// adapter (with no resolving callsite) must send ZERO requests and
// return a final error that names the LLM entry and never the secret
// value.
func TestOpenCodeAPI_UnresolvedRefSendsNoRequest(t *testing.T) {
	var count int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"must never arrive"}}]}`))
	}))
	defer ts.Close()

	a := &OpenCodeAPI{}
	llm := &inference.LLM{
		Name:      "opencode-unresolved",
		Kind:      inference.KindOpenCodeAPI,
		Model:     "claude-3-5-sonnet",
		APIKeyRef: "${secret:opencode-key-missing}",
	}
	_, err := a.Infer(context.Background(), testNode(ts.URL), llm, inference.Request{Prompt: "ping"})
	if err == nil {
		t.Fatal("expected an error for an unresolved ${secret: reference, got nil")
	}
	if inference.IsTransient(err) {
		t.Fatalf("an unresolved reference must be FINAL, not transient: %v", err)
	}
	if got := atomic.LoadInt32(&count); got != 0 {
		t.Fatalf("adapter must send zero requests on an unresolved reference, server saw %d", got)
	}
	if !strings.Contains(err.Error(), "opencode-unresolved") {
		t.Fatalf("error must name the LLM entry: %v", err)
	}
	for _, forbidden := range []string{"opencode-key-missing", "${secret:", "Bearer"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("error must not leak the reference or a bearer token, got: %v", err)
		}
	}
}

// Test 5 — the literal-key path is unchanged: Authorization still
// carries the bearer key on the wire as-is and the 401-gated server
// accepts it.
func TestOpenCodeAPI_LiteralKeyStillWorks(t *testing.T) {
	const literal = "sk-oc-literal-123"
	want := "Bearer " + literal
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != want {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("bad credentials"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"pong"}}]}`))
	}))
	defer ts.Close()

	a := &OpenCodeAPI{}
	llm := &inference.LLM{
		Name:      "opencode-literal",
		Kind:      inference.KindOpenCodeAPI,
		Model:     "claude-3-5-sonnet",
		APIKeyRef: literal,
	}
	resp, err := a.Infer(context.Background(), testNode(ts.URL), llm, inference.Request{Prompt: "ping"})
	if err != nil {
		t.Fatalf("literal key must still work: %v", err)
	}
	if resp.Text != "pong" {
		t.Fatalf("unexpected reply: %q", resp.Text)
	}
}

// Test 6 — a server 5xx is still surfaced as a transient
// (failover-eligible) error.
func TestOpenCodeAPI_Server500IsTransient(t *testing.T) {
	var count int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream exploded"))
	}))
	defer ts.Close()

	a := &OpenCodeAPI{}
	llm := &inference.LLM{
		Name:  "opencode-500",
		Kind:  inference.KindOpenCodeAPI,
		Model: "claude-3-5-sonnet",
	}
	_, err := a.Infer(context.Background(), testNode(ts.URL), llm, inference.Request{Prompt: "ping"})
	if err == nil {
		t.Fatal("expected an error for a 500 response, got nil")
	}
	if !inference.IsTransient(err) {
		t.Fatalf("a 5xx must be transient so the dispatcher can fail over: %v", err)
	}
	if got := atomic.LoadInt32(&count); got != 1 {
		t.Fatalf("expected exactly one request to the server, saw %d", got)
	}
}
