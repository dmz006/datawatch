// Configuration round-trip for autonomous.capacity_rate_requests,
// capacity_rate_tokens and capacity_rate_window_seconds on the REST config
// surfaces: GET/PUT /api/config and GET/PUT /api/autonomous/config. The MCP
// surface is covered in internal/mcp (it imports this package).

package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestRateCeilingREST_GetConfigDefaultsAreZero(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	m := getAutonomousConfigMap(t, s)
	for _, k := range []string{"capacity_rate_requests", "capacity_rate_tokens", "capacity_rate_window_seconds"} {
		if got := string(m[k]); got != "0" {
			t.Errorf("GET /api/config autonomous.%s = %q, want 0", k, got)
		}
	}
}

func TestRateCeilingREST_PutConfigRoundTrip(t *testing.T) {
	s, mgr, cfgPath := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.capacity_rate_requests":       60,
		"autonomous.capacity_rate_tokens":         2000000,
		"autonomous.capacity_rate_window_seconds": 1800,
	})
	m := getAutonomousConfigMap(t, s)
	for k, want := range map[string]string{"capacity_rate_requests": "60", "capacity_rate_tokens": "2000000", "capacity_rate_window_seconds": "1800"} {
		if got := string(m[k]); got != want {
			t.Errorf("GET after PUT %s = %q, want %s", k, got, want)
		}
	}
	c := mgr.Config()
	if c.CapacityRateRequests != 60 || c.CapacityRateTokens != 2000000 || c.CapacityRateWindowSeconds != 1800 {
		t.Fatalf("live manager rate ceiling = %d/%d/%d", c.CapacityRateRequests, c.CapacityRateTokens, c.CapacityRateWindowSeconds)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	if !strings.Contains(string(raw), "capacity_rate_requests: 60") {
		t.Fatalf("config.yaml lacks capacity_rate_requests: 60:\n%s", raw)
	}
}

func TestRateCeilingREST_PutConfigNegativeIgnored(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.capacity_rate_requests": 5})
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.capacity_rate_requests": -4})
	if s.cfg.Autonomous.CapacityRateRequests != 5 {
		t.Fatalf("CapacityRateRequests = %d after a negative PUT, want it left at 5", s.cfg.Autonomous.CapacityRateRequests)
	}
}

func TestRateCeilingREST_PutAutonomousConfigRoundTripAndReject(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"capacity_rate_tokens": 5000, "capacity_rate_window_seconds": 600})
	if got := mgr.Config().CapacityRateTokens; got != 5000 {
		t.Fatalf("live manager CapacityRateTokens = %d, want 5000", got)
	}
	if s.cfg.Autonomous.CapacityRateTokens != 5000 || s.cfg.Autonomous.CapacityRateWindowSeconds != 600 {
		t.Fatalf("s.cfg not synced back: %d/%d", s.cfg.Autonomous.CapacityRateTokens, s.cfg.Autonomous.CapacityRateWindowSeconds)
	}
	rr := httptest.NewRecorder()
	s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodGet, "/api/autonomous/config", nil))
	if !strings.Contains(rr.Body.String(), `"capacity_rate_tokens":5000`) {
		t.Fatalf("GET /api/autonomous/config lacks capacity_rate_tokens:5000: %s", rr.Body.String())
	}

	rr = httptest.NewRecorder()
	s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodPut, "/api/autonomous/config", bytes.NewReader([]byte(`{"capacity_rate_requests":-1}`))))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("PUT negative capacity_rate_requests: want 400, got %d", rr.Code)
	}
	if got := mgr.Config().CapacityRateRequests; got != 0 {
		t.Fatalf("negative value reached the manager: %d", got)
	}
}
