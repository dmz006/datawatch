// Configuration round-trip for autonomous.max_engines_per_node (how many
// local-model engine sessions one compute node may run at once) on the
// REST config surfaces: GET/PUT /api/config and GET/PUT
// /api/autonomous/config. The MCP surface (autonomous_config_set) is
// covered in internal/mcp, because internal/mcp imports this package and
// cannot be imported back from here.

package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestMaxEnginesPerNode_GetConfigDefaultIsOne(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	if got := string(getAutonomousConfigMap(t, s)["max_engines_per_node"]); got != "1" {
		t.Fatalf("GET /api/config autonomous.max_engines_per_node = %q, want 1", got)
	}
}

func TestMaxEnginesPerNode_PutConfigRoundTrip(t *testing.T) {
	s, mgr, cfgPath := newMaxConcurrentTasksServer(t)
	resp := putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.max_engines_per_node": 2})
	if _, ok := resp["deprecated_fields"]; ok {
		t.Errorf("deprecated_fields present for a non-deprecated key: %v", resp)
	}
	if got := string(getAutonomousConfigMap(t, s)["max_engines_per_node"]); got != "2" {
		t.Fatalf("GET after PUT = %q, want 2", got)
	}
	if got := mgr.Config().MaxEnginesPerNode; got != 2 {
		t.Fatalf("live manager MaxEnginesPerNode = %d, want 2", got)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	if !strings.Contains(string(raw), "max_engines_per_node: 2") {
		t.Fatalf("config.yaml lacks max_engines_per_node: 2:\n%s", raw)
	}
}

func TestMaxEnginesPerNode_PutConfigZeroClampsToOne(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.max_engines_per_node": 2})

	// Unlike max_concurrent_tasks (where 0 is skipped), 0 is stored as 1,
	// the same clamp config load applies.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.max_engines_per_node": 0})
	if s.cfg.Autonomous.MaxEnginesPerNode != 1 {
		t.Fatalf("s.cfg MaxEnginesPerNode = %d, want clamp to 1", s.cfg.Autonomous.MaxEnginesPerNode)
	}
	if got := mgr.Config().MaxEnginesPerNode; got != 1 {
		t.Fatalf("live manager MaxEnginesPerNode = %d, want clamp to 1", got)
	}
	if got := string(getAutonomousConfigMap(t, s)["max_engines_per_node"]); got != "1" {
		t.Fatalf("GET after 0 = %q, want 1", got)
	}
}

func TestMaxEnginesPerNode_PutAutonomousConfigRoundTripAndClamp(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)

	putJSON(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"max_engines_per_node": 2})
	if got := mgr.Config().MaxEnginesPerNode; got != 2 {
		t.Fatalf("live manager MaxEnginesPerNode = %d, want 2", got)
	}
	if got := s.cfg.Autonomous.MaxEnginesPerNode; got != 2 {
		t.Fatalf("s.cfg.Autonomous.MaxEnginesPerNode = %d, want 2 (GH#180 sync-back)", got)
	}
	rr := httptest.NewRecorder()
	s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodGet, "/api/autonomous/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/autonomous/config: want 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"max_engines_per_node":2`) {
		t.Fatalf("GET /api/autonomous/config lacks max_engines_per_node:2: %s", rr.Body.String())
	}

	putJSON(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"max_engines_per_node": 0})
	if got := mgr.Config().MaxEnginesPerNode; got != 1 {
		t.Fatalf("live manager MaxEnginesPerNode = %d after 0, want clamp to 1", got)
	}
	if got := s.cfg.Autonomous.MaxEnginesPerNode; got != 1 {
		t.Fatalf("s.cfg.Autonomous.MaxEnginesPerNode = %d after 0, want clamp to 1", got)
	}
}
