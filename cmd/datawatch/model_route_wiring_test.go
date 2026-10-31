package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
)

// fakeOllamaNode serves a /api/tags list of the given models and counts reads.
// A nil model list answers HTTP 500, so the node's models cannot be read.
func fakeOllamaNode(t *testing.T, hits *int32, models ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		if hits != nil {
			atomic.AddInt32(hits, 1)
		}
		if models == nil {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		var b strings.Builder
		b.WriteString(`{"models":[`)
		for i, m := range models {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(`{"name":"` + m + `"}`)
		}
		b.WriteString(`]}`)
		_, _ = w.Write([]byte(b.String()))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newRouteWiringFixture builds fake inference and compute registries over
// httptest nodes: alpha serves qwen3.8:27b, beta serves only llama3:8b, gamma
// serves the model but is not one of the entry's nodes, and off is disabled.
func newRouteWiringFixture(t *testing.T) func(ctx context.Context, backend, model, node string) (applicable, refused, nodeUnknown bool, msg string) {
	t.Helper()
	alpha := fakeOllamaNode(t, nil, "qwen3.8:27b")
	beta := fakeOllamaNode(t, nil, "llama3:8b")
	gamma := fakeOllamaNode(t, nil, "qwen3.8:27b")
	off := fakeOllamaNode(t, nil, "qwen3.8:27b")

	ireg := inference.NewRegistry()
	if err := ireg.Add(&inference.LLM{Name: "ollama", Kind: "ollama", ComputeNodes: []string{"alpha", "beta", "off"}}); err != nil {
		t.Fatal(err)
	}
	creg, err := compute.NewRegistry(t.TempDir() + "/nodes.json")
	if err != nil {
		t.Fatal(err)
	}
	for name, srv := range map[string]*httptest.Server{"alpha": alpha, "beta": beta, "gamma": gamma, "off": off} {
		if err := creg.Add(&compute.Node{Name: name, Kind: compute.KindOllama, Address: srv.URL, Disabled: name == "off"}); err != nil {
			t.Fatalf("add node %s: %v", name, err)
		}
	}
	cfg := &config.Config{}
	live := func() *config.Config { return cfg }
	cache := newServedModelsCache(live)
	fn := newModelHostCheck(live, func() *inference.Registry { return ireg }, func() *compute.Registry { return creg }, cache)
	run := func(ctx context.Context, backend, model, node string) (bool, bool, bool, string) {
		v := fn(ctx, backend, model, node)
		return v.Applicable, v.Refused, v.NodeUnknown, v.Message
	}
	return run
}

func TestModelRouteWiring_BuildsCheckFromRegistries(t *testing.T) {
	check := newRouteWiringFixture(t)
	ctx := context.Background()

	t.Run("a node serves the model", func(t *testing.T) {
		app, refused, _, msg := check(ctx, "pi", "ollama/qwen3.8:27b", "")
		if !app || refused || msg != "" {
			t.Fatalf("applicable=%v refused=%v msg=%q, want applicable and accepted", app, refused, msg)
		}
	})
	t.Run("no node serves the model", func(t *testing.T) {
		app, refused, unknown, msg := check(ctx, "pi", "ollama/ghost:7b", "")
		if !app || !refused || unknown {
			t.Fatalf("applicable=%v refused=%v nodeUnknown=%v, want applicable and refused", app, refused, unknown)
		}
		for _, want := range []string{"ollama/ghost:7b", "alpha", "beta"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("message %q does not name %q", msg, want)
			}
		}
		// gamma is not in the entry's compute_nodes and off is disabled, so
		// neither was checked.
		for _, not := range []string{"gamma", "off"} {
			if strings.Contains(msg, not) {
				t.Fatalf("message %q names %q, which is not checked", msg, not)
			}
		}
	})
	t.Run("any one node serving the model is enough", func(t *testing.T) {
		app, refused, _, _ := check(ctx, "pi", "ollama/llama3:8b", "")
		if !app || refused {
			t.Fatalf("beta serves llama3:8b: applicable=%v refused=%v", app, refused)
		}
	})
	t.Run("a hosted backend is not applicable", func(t *testing.T) {
		app, refused, _, msg := check(ctx, "claude-code", "claude-opus-5-5", "")
		if app || refused || msg != "" {
			t.Fatalf("hosted backend: applicable=%v refused=%v msg=%q", app, refused, msg)
		}
		// Even a provider-prefixed name on a hosted backend is not refused.
		if app, refused, _, _ := check(ctx, "claude-code", "ollama/ghost:7b", ""); app || refused {
			t.Fatalf("hosted backend with a prefixed model: applicable=%v refused=%v", app, refused)
		}
	})
	t.Run("a model with no provider prefix is not applicable", func(t *testing.T) {
		if app, refused, _, _ := check(ctx, "pi", "qwen3.8:27b", ""); app || refused {
			t.Fatalf("applicable=%v refused=%v", app, refused)
		}
	})
	t.Run("no backend still checks a local engine model", func(t *testing.T) {
		app, refused, _, _ := check(ctx, "", "ollama/ghost:7b", "")
		if !app || !refused {
			t.Fatalf("applicable=%v refused=%v, want both", app, refused)
		}
		if app, refused, _, _ := check(ctx, "", "claude-opus-5-5", ""); app || refused {
			t.Fatalf("an unprefixed model with no backend: applicable=%v refused=%v", app, refused)
		}
	})
	t.Run("a node pin with no model checks only that the node is registered", func(t *testing.T) {
		if app, refused, unknown, _ := check(ctx, "pi", "", "nowhere"); !app || !refused || !unknown {
			t.Fatalf("unregistered node, no model: applicable=%v refused=%v nodeUnknown=%v", app, refused, unknown)
		}
		if app, refused, unknown, _ := check(ctx, "pi", "", "alpha"); app || refused || unknown {
			t.Fatalf("registered node, no model: applicable=%v refused=%v nodeUnknown=%v", app, refused, unknown)
		}
	})
	t.Run("a node that is not registered", func(t *testing.T) {
		app, refused, unknown, msg := check(ctx, "pi", "ollama/qwen3.8:27b", "nowhere")
		if !app || !refused || !unknown || !strings.Contains(msg, `"nowhere"`) {
			t.Fatalf("applicable=%v refused=%v nodeUnknown=%v msg=%q", app, refused, unknown, msg)
		}
	})
	t.Run("a node outside the entry", func(t *testing.T) {
		app, refused, unknown, msg := check(ctx, "pi", "ollama/qwen3.8:27b", "gamma")
		if !app || !refused || unknown {
			t.Fatalf("applicable=%v refused=%v nodeUnknown=%v", app, refused, unknown)
		}
		if !strings.Contains(msg, `"gamma"`) || !strings.Contains(msg, `"ollama"`) {
			t.Fatalf("message %q does not name the node and the entry", msg)
		}
	})
	t.Run("a named node is judged alone", func(t *testing.T) {
		if _, refused, _, msg := check(ctx, "pi", "ollama/qwen3.8:27b", "alpha"); refused {
			t.Fatalf("alpha serves the model but was refused: %s", msg)
		}
		_, refused, _, msg := check(ctx, "pi", "ollama/qwen3.8:27b", "beta")
		if !refused || !strings.Contains(msg, "beta") {
			t.Fatalf("beta does not serve the model: refused=%v msg=%q", refused, msg)
		}
	})
	t.Run("a hosted backend is not refused for a named node", func(t *testing.T) {
		if app, refused, _, _ := check(ctx, "claude-code", "claude-opus-5-5", "nowhere"); app || refused {
			t.Fatalf("applicable=%v refused=%v", app, refused)
		}
	})
}

func TestModelRouteWiring_UnknownNodePolicyDecidesUnreadableNodes(t *testing.T) {
	down := fakeOllamaNode(t, nil) // nil model list: HTTP 500
	ireg := inference.NewRegistry()
	if err := ireg.Add(&inference.LLM{Name: "ollama", Kind: "ollama", ComputeNodes: []string{"down"}}); err != nil {
		t.Fatal(err)
	}
	creg, err := compute.NewRegistry(t.TempDir() + "/nodes.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := creg.Add(&compute.Node{Name: "down", Kind: compute.KindOllama, Address: down.URL}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	live := func() *config.Config { return cfg }
	fn := newModelHostCheck(live, func() *inference.Registry { return ireg }, func() *compute.Registry { return creg }, newServedModelsCache(live))

	if v := fn(context.Background(), "pi", "ollama/qwen3.8:27b", ""); !v.Applicable || v.Refused {
		t.Fatalf("assume_serves (default): %+v", v)
	}
	cfg.Autonomous.ModelRouting.UnknownNodePolicy = config.ModelRoutingUnknownAssumeNot
	v := fn(context.Background(), "pi", "ollama/qwen3.8:27b", "")
	if !v.Applicable || !v.Refused || !strings.Contains(v.Message, "down") {
		t.Fatalf("assume_not: %+v", v)
	}
}

func TestModelRouteWiring_CacheAgeAndSettingsReadLive(t *testing.T) {
	var hits int32
	srv := fakeOllamaNode(t, &hits, "qwen3.8:27b")
	cfg := &config.Config{}
	live := func() *config.Config { return cfg }
	cache := newServedModelsCache(live)
	node := &compute.Node{Name: "n", Kind: compute.KindOllama, Address: srv.URL}
	ctx := context.Background()

	cache.GetAll(ctx, []*compute.Node{node}, false)
	cache.GetAll(ctx, []*compute.Node{node}, false)
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("reads with the default 300s cache = %d, want 1", got)
	}
	zero := 0
	cfg.Autonomous.ModelRouting.ServedModelsCacheSeconds = &zero
	cache.GetAll(ctx, []*compute.Node{node}, false)
	cache.GetAll(ctx, []*compute.Node{node}, false)
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Fatalf("reads after setting the cache age to 0 = %d, want 3", got)
	}

	// Settings: defaults, then explicit values.
	cfg2 := &config.Config{}
	def := modelRoutingSettings(func() *config.Config { return cfg2 })
	if def.RefusalMode != "refuse" || !def.AlertOnRefusal || !def.VerifierFollowsWorkerNode {
		t.Fatalf("default settings = %+v", def)
	}
	off := false
	cfg2.Autonomous.ModelRouting.RefusalMode = "warn"
	cfg2.Autonomous.ModelRouting.AlertOnRefusal = &off
	cfg2.Autonomous.ModelRouting.VerifierFollowsWorkerNode = &off
	got := modelRoutingSettings(func() *config.Config { return cfg2 })
	if got.RefusalMode != "warn" || got.AlertOnRefusal || got.VerifierFollowsWorkerNode {
		t.Fatalf("explicit settings = %+v", got)
	}
	if s := modelRoutingSettings(nil); s.RefusalMode != "refuse" || !s.AlertOnRefusal {
		t.Fatalf("nil config settings = %+v", s)
	}
}
