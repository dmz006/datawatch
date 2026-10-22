package inference

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
)

// fakeOllama is a real HTTP server standing in for an Ollama node. It
// records the "model" field of every POST /api/generate body it receives.
type fakeOllama struct {
	srv    *httptest.Server
	mu     sync.Mutex
	models []string
}

func newFakeOllama(t *testing.T) *fakeOllama {
	t.Helper()
	f := &fakeOllama{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/generate" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.models = append(f.models, body.Model)
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": "answer from " + body.Model,
			"done":     true,
		})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// received returns the model names the fake has seen, in arrival order.
func (f *fakeOllama) received() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.models...)
}

// newPinTestResolver registers nodes in a real compute registry on disk
// and returns a resolver that looks nodes up through it.
func newPinTestResolver(t *testing.T, nodes ...*compute.Node) *PinResolver {
	t.Helper()
	reg, err := compute.NewRegistry(t.TempDir() + "/compute/nodes.json")
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	for _, n := range nodes {
		if err := reg.Add(n); err != nil {
			t.Fatalf("Add %s: %v", n.Name, err)
		}
	}
	return NewPinResolver(reg.Get)
}

func pinnedNode(name, address, pinned string, allowSwap bool) *compute.Node {
	return &compute.Node{
		Name:            name,
		Kind:            compute.KindOllama,
		Address:         address,
		NodePinnedModel: pinned,
		AllowModelSwap:  allowSwap,
	}
}

const (
	pinnedTag  = "qwen3.8-64k:27b"
	backendDef = "qwen3.8:27b"
)

func TestPinResolver_BackendDefaultIsSubstitutedWithPin(t *testing.T) {
	fake := newFakeOllama(t)
	r := newPinTestResolver(t, pinnedNode("gpu1", fake.srv.URL, pinnedTag, false))
	ctx := context.Background()

	got, err := r.ResolveModel(ctx, "gpu1", backendDef, "")
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if got != pinnedTag {
		t.Fatalf("ResolveModel = %q, want pinned %q", got, pinnedTag)
	}

	model, text, err := r.Generate(ctx, "gpu1", backendDef, "", "hello")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if model != pinnedTag {
		t.Errorf("Generate model = %q, want %q", model, pinnedTag)
	}
	if text != "answer from "+pinnedTag {
		t.Errorf("Generate text = %q", text)
	}
	if want := []string{pinnedTag}; !reflect.DeepEqual(fake.received(), want) {
		t.Errorf("fake Ollama received %v, want %v", fake.received(), want)
	}
}

func TestPinResolver_EmptyRequestUsesPin(t *testing.T) {
	fake := newFakeOllama(t)
	r := newPinTestResolver(t, pinnedNode("gpu1", fake.srv.URL, pinnedTag, false))

	model, _, err := r.Generate(context.Background(), "gpu1", "", "", "hi")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if model != pinnedTag {
		t.Errorf("model = %q, want %q", model, pinnedTag)
	}
	if want := []string{pinnedTag}; !reflect.DeepEqual(fake.received(), want) {
		t.Errorf("fake Ollama received %v, want %v", fake.received(), want)
	}
}

func TestPinResolver_ExplicitOverrideRefusedWithoutSwap(t *testing.T) {
	fake := newFakeOllama(t)
	r := newPinTestResolver(t, pinnedNode("gpu1", fake.srv.URL, pinnedTag, false))
	ctx := context.Background()

	if _, err := r.ResolveModel(ctx, "gpu1", backendDef, backendDef); err == nil {
		t.Fatal("ResolveModel: want refusal, got nil error")
	} else {
		assertRefusal(t, err)
	}

	_, _, err := r.Generate(ctx, "gpu1", backendDef, backendDef, "hi")
	if err == nil {
		t.Fatal("Generate: want refusal, got nil error")
	}
	assertRefusal(t, err)
	if got := fake.received(); len(got) != 0 {
		t.Fatalf("fake Ollama received %v, want no request at all", got)
	}
}

// assertRefusal checks the refusal error names the node, both models
// and the setting that would allow the swap.
func assertRefusal(t *testing.T, err error) {
	t.Helper()
	for _, want := range []string{"refused", "gpu1", pinnedTag, backendDef, "allow_model_swap"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not mention %q", err, want)
		}
	}
}

func TestPinResolver_ExplicitOverrideAllowedWithSwap(t *testing.T) {
	fake := newFakeOllama(t)
	r := newPinTestResolver(t, pinnedNode("gpu1", fake.srv.URL, pinnedTag, true))
	ctx := context.Background()

	got, err := r.ResolveModel(ctx, "gpu1", backendDef, backendDef)
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if got != backendDef {
		t.Errorf("ResolveModel = %q, want override %q", got, backendDef)
	}

	model, _, err := r.Generate(ctx, "gpu1", backendDef, backendDef, "hi")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if model != backendDef {
		t.Errorf("Generate model = %q, want %q", model, backendDef)
	}
	if want := []string{backendDef}; !reflect.DeepEqual(fake.received(), want) {
		t.Errorf("fake Ollama received %v, want %v", fake.received(), want)
	}
}

func TestPinResolver_ExplicitRequestForPinnedModelRunsOnPin(t *testing.T) {
	fake := newFakeOllama(t)
	r := newPinTestResolver(t, pinnedNode("gpu1", fake.srv.URL, pinnedTag, false))

	model, _, err := r.Generate(context.Background(), "gpu1", backendDef, pinnedTag, "hi")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if model != pinnedTag {
		t.Errorf("model = %q, want %q", model, pinnedTag)
	}
	if want := []string{pinnedTag}; !reflect.DeepEqual(fake.received(), want) {
		t.Errorf("fake Ollama received %v, want %v", fake.received(), want)
	}
}

func TestPinResolver_UnconfiguredNodePassesThrough(t *testing.T) {
	fake := newFakeOllama(t)
	r := newPinTestResolver(t, pinnedNode("plain", fake.srv.URL, "", false))
	ctx := context.Background()

	got, err := r.ResolveModel(ctx, "plain", backendDef, "")
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if got != backendDef {
		t.Errorf("ResolveModel = %q, want pass-through %q", got, backendDef)
	}

	got, err = r.ResolveModel(ctx, "plain", backendDef, pinnedTag)
	if err != nil {
		t.Fatalf("ResolveModel with override: %v", err)
	}
	if got != pinnedTag {
		t.Errorf("ResolveModel with override = %q, want pass-through %q", got, pinnedTag)
	}

	model, _, err := r.Generate(ctx, "plain", backendDef, "", "hi")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if model != backendDef {
		t.Errorf("Generate model = %q, want %q", model, backendDef)
	}
	if want := []string{backendDef}; !reflect.DeepEqual(fake.received(), want) {
		t.Errorf("fake Ollama received %v, want %v", fake.received(), want)
	}
}

func TestPinResolver_UnknownNodePassesThrough(t *testing.T) {
	r := newPinTestResolver(t)
	ctx := context.Background()

	got, err := r.ResolveModel(ctx, "ghost", backendDef, "")
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if got != backendDef {
		t.Errorf("ResolveModel = %q, want pass-through %q", got, backendDef)
	}

	// Generate has no node to post to, so an unknown name is an error there.
	if _, _, err := r.Generate(ctx, "ghost", backendDef, "", "hi"); err == nil ||
		!strings.Contains(err.Error(), "not found") {
		t.Errorf("Generate on unknown node: err = %v, want not found", err)
	}
}

func TestPinResolver_LookupFailurePropagates(t *testing.T) {
	boom := errors.New("registry unavailable")
	r := NewPinResolver(func(string) (*compute.Node, error) { return nil, boom })

	_, err := r.ResolveModel(context.Background(), "gpu1", backendDef, "")
	if !errors.Is(err, boom) {
		t.Fatalf("ResolveModel err = %v, want wrapped %v", err, boom)
	}
	if _, _, err := r.Generate(context.Background(), "gpu1", backendDef, "", "hi"); !errors.Is(err, boom) {
		t.Fatalf("Generate err = %v, want wrapped %v", err, boom)
	}
}

func TestPinResolver_GenerateNoModel(t *testing.T) {
	fake := newFakeOllama(t)
	r := newPinTestResolver(t, pinnedNode("plain", fake.srv.URL, "", false))

	_, _, err := r.Generate(context.Background(), "plain", "", "", "hi")
	if err == nil || !strings.Contains(err.Error(), "no model") {
		t.Fatalf("Generate err = %v, want no-model error", err)
	}
	if got := fake.received(); len(got) != 0 {
		t.Fatalf("fake Ollama received %v, want none", got)
	}
}

func TestPinResolver_GenerateNoAddress(t *testing.T) {
	r := newPinTestResolver(t, pinnedNode("noaddr", "", pinnedTag, false))

	_, _, err := r.Generate(context.Background(), "noaddr", backendDef, "", "hi")
	if err == nil || !strings.Contains(err.Error(), "has no address") {
		t.Fatalf("Generate err = %v, want no-address error", err)
	}
}

func TestPinResolver_GenerateBadAddressURL(t *testing.T) {
	// An unclosed IPv6 literal makes url.Parse fail, so NewRequest rejects it.
	r := newPinTestResolver(t, pinnedNode("badurl", "http://[::1", pinnedTag, false))

	_, _, err := r.Generate(context.Background(), "badurl", backendDef, "", "hi")
	if err == nil || !strings.Contains(err.Error(), "build request") {
		t.Fatalf("Generate err = %v, want build-request error", err)
	}
}

func TestPinResolver_GenerateUnreachableNodeIsTransient(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	addr := dead.URL
	dead.Close()
	r := newPinTestResolver(t, pinnedNode("down", addr, pinnedTag, false))

	_, _, err := r.Generate(context.Background(), "down", backendDef, "", "hi")
	if !IsTransient(err) {
		t.Fatalf("Generate err = %v, want transient failover error", err)
	}
}

func TestPinResolver_GenerateServerErrorStatus(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		transient bool
	}{
		{"server error is transient", http.StatusInternalServerError, true},
		{"client error is final", http.StatusNotFound, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "model not loaded", tc.status)
			}))
			t.Cleanup(srv.Close)
			r := newPinTestResolver(t, pinnedNode("gpu1", srv.URL, pinnedTag, false))

			_, _, err := r.Generate(context.Background(), "gpu1", backendDef, "", "hi")
			if err == nil {
				t.Fatal("Generate: want error, got nil")
			}
			if IsTransient(err) != tc.transient {
				t.Errorf("IsTransient = %v, want %v (err %v)", IsTransient(err), tc.transient, err)
			}
			if !strings.Contains(err.Error(), "model not loaded") {
				t.Errorf("error %q does not carry the node body", err)
			}
		})
	}
}

func TestPinResolver_GenerateUndecodableBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	t.Cleanup(srv.Close)
	r := newPinTestResolver(t, pinnedNode("gpu1", srv.URL, pinnedTag, false))

	_, _, err := r.Generate(context.Background(), "gpu1", backendDef, "", "hi")
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("Generate err = %v, want decode error", err)
	}
}
