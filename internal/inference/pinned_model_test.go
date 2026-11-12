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

// PlacementModel asks pinnedModel for an implicit request: a pin is answered
// with the pin whatever allow_model_swap says, an unpinned node with the model.
func TestPlacementModelNeverReplacesAPin(t *testing.T) {
	cases := []struct {
		name string
		node *compute.Node
		want string
	}{
		{"nil node", nil, "qwen3:8b"},
		{"no pin", &compute.Node{Name: "a"}, "qwen3:8b"},
		{"pinned to another model", &compute.Node{Name: "a", NodePinnedModel: "other:1b"}, "other:1b"},
		{"pinned to another model with swap allowed", &compute.Node{Name: "a", NodePinnedModel: "other:1b", AllowModelSwap: true}, "other:1b"},
		{"pinned to this model", &compute.Node{Name: "a", NodePinnedModel: "qwen3:8b"}, "qwen3:8b"},
	}
	for _, c := range cases {
		got, err := PlacementModel(c.node, "qwen3:8b")
		if err != nil || got != c.want {
			t.Errorf("%s: PlacementModel = %q, %v; want %q", c.name, got, err, c.want)
		}
	}
}

// tagsFake is an Ollama node standing behind an address: it answers GET
// /api/tags with a fixed model listing and records every request it sees.
// That lets a test hold the served-model read against the node's own
// listing — the same /api/tags listing GET /api/compute/nodes/{name}/models
// returns (internal/server/compute.go, ProbeNodeModels reads it for kind
// "ollama").
type tagsFake struct {
	srv     *httptest.Server
	listing []string
	mu      sync.Mutex
	paths   []string
}

func newTagsFake(t *testing.T, listing ...string) *tagsFake {
	t.Helper()
	f := &tagsFake{listing: listing}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.paths = append(f.paths, r.Method+" "+r.URL.Path)
		f.mu.Unlock()
		if r.Method != http.MethodGet || r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		models := make([]map[string]string, 0, len(f.listing))
		for _, n := range f.listing {
			models = append(models, map[string]string{"name": n})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"models": models})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *tagsFake) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paths...)
}

// TestServedModelReadAgreesWithNodeTagsListing locks the invariant the
// node-models endpoint and placement's served-model read share: a node serves
// exactly the models its GET /api/tags listing reports. The compute API
// returns that listing; the served check this package runs while placement
// makes a model resident (ModelLoader.Ensure, via nodeHasModel) must read
// every model the listing names as served, every model it does not name as
// missing, and it must consult /api/tags itself — the same source — and no
// other endpoint.
func TestServedModelReadAgreesWithNodeTagsListing(t *testing.T) {
	fake := newTagsFake(t, "qwen3.8-64k", "llama3:70b", "qwen3.8:27b")
	ctx := context.Background()

	// Every model the node's own listing reports must read as served.
	for _, m := range []string{"qwen3.8-64k", "llama3:70b", "qwen3.8:27b"} {
		if got, err := nodeHasModel(ctx, http.DefaultClient, fake.srv.URL, m); err != nil {
			t.Fatalf("nodeHasModel(%q): %v", m, err)
		} else if !got {
			t.Errorf("nodeHasModel(%q) = false, want served: it is in the node's /api/tags listing", m)
		}
	}

	// A model the listing does not name must read as missing — including the
	// bare base of a non-latest tag: "qwen3.8" is not "qwen3.8:27b".
	for _, m := range []string{"deepseek:16b", "qwen3.8", "qwen3.8-64k:999b"} {
		if got, err := nodeHasModel(ctx, http.DefaultClient, fake.srv.URL, m); err != nil {
			t.Fatalf("nodeHasModel(%q): %v", m, err)
		} else if got {
			t.Errorf("nodeHasModel(%q) = true, want missing: it is not in the node's /api/tags listing", m)
		}
	}

	// Every read consulted the node's /api/tags listing (the source the
	// node-models endpoint reads) and no other endpoint.
	for _, p := range fake.seen() {
		if p != "GET /api/tags" {
			t.Errorf("served-model read requested %q, want GET /api/tags", p)
		}
	}
}

// TestServedModelReadMatchesLatestAlias locks the name matching the served
// check and the node listing share: Ollama reports a latest-tag model under
// its bare name, so a request for "name:latest" must match the listed
// "name" — and a different tag is a different model.
func TestServedModelReadMatchesLatestAlias(t *testing.T) {
	fake := newTagsFake(t, "qwen3.8-64k")
	ctx := context.Background()

	if got, err := nodeHasModel(ctx, http.DefaultClient, fake.srv.URL, "qwen3.8-64k"); err != nil || !got {
		t.Errorf("nodeHasModel(qwen3.8-64k) = %v, %v; want served", got, err)
	}
	if got, err := nodeHasModel(ctx, http.DefaultClient, fake.srv.URL, "qwen3.8-64k:latest"); err != nil || !got {
		t.Errorf("nodeHasModel(qwen3.8-64k:latest) = %v, %v; want served: the node lists it as qwen3.8-64k", got, err)
	}
	if got, err := nodeHasModel(ctx, http.DefaultClient, fake.srv.URL, "qwen3.8-64k:15b"); err != nil || got {
		t.Errorf("nodeHasModel(qwen3.8-64k:15b) = %v, %v; want missing: a different tag is a different model", got, err)
	}
}

// TestExplicitOverrideRefusalIsPinRefusedError locks the refusal's type and
// fields, not just its text: placement must surface a *PinRefusedError naming
// the node, its current pin and the requested model — the operator-facing
// "set allow_model_swap" guidance is built from this error alone.
func TestExplicitOverrideRefusalIsPinRefusedError(t *testing.T) {
	fake := newFakeOllama(t)
	r := newPinTestResolver(t, pinnedNode("gpu1", fake.srv.URL, pinnedTag, false))

	_, err := r.ResolveModel(context.Background(), "gpu1", backendDef, "deepseek:16b")
	if err == nil {
		t.Fatal("ResolveModel: want refusal, got nil")
	}
	var refused *PinRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("err type = %T (%v), want *PinRefusedError", err, err)
	}
	if refused.Node != "gpu1" || refused.Pinned != pinnedTag || refused.Requested != "deepseek:16b" {
		t.Errorf("refusal fields = node %q / pinned %q / requested %q, want gpu1 / %s / deepseek:16b",
			refused.Node, refused.Pinned, refused.Requested, pinnedTag)
	}
	msg, ok := PinRefusalMessage(err)
	if !ok {
		t.Fatal("PinRefusalMessage ok = false, want true")
	}
	if msg != refused.Error() {
		t.Errorf("PinRefusalMessage = %q, want the PinRefusedError's own text %q", msg, refused.Error())
	}
	for _, want := range []string{"refused", "gpu1", pinnedTag, "deepseek:16b", "allow_model_swap"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal text %q must name %q", msg, want)
		}
	}
}
