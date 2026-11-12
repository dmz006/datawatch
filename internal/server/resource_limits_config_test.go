// resource_limits.* config surface: PUT /api/config applies and validates the
// three memory ceiling keys, and GET /api/config reads them back.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func putResourceLimits(t *testing.T, s *Server, patch map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(patch)
	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, req)
	return rr
}

func getResourceLimits(t *testing.T, s *Server) map[string]float64 {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: %d", rr.Code)
	}
	var out struct {
		RL map[string]float64 `json:"resource_limits"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.RL
}

func TestResourceLimitsRoundTrip(t *testing.T) {
	cfg := &config.Config{ResourceLimits: config.DefaultResourceLimits()}
	s := &Server{cfg: cfg, cfgPath: t.TempDir() + "/config.yaml"}
	if got := getResourceLimits(t, s); got["memory_share_percent"] != 50 || got["memory_floor_mb"] != 2048 || got["memory_cap_mb"] != 16384 {
		t.Fatalf("defaults = %v", got)
	}
	rr := putResourceLimits(t, s, map[string]interface{}{
		"resource_limits.memory_share_percent": 30,
		"resource_limits.memory_floor_mb":      1024,
		"resource_limits.memory_cap_mb":        8192,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rr.Code, rr.Body.String())
	}
	got := getResourceLimits(t, s)
	if got["memory_share_percent"] != 30 || got["memory_floor_mb"] != 1024 || got["memory_cap_mb"] != 8192 {
		t.Errorf("read back = %v", got)
	}
}

func TestResourceLimitsValidation(t *testing.T) {
	cases := []struct {
		name  string
		patch map[string]interface{}
	}{
		{"share zero", map[string]interface{}{"resource_limits.memory_share_percent": 0}},
		{"share over 100", map[string]interface{}{"resource_limits.memory_share_percent": 101}},
		{"share not a number", map[string]interface{}{"resource_limits.memory_share_percent": "abc"}},
		{"floor above cap", map[string]interface{}{"resource_limits.memory_floor_mb": 9000, "resource_limits.memory_cap_mb": 8000}},
		{"floor above current cap", map[string]interface{}{"resource_limits.memory_floor_mb": 20000}},
		{"cap below current floor", map[string]interface{}{"resource_limits.memory_cap_mb": 1000}},
		{"floor negative", map[string]interface{}{"resource_limits.memory_floor_mb": -1}},
	}
	for _, c := range cases {
		cfg := &config.Config{ResourceLimits: config.DefaultResourceLimits()}
		s := &Server{cfg: cfg, cfgPath: t.TempDir() + "/config.yaml"}
		rr := putResourceLimits(t, s, c.patch)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", c.name, rr.Code)
		}
		if cfg.ResourceLimits != config.DefaultResourceLimits() {
			t.Errorf("%s: config changed to %+v", c.name, cfg.ResourceLimits)
		}
	}
	// Boundaries accepted; floor == cap is allowed.
	cfg := &config.Config{ResourceLimits: config.DefaultResourceLimits()}
	s := &Server{cfg: cfg, cfgPath: t.TempDir() + "/config.yaml"}
	for _, p := range []map[string]interface{}{
		{"resource_limits.memory_share_percent": 1},
		{"resource_limits.memory_share_percent": 100},
		{"resource_limits.memory_floor_mb": 4096, "resource_limits.memory_cap_mb": 4096},
	} {
		if rr := putResourceLimits(t, s, p); rr.Code != http.StatusOK {
			t.Errorf("%v: want 200, got %d %s", p, rr.Code, rr.Body.String())
		}
	}
}
