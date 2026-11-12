package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
)

// ProbeNodeModels is the one source of a node's served models: it reads
// /api/tags for ollama, /api/v1/models for openwebui, and reports a note
// instead of a list when there is nothing to probe.
func TestProbeNodeModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_, _ = fmt.Fprint(w, `{"models":[{"name":"qwen3:8b"},{"name":""},{"name":"llama3:70b"}]}`)
		case "/api/v1/models":
			_, _ = fmt.Fprint(w, `{"data":[{"id":"gpt-x"}]}`)
		default:
			http.Error(w, "nope", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	node := &compute.Node{Name: "n", Address: srv.URL + "/"}

	models, note, err := ProbeNodeModels(ctx, node, "")
	if err != nil || note != "" || !reflect.DeepEqual(models, []string{"qwen3:8b", "llama3:70b"}) {
		t.Fatalf("ollama default kind = %v %q %v", models, note, err)
	}
	if models, _, err := ProbeNodeModels(ctx, node, "opencode"); err != nil || len(models) != 2 {
		t.Fatalf("opencode reads /api/tags too, got %v %v", models, err)
	}
	if models, _, err := ProbeNodeModels(ctx, node, "openwebui"); err != nil || !reflect.DeepEqual(models, []string{"gpt-x"}) {
		t.Fatalf("openwebui = %v %v", models, err)
	}
	if models, note, err := ProbeNodeModels(ctx, node, "claude-code"); err != nil || len(models) != 0 || note != "kind has no model probe" {
		t.Fatalf("a kind with no probe = %v %q %v", models, note, err)
	}
	if _, note, err := ProbeNodeModels(ctx, &compute.Node{Name: "n"}, "ollama"); err != nil || note != "node has no address" {
		t.Fatalf("no address = %q %v", note, err)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", http.StatusBadGateway) }))
	defer bad.Close()
	if _, _, err := ProbeNodeModels(ctx, &compute.Node{Name: "n", Address: bad.URL}, "ollama"); err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("a non-200 reply is a probe failure, got %v", err)
	}
	junk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, "not json") }))
	defer junk.Close()
	if models, note, err := ProbeNodeModels(ctx, &compute.Node{Name: "n", Address: junk.URL}, "ollama"); err != nil || len(models) != 0 || note != "could not parse probe response" {
		t.Fatalf("an unparsable reply = %v %q %v", models, note, err)
	}
	if _, _, err := ProbeNodeModels(ctx, &compute.Node{Name: "n", Address: "http://127.0.0.1:1"}, "ollama"); err == nil {
		t.Fatal("an unreachable node is a probe failure")
	}
}
