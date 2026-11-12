// OpenWebUI adapter credential tests.
//
// The bug under test: the adapter set Authorization to
// "Bearer " + llm.APIKeyRef with no resolution, so a
// ${secret:name} reference was sent literally and the server
// answered 401. The dispatcher (callsite) resolves the reference
// into a shallow copy of the LLM entry; the adapter is a
// defense-in-depth guard that refuses to send an unresolved
// reference instead of transmitting it.

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
	"github.com/dmz006/datawatch/internal/secrets"
)

// fakeSecret implements secrets.Store with a fixed name→value map.
type fakeSecret struct {
	byName map[string]string
}

func (f *fakeSecret) Get(name string) (secrets.Secret, error) {
	v, ok := f.byName[name]
	if !ok {
		return secrets.Secret{}, secrets.ErrSecretNotFound
	}
	return secrets.Secret{Name: name, Value: v}, nil
}

func (f *fakeSecret) List() ([]secrets.Secret, error) {
	out := make([]secrets.Secret, 0, len(f.byName))
	for n, v := range f.byName {
		out = append(out, secrets.Secret{Name: n, Value: v})
	}
	return out, nil
}

func (f *fakeSecret) Set(name, value string, tags []string, description string, scopes []string) error {
	if f.byName == nil {
		f.byName = map[string]string{}
	}
	f.byName[name] = value
	return nil
}

func (f *fakeSecret) Delete(name string) error {
	delete(f.byName, name)
	return nil
}

func (f *fakeSecret) Exists(name string) (bool, error) {
	_, ok := f.byName[name]
	return ok, nil
}

// chatServer returns an OpenAI-compatible chat completion with the fixed
// content; checkAuth (when non-nil) runs first and controls status.
func chatServer(content string, checkAuth func(got string) (int, string)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if checkAuth != nil {
			code, body := checkAuth(r.Header.Get("Authorization"))
			if code != http.StatusOK {
				w.WriteHeader(code)
				_, _ = w.Write([]byte(body))
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"` + content + `"}}]}`))
	}))
}

func testNode(url string) *compute.Node {
	return &compute.Node{Name: "ow-node", Kind: compute.KindOpenAICompat, Address: url}
}

// Test 1 — the regression: a fake server that returns 401 unless
// Authorization is exactly the resolved bearer value, driven through the
// DISPATCHER with a ${secret:name} reference. Pre-fix the adapter sent the
// literal reference and this got 401; post-fix the dispatcher hands the
// resolved value and the server gets 200.
func TestOpenWebUI_DispatcherSecretRefSendsResolvedKey(t *testing.T) {
	const stored = "RESOLVED-OWUI-KEY-77"
	want := "Bearer " + stored
	ts := chatServer("pong", func(got string) (int, string) {
		if got != want {
			return http.StatusUnauthorized, "bad credentials"
		}
		return http.StatusOK, ""
	})
	defer ts.Close()

	reg := inference.NewRegistry()
	if err := reg.Add(&inference.LLM{
		Name:         "owui",
		Kind:         inference.KindOpenWebUI,
		Model:        "local-model",
		APIKeyRef:    "${secret:owui-key}",
		ComputeNodes: []string{"ow-node"},
	}); err != nil {
		t.Fatalf("registry add: %v", err)
	}
	d := inference.NewDispatcher(reg, func(name string) (*compute.Node, error) {
		if name != "ow-node" {
			return nil, compute.ErrNotFound
		}
		return testNode(ts.URL), nil
	})
	d.RegisterAdapter(&OpenWebUI{})
	d.SetSecretsStore(&fakeSecret{byName: map[string]string{"owui-key": stored}}, nil)

	resp, err := d.Call(context.Background(), "owui", inference.Request{Prompt: "ping"})
	if err != nil {
		t.Fatalf("expected 200 with the resolved key, got error (bug: unresolved ${secret: sent verbatim) : %v", err)
	}
	if resp.Text != "pong" {
		t.Fatalf("unexpected reply: %q", resp.Text)
	}
	// The registry entry must still carry the reference, not the value.
	got, gerr := reg.Get("owui")
	if gerr != nil || got == nil {
		t.Fatalf("registry get: %v", gerr)
	}
	if got.APIKeyRef != "${secret:owui-key}" {
		t.Fatalf("registry entry must keep the ${secret: reference, got %q", got.APIKeyRef)
	}
}

// Test 2 — an unresolved ${secret: reference handed straight to the adapter
// (with no resolving callsite) must send ZERO requests and return a final
// error that names the LLM entry and never the reference contents.
func TestOpenWebUI_UnresolvedRefSendsNoRequest(t *testing.T) {
	var count int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"must never arrive"}}]}`))
	}))
	defer ts.Close()

	a := &OpenWebUI{}
	llm := &inference.LLM{
		Name:      "owui-unresolved",
		Kind:      inference.KindOpenWebUI,
		Model:     "local-model",
		APIKeyRef: "${secret:owui-key-missing}",
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
	if !strings.Contains(err.Error(), "owui-unresolved") {
		t.Fatalf("error must name the LLM entry: %v", err)
	}
	for _, forbidden := range []string{"owui-key-missing", "${secret:", "Bearer"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("error must not leak the reference or a bearer token, got: %v", err)
		}
	}
}

// Test 3 — the literal-key path is unchanged: the key still goes on the wire
// as-is and the call succeeds.
func TestOpenWebUI_LiteralKeyStillWorks(t *testing.T) {
	const literal = "sk-live-literal-123"
	want := "Bearer " + literal
	ts := chatServer("pong", func(got string) (int, string) {
		if got != want {
			return http.StatusUnauthorized, "bad credentials"
		}
		return http.StatusOK, ""
	})
	defer ts.Close()

	a := &OpenWebUI{}
	llm := &inference.LLM{
		Name:      "owui-literal",
		Kind:      inference.KindOpenWebUI,
		Model:     "local-model",
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

// Test 4 — a server 5xx is still surfaced as a transient (failover-eligible)
// error.
func TestOpenWebUI_Server500IsTransient(t *testing.T) {
	var count int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream exploded"))
	}))
	defer ts.Close()

	a := &OpenWebUI{}
	llm := &inference.LLM{
		Name:  "owui-500",
		Kind:  inference.KindOpenWebUI,
		Model: "local-model",
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

// Test 5 (supporting) — the same 401-vs-200 boundary driven straight through
// the adapter: the adapter must send whatever APIKeyRef it was given verbatim
// for the literal path (no mutation at the adapter layer).
func TestOpenWebAdapter_WrongLiteralKeyGets401(t *testing.T) {
	ts := chatServer("pong", func(got string) (int, string) {
		if got != "Bearer expected-key" {
			return http.StatusUnauthorized, "bad credentials"
		}
		return http.StatusOK, ""
	})
	defer ts.Close()

	a := &OpenWebUI{}
	llm := &inference.LLM{
		Name:      "owui-401",
		Kind:      inference.KindOpenWebUI,
		Model:     "local-model",
		APIKeyRef: "wrong-key",
	}
	_, err := a.Infer(context.Background(), testNode(ts.URL), llm, inference.Request{Prompt: "ping"})
	if err == nil {
		t.Fatal("expected a 401 error, got nil")
	}
	if inference.IsTransient(err) {
		t.Fatalf("a 401 is final (credentials problem), not transient: %v", err)
	}
}
