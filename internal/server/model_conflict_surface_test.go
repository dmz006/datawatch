package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
)

// newModelConflictServer seeds an in-memory config with a conflicting pair:
// the ollama default model and a planning model both run on gpu1.
func newModelConflictServer(t *testing.T) *Server {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Ollama.Host = "http://gpu1:11434"
	cfg.Ollama.Model = "qwen3.8:27b"
	cfg.Autonomous.PlanningModel = "ollama/qwen3.8-64k:27b"
	cfg.ComputeNodes = []config.ComputeNodeYAML{{Name: "gpu1", Kind: "ollama", Address: "http://gpu1:11434"}}
	return &Server{cfg: cfg, cfgPath: filepath.Join(t.TempDir(), "config.yaml")}
}

func assertOneGpu1Conflict(t *testing.T, where string, got []inference.ModelNodeConflict) {
	t.Helper()
	if len(got) != 1 {
		t.Fatalf("%s: want 1 model_node warning, got %d: %+v", where, len(got), got)
	}
	if got[0].Node != "gpu1" {
		t.Errorf("%s: Node = %q, want gpu1", where, got[0].Node)
	}
	if len(got[0].Models) != 2 {
		t.Errorf("%s: Models = %v, want two model names", where, got[0].Models)
	}
}

func TestModelNodeWarningsInConfigResponse(t *testing.T) {
	s := newModelConflictServer(t)
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d", rr.Code)
	}
	var got struct {
		ModelNodeWarnings []inference.ModelNodeConflict `json:"model_node_warnings"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET /api/config: %v", err)
	}
	assertOneGpu1Conflict(t, "GET /api/config", got.ModelNodeWarnings)
}

func TestModelNodeWarningsInCapacityResponse(t *testing.T) {
	s := newModelConflictServer(t)
	rr := httptest.NewRecorder()
	s.handleCapacity(rr, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/capacity: want 200, got %d", rr.Code)
	}
	var got struct {
		Warnings []inference.ModelNodeConflict `json:"warnings"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET /api/capacity: %v", err)
	}
	assertOneGpu1Conflict(t, "GET /api/capacity", got.Warnings)
}

// With no conflict in the config, neither surface carries the field.
func TestModelNodeWarningsOmittedWhenNoConflict(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Ollama.Host = "http://gpu1:11434"
	cfg.Ollama.Model = "qwen3.8:27b"
	s := &Server{cfg: cfg, cfgPath: filepath.Join(t.TempDir(), "config.yaml")}

	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var cfgBody map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &cfgBody); err != nil {
		t.Fatalf("decode GET /api/config: %v", err)
	}
	if _, ok := cfgBody["model_node_warnings"]; ok {
		t.Errorf("GET /api/config: model_node_warnings present with no conflict")
	}

	rr = httptest.NewRecorder()
	s.handleCapacity(rr, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
	var capBody map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &capBody); err != nil {
		t.Fatalf("decode GET /api/capacity: %v", err)
	}
	if _, ok := capBody["warnings"]; ok {
		t.Errorf("GET /api/capacity: warnings present with no conflict")
	}
}
