// BL321 unit tests — Gemini adapter.

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

func TestGemini_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]string{
							{"text": "Hello, world!"},
						},
					},
				},
			},
		})
	}))
	defer ts.Close()

	a := &Gemini{}
	node := &compute.Node{Name: "gemini-node", Address: ts.URL, Kind: compute.KindOpenAICompat}
	llm := &inference.LLM{
		Name:      "gemini-test",
		Kind:      inference.KindGeminiAPI,
		Model:     "gemini-1.5-flash",
		APIKeyRef: "test-key",
	}

	resp, err := a.Infer(context.Background(), node, llm, inference.Request{Prompt: "Hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", resp.Text)
	}
	if resp.UsedModel != "gemini-1.5-flash" {
		t.Errorf("expected model=gemini-1.5-flash, got %q", resp.UsedModel)
	}
}

func TestGemini_MissingAPIKey(t *testing.T) {
	a := &Gemini{}
	llm := &inference.LLM{
		Name:  "gemini-test",
		Kind:  inference.KindGeminiAPI,
		Model: "gemini-1.5-flash",
		// APIKeyRef intentionally empty
	}
	_, err := a.Infer(context.Background(), nil, llm, inference.Request{Prompt: "Hi"})
	if err == nil {
		t.Fatal("expected error for missing api_key_ref")
	}
}

func TestGemini_500_Transient(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer ts.Close()

	a := &Gemini{}
	node := &compute.Node{Name: "gemini-node", Address: ts.URL, Kind: compute.KindOpenAICompat}
	llm := &inference.LLM{
		Name:      "gemini-test",
		Kind:      inference.KindGeminiAPI,
		Model:     "gemini-1.5-flash",
		APIKeyRef: "test-key",
	}
	_, err := a.Infer(context.Background(), node, llm, inference.Request{Prompt: "Hi"})
	if err == nil {
		t.Fatal("expected error for 500")
	}
	if !inference.IsTransient(err) {
		t.Errorf("expected ErrTransient for 500, got: %v", err)
	}
}

// Credential tests — same contract as the Claude and OpenWebUI adapters:
// ${secret:name} is resolved by the dispatcher into a shallow copy; the
// adapter never transmits an unresolved reference. Gemini sends the key
// as the ?key= query parameter, so the 401 gate checks that parameter.
// The fakeSecret and testNode helpers live in openwebui_test.go (same
// package).

// Test 5 — the regression: a fake server that returns 401 unless ?key=
// carries exactly the resolved value, driven through the DISPATCHER with
// a ${secret:name} reference. Post-resolution the dispatcher hands the
// adapter the resolved value and the server gets 200.
func TestGemini_DispatcherSecretRefSendsResolvedKey(t *testing.T) {
	const stored = "RESOLVED-GEMINI-KEY-77"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != stored {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("API key not valid"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"pong"}]}}]}`))
	}))
	defer ts.Close()

	reg := inference.NewRegistry()
	if err := reg.Add(&inference.LLM{
		Name:         "gemini",
		Kind:         inference.KindGeminiAPI,
		Model:        "gemini-1.5-flash",
		APIKeyRef:    "${secret:gemini-key}",
		ComputeNodes: []string{"gemini-node"},
	}); err != nil {
		t.Fatalf("registry add: %v", err)
	}
	d := inference.NewDispatcher(reg, func(name string) (*compute.Node, error) {
		if name != "gemini-node" {
			return nil, compute.ErrNotFound
		}
		return &compute.Node{Name: "gemini-node", Kind: compute.KindOpenAICompat, Address: ts.URL}, nil
	})
	d.RegisterAdapter(&Gemini{})
	d.SetSecretsStore(&fakeSecret{byName: map[string]string{"gemini-key": stored}}, nil)

	resp, err := d.Call(context.Background(), "gemini", inference.Request{Prompt: "ping"})
	if err != nil {
		t.Fatalf("expected 200 with the resolved key, got error: %v", err)
	}
	if resp.Text != "pong" {
		t.Fatalf("unexpected reply: %q", resp.Text)
	}
	// The registry entry must still carry the reference, not the value.
	got, gerr := reg.Get("gemini")
	if gerr != nil || got == nil {
		t.Fatalf("registry get: %v", gerr)
	}
	if got.APIKeyRef != "${secret:gemini-key}" {
		t.Fatalf("registry entry must keep the ${secret: reference, got %q", got.APIKeyRef)
	}
}

// Test 6 — an unresolved ${secret: reference handed straight to the
// adapter (with no resolving callsite) must send ZERO requests and
// return a final error that names the LLM entry and never the secret
// value.
func TestGemini_UnresolvedRefSendsNoRequest(t *testing.T) {
	var count int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"must never arrive"}]}}]}`))
	}))
	defer ts.Close()

	a := &Gemini{}
	llm := &inference.LLM{
		Name:      "gemini-unresolved",
		Kind:      inference.KindGeminiAPI,
		Model:     "gemini-1.5-flash",
		APIKeyRef: "${secret:gemini-key-missing}",
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
	if !strings.Contains(err.Error(), "gemini-unresolved") {
		t.Fatalf("error must name the LLM entry: %v", err)
	}
	for _, forbidden := range []string{"gemini-key-missing", "${secret:"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("error must not leak the reference, got: %v", err)
		}
	}
}

// Test 7 — the literal-key path is unchanged: ?key= still carries the
// key on the wire as-is and the 401-gated server accepts it.
func TestGemini_LiteralKeyStillWorks(t *testing.T) {
	const literal = "AIza-literal-123"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != literal {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("API key not valid"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"pong"}]}}]}`))
	}))
	defer ts.Close()

	a := &Gemini{}
	llm := &inference.LLM{
		Name:      "gemini-literal",
		Kind:      inference.KindGeminiAPI,
		Model:     "gemini-1.5-flash",
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
