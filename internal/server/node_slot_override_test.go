package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
)

// TestNodeSlotOverrideRoundTrip carries session_slots through the three places
// an operator sets it: the config file record, the registry JSON on disk, and
// the REST node endpoints. The config step mirrors the seed loop in
// cmd/datawatch/main.go.
func TestNodeSlotOverrideRoundTrip(t *testing.T) {
	// Config file: the field is read from the node record.
	const file = "name: gpubox\nkind: ollama\naddress: http://gpubox:11434\nsession_slots: 3\n"
	var c config.ComputeNodeYAML
	if err := yaml.Unmarshal([]byte(file), &c); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if c.SessionSlots != 3 {
		t.Fatalf("config SessionSlots = %d, want 3", c.SessionSlots)
	}

	// Registry: the seeded value is persisted and survives a reload.
	path := filepath.Join(t.TempDir(), "nodes.json")
	reg, err := compute.NewRegistry(path)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	reg.Seed([]compute.Node{{
		Name:         c.Name,
		Kind:         compute.NodeKind(c.Kind),
		Address:      c.Address,
		SessionSlots: c.SessionSlots,
	}})
	reloaded, err := compute.NewRegistry(path)
	if err != nil {
		t.Fatalf("reopen registry: %v", err)
	}
	got, err := reloaded.Get("gpubox")
	if err != nil {
		t.Fatalf("Get after reload: %v", err)
	}
	if got.SessionSlots != 3 {
		t.Fatalf("registry SessionSlots after reload = %d, want 3", got.SessionSlots)
	}

	// REST: PUT replaces the override, GET returns it.
	srv := &Server{computeReg: reloaded}
	const body = `{"name":"gpubox","kind":"ollama","address":"http://gpubox:11434","session_slots":5}`
	rec := httptest.NewRecorder()
	srv.handleComputeNodes(rec, httptest.NewRequest(http.MethodPut, "/api/compute/nodes/gpubox?probe=skip", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	srv.handleComputeNodes(rec, httptest.NewRequest(http.MethodGet, "/api/compute/nodes/gpubox", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var node map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &node); err != nil {
		t.Fatalf("GET body is not JSON: %v", err)
	}
	if string(node["session_slots"]) != "5" {
		t.Errorf("REST session_slots = %s, want 5", node["session_slots"])
	}

	// The REST write also reaches the JSON on disk.
	again, err := compute.NewRegistry(path)
	if err != nil {
		t.Fatalf("reopen after PUT: %v", err)
	}
	stored, err := again.Get("gpubox")
	if err != nil {
		t.Fatalf("Get after PUT reload: %v", err)
	}
	if stored.SessionSlots != 5 {
		t.Errorf("registry SessionSlots after PUT = %d, want 5", stored.SessionSlots)
	}
}
