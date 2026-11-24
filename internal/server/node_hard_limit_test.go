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

// hard_limit round trip: config file record, registry JSON on disk, REST. Off
// by default; PUT replaces the whole node, so a PUT without it turns it off.
func TestNodeHardLimitRoundTrip(t *testing.T) {
	var c config.ComputeNodeYAML
	if err := yaml.Unmarshal([]byte("name: gpubox\nkind: ollama\naddress: http://gpubox:11434\nhard_limit: true\n"), &c); err != nil {
		t.Fatal(err)
	}
	if !c.HardLimit {
		t.Fatal("config hard_limit not read")
	}
	path := filepath.Join(t.TempDir(), "nodes.json")
	reg, err := compute.NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	reg.Seed([]compute.Node{{Name: c.Name, Kind: compute.NodeKind(c.Kind), Address: c.Address, HardLimit: c.HardLimit}})
	reloaded, err := compute.NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := reloaded.Get("gpubox"); err != nil || !got.HardLimit {
		t.Fatalf("registry after reload: %+v, %v; want hard_limit true", got, err)
	}

	srv := &Server{computeReg: reloaded}
	put := func(body string) {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.handleComputeNodes(rec, httptest.NewRequest(http.MethodPut, "/api/compute/nodes/gpubox?probe=skip", strings.NewReader(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT status = %d: %s", rec.Code, rec.Body.String())
		}
	}
	read := func() map[string]json.RawMessage {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.handleComputeNodes(rec, httptest.NewRequest(http.MethodGet, "/api/compute/nodes/gpubox", nil))
		var node map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &node); err != nil {
			t.Fatalf("GET body: %v", err)
		}
		return node
	}
	put(`{"name":"gpubox","kind":"ollama","address":"http://gpubox:11434","hard_limit":true}`)
	if v := string(read()["hard_limit"]); v != "true" {
		t.Fatalf("REST hard_limit = %s, want true", v)
	}
	put(`{"name":"gpubox","kind":"ollama","address":"http://gpubox:11434"}`)
	if v, ok := read()["hard_limit"]; ok && string(v) != "false" {
		t.Fatalf("REST hard_limit after a PUT without it = %s, want off", v)
	}
	again, _ := compute.NewRegistry(path)
	if stored, _ := again.Get("gpubox"); stored == nil || stored.HardLimit {
		t.Fatalf("stored node = %+v, want hard_limit off", stored)
	}
}
