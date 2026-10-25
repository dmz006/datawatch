package inference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/compute"
)

// loadFakeOllama serves /api/ps and /api/generate. resident lists the models
// reported by /api/ps; a load request records its model and makes it show.
type loadFakeOllama struct {
	mu       sync.Mutex
	resident []string
	loaded   []string
}

func (f *loadFakeOllama) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/api/ps":
		var models []map[string]string
		for _, m := range f.resident {
			models = append(models, map[string]string{"name": m, "model": m})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"models": models})
	case "/api/generate":
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.loaded = append(f.loaded, body.Model)
		f.resident = append(f.resident, body.Model)
		_, _ = fmt.Fprint(w, `{"done":true}`)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestEnsureLoadsPinnedModel(t *testing.T) {
	f := &loadFakeOllama{}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	defer srv.Close()
	node := &compute.Node{Name: "alpha", Address: srv.URL, NodePinnedModel: "qwen3:8b"}
	loader := &ModelLoader{Wait: time.Second, Poll: time.Millisecond}
	var reasons []string
	err := loader.Ensure(context.Background(), node, "qwen3:27b", func(r string) { reasons = append(reasons, r) })
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if len(f.loaded) != 1 || f.loaded[0] != "qwen3:8b" {
		t.Fatalf("a pinned node must load its pin, not the request; loaded=%v", f.loaded)
	}
	if len(reasons) != 1 {
		t.Fatalf("want one wait reason while loading, got %v", reasons)
	}
}

func TestEnsureSkipsLoadWhenResident(t *testing.T) {
	f := &loadFakeOllama{resident: []string{"qwen3:27b"}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	defer srv.Close()
	node := &compute.Node{Name: "alpha", Address: srv.URL}
	loader := &ModelLoader{Wait: time.Second, Poll: time.Millisecond}
	err := loader.Ensure(context.Background(), node, "qwen3:27b", func(string) {
		t.Error("a resident model must not report a wait")
	})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if len(f.loaded) != 0 {
		t.Fatalf("a resident model must not be loaded again; loaded=%v", f.loaded)
	}
}

func TestEnsureUnreachableNodeIsNodeLoadError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	node := &compute.Node{Name: "alpha", Address: srv.URL}
	loader := &ModelLoader{Wait: time.Second, Poll: time.Millisecond}
	err := loader.Ensure(context.Background(), node, "qwen3:27b", nil)
	var nodeErr *NodeLoadError
	if !errors.As(err, &nodeErr) {
		t.Fatalf("want *NodeLoadError, got %v", err)
	}
	if nodeErr.Node != "alpha" || nodeErr.Model != "qwen3:27b" {
		t.Fatalf("error should name the node and model, got %+v", nodeErr)
	}
}
