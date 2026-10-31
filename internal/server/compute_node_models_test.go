package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
)

func newComputeNodeModelsServer(t *testing.T, addr string) *Server {
	t.Helper()
	reg, err := compute.NewRegistry(filepath.Join(t.TempDir(), "nodes.json"))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if err := reg.Add(&compute.Node{Name: "gpu-a", Kind: compute.KindOllama, Address: addr}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	return &Server{computeReg: reg}
}

func getComputeNodeModels(srv *Server, query string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	srv.handleComputeNodeModels(w, httptest.NewRequest(http.MethodGet, "/api/compute/nodes/gpu-a/models"+query, nil), "gpu-a")
	return w
}

func TestComputeNodeModels_ReadsOllamaAndOpenWebUI(t *testing.T) {
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[{"name":"llama3:70b"},{"name":"qwen3:8b"}]}`))
		case "/api/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer node.Close()
	srv := newComputeNodeModelsServer(t, node.URL)

	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"", []string{"llama3:70b", "qwen3:8b"}},
		{"?kind=opencode", []string{"llama3:70b", "qwen3:8b"}},
		{"?kind=openwebui", []string{"m1"}},
	} {
		w := getComputeNodeModels(srv, tc.query)
		if w.Code != http.StatusOK {
			t.Fatalf("%q: status %d %s", tc.query, w.Code, w.Body.String())
		}
		var got struct {
			Models []string `json:"models"`
			Node   string   `json:"node"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Node != "gpu-a" || len(got.Models) != len(tc.want) || got.Models[0] != tc.want[0] {
			t.Fatalf("%q: got %+v want %v", tc.query, got, tc.want)
		}
	}
}

func TestComputeNodeModels_StatusCodes(t *testing.T) {
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer node.Close()
	srv := newComputeNodeModelsServer(t, node.URL)
	if w := getComputeNodeModels(srv, ""); w.Code != http.StatusBadGateway {
		t.Fatalf("upstream 500: status %d, want 502", w.Code)
	}
	if w := getComputeNodeModels(srv, "?kind=claude-code"); w.Code != http.StatusOK {
		t.Fatalf("kind without a model probe: status %d, want 200", w.Code)
	}
	w := httptest.NewRecorder()
	srv.handleComputeNodeModels(w, httptest.NewRequest(http.MethodGet, "/x", nil), "missing")
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown node: status %d, want 404", w.Code)
	}
}
