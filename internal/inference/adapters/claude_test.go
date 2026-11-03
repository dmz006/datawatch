// Claude adapter credential tests.
//
// The bug under test: the adapter sends llm.APIKeyRef as the x-api-key
// header with no resolution, so a ${secret:name} reference handed to the
// adapter by a callsite that doesn't resolve it would be sent literally
// and the server would answer 401. The dispatcher (callsite) resolves the
// reference into a shallow copy of the LLM entry; the adapter is a
// defense-in-depth guard that refuses to send an unresolved reference
// instead of transmitting it.
//
// The fakeSecret and testNode helpers live in openwebui_test.go (same
// package).

package adapters

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dmz006/datawatch/internal/inference"
)

// claudeServer answers the Claude Messages API with the fixed text, but
// 401s first unless checkAuth accepts the x-api-key header value.
func claudeServer(checkAuth func(got string) (int, string)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if checkAuth != nil {
			code, body := checkAuth(r.Header.Get("x-api-key"))
			if code != http.StatusOK {
				w.WriteHeader(code)
				_, _ = w.Write([]byte(body))
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"pong"}]}`))
	}))
}

// Test 1 — the regression: a fake server that returns 401 unless
// x-api-key is exactly the resolved value, with the reference resolved
// the same way the dispatcher does (inference.ResolveAPIKey into a
// shallow copy of the entry — dispatcher.go callOnce) before the adapter
// call. Post-resolution the adapter hands the resolved value to the
// server, which gets 200.
//
// Note: claude is a cloud kind, so Dispatcher.Call passes a nil node and
// the adapter targets the hard-coded api.anthropic.com endpoint — an
// in-process httptest server is not reachable through the full
// dispatcher. dispatcher-level resolution (same resolver, registry entry
// unchanged, audit report) is covered by
// internal/inference/dispatcher_secret_test.go. This test drives the
// real adapter over the wire with that same resolution handoff and a
// key-gated fake server.
func TestClaude_ResolvedSecretRefSendsResolvedKey(t *testing.T) {
	const stored = "RESOLVED-ANTHROPIC-KEY-77"
	ts := claudeServer(func(got string) (int, string) {
		if got != stored {
			return http.StatusUnauthorized, "invalid x-api-key"
		}
		return http.StatusOK, ""
	})
	defer ts.Close()

	llm := &inference.LLM{
		Name:      "claude",
		Kind:      inference.KindClaude,
		Model:     "claude-opus-4-1",
		APIKeyRef: "${secret:anthropic-key}",
	}
	store := &fakeSecret{byName: map[string]string{"anthropic-key": stored}}

	// Exactly the dispatcher's callOnce resolution handoff.
	key, err := inference.ResolveAPIKey(llm, store)
	if err != nil {
		t.Fatalf("ResolveAPIKey: %v", err)
	}
	cp := *llm
	cp.APIKeyRef = key

	resp, err := (&Claude{}).Infer(context.Background(), testNode(ts.URL), &cp, inference.Request{Prompt: "ping"})
	if err != nil {
		t.Fatalf("expected 200 with the resolved key, got error: %v", err)
	}
	if resp.Text != "pong" {
		t.Fatalf("unexpected reply: %q", resp.Text)
	}
	// The registry entry must still carry the reference, not the value.
	if llm.APIKeyRef != "${secret:anthropic-key}" {
		t.Fatalf("registry entry must keep the ${secret: reference, got %q", llm.APIKeyRef)
	}
}

// Test 2 — an unresolved ${secret: reference handed straight to the
// adapter (with no resolving callsite) must send ZERO requests and
// return a final error that names the LLM entry and never the secret
// value.
func TestClaude_UnresolvedRefSendsNoRequest(t *testing.T) {
	var count int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"must never arrive"}]}`))
	}))
	defer ts.Close()

	a := &Claude{}
	llm := &inference.LLM{
		Name:      "claude-unresolved",
		Kind:      inference.KindClaude,
		Model:     "claude-opus-4-1",
		APIKeyRef: "${secret:anthropic-key-missing}",
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
	if !strings.Contains(err.Error(), "claude-unresolved") {
		t.Fatalf("error must name the LLM entry: %v", err)
	}
	for _, forbidden := range []string{"anthropic-key-missing", "${secret:"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("error must not leak the reference, got: %v", err)
		}
	}
}

// Test 3 — the literal-key path is unchanged: the key still goes on the
// wire as-is and the 401-gated server accepts it.
func TestClaude_LiteralKeyStillWorks(t *testing.T) {
	const literal = "sk-ant-literal-123"
	ts := claudeServer(func(got string) (int, string) {
		if got != literal {
			return http.StatusUnauthorized, "invalid x-api-key"
		}
		return http.StatusOK, ""
	})
	defer ts.Close()

	a := &Claude{}
	llm := &inference.LLM{
		Name:      "claude-literal",
		Kind:      inference.KindClaude,
		Model:     "claude-opus-4-1",
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

// Test 4 — a server 5xx is still surfaced as a transient
// (failover-eligible) error.
func TestClaude_Server500IsTransient(t *testing.T) {
	var count int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server exploded"))
	}))
	defer ts.Close()

	a := &Claude{}
	llm := &inference.LLM{
		Name:      "claude-500",
		Kind:      inference.KindClaude,
		Model:     "claude-opus-4-1",
		APIKeyRef: "whatever",
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
