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

	"github.com/dmz006/datawatch/internal/compute"
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

// Test 1 — the regression: a fake server returning 401 unless x-api-key
// is exactly the resolved value, driven end-to-end through the
// DISPATCHER with a ${secret:name} reference in the registry entry.
//
// KindClaude is a cloud kind — the dispatcher passes a nil node and the
// adapter targets the package-level anthropicEndpoint. The test
// overrides that one variable with the httptest URL (restored on
// cleanup) so the wire call actually lands on the fake server. No other
// production behavior changes.
func TestClaude_ResolvedSecretRefThroughDispatcher(t *testing.T) {
	const stored = "RESOLVED-ANTHROPIC-KEY-77"
	ts := claudeServer(func(got string) (int, string) {
		if got != stored {
			return http.StatusUnauthorized, "invalid x-api-key"
		}
		return http.StatusOK, ""
	})
	defer ts.Close()

	savedEndpoint := anthropicEndpoint
	anthropicEndpoint = ts.URL
	t.Cleanup(func() { anthropicEndpoint = savedEndpoint })

	reg := inference.NewRegistry()
	if err := reg.Add(&inference.LLM{
		Name:      "claude",
		Kind:      inference.KindClaude,
		Model:     "claude-opus-4-1",
		APIKeyRef: "${secret:anthropic-key}",
	}); err != nil {
		t.Fatalf("registry add: %v", err)
	}
	// Cloud kinds never consult the compute-node lookup (see
	// dispatch.go isCloudKind branch in Call). Return a hard error if
	// something is accidentally routed here, so a regression surfaces
	// loudly instead of a silent nil-node path.
	d := inference.NewDispatcher(reg, func(string) (*compute.Node, error) {
		return nil, compute.ErrNotFound
	})
	d.RegisterAdapter(&Claude{})
	d.SetSecretsStore(&fakeSecret{byName: map[string]string{"anthropic-key": stored}}, nil)

	resp, err := d.Call(context.Background(), "claude", inference.Request{Prompt: "ping"})
	if err != nil {
		t.Fatalf("expected 200 with the resolved key, got error: %v", err)
	}
	if resp.Text != "pong" {
		t.Fatalf("unexpected reply: %q", resp.Text)
	}
	// The registry entry must still carry the reference, not the resolved
	// value: the dispatcher's resolution step must not mutate the entry.
	got, gerr := reg.Get("claude")
	if gerr != nil || got == nil {
		t.Fatalf("registry get: %v", gerr)
	}
	if got.APIKeyRef != "${secret:anthropic-key}" {
		t.Fatalf("registry entry must keep the ${secret: reference, got %q", got.APIKeyRef)
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
