package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
)

// A lease carries the provider-qualified model, but a real ollama node knows
// the model only by its own name and answers 404 to the qualified one. The
// loader must ask the node for the bare name.
func TestNodeLoaderAsksTheNodeForTheModelWithoutItsProvider(t *testing.T) {
	var mu sync.Mutex
	var loaded string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ps":
			mu.Lock()
			name := loaded
			mu.Unlock()
			models := []map[string]string{}
			if name != "" {
				models = append(models, map[string]string{"name": name, "model": name})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"models": models})
		case "/api/generate":
			var req struct {
				Model string `json:"model"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Model != "qwen3.8:27b" {
				http.Error(w, `{"error":"model not found"}`, http.StatusNotFound)
				return
			}
			mu.Lock()
			loaded = req.Model
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"done": true})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	creg, err := compute.NewRegistry(filepath.Join(t.TempDir(), "nodes.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := creg.Add(&compute.Node{Name: "node-a", Kind: compute.KindOllama, Address: srv.URL}); err != nil {
		t.Fatal(err)
	}
	load := nodeLoader(func() *compute.Registry { return creg })

	if err := load(context.Background(), "node-a", "ollama/qwen3.8:27b", nil); err != nil {
		t.Fatalf("load of a provider-qualified model: %v", err)
	}
	mu.Lock()
	got := loaded
	mu.Unlock()
	if got != "qwen3.8:27b" {
		t.Fatalf("node holds %q, want qwen3.8:27b", got)
	}
}

func TestNodeModelName(t *testing.T) {
	cases := map[string]string{
		"ollama/qwen3.8:27b":     "qwen3.8:27b",
		"qwen3.8:27b":            "qwen3.8:27b",
		"ollama/hf.co/acme/m:q4": "hf.co/acme/m:q4",
		"ollama/":                "ollama/",
		"":                       "",
	}
	for in, want := range cases {
		if got := nodeModelName(in); got != want {
			t.Errorf("nodeModelName(%q) = %q, want %q", in, got, want)
		}
	}
}
