// autonomous.stuck_running_seconds — config round-trip: PUT /api/config stores
// the value (whole numbers >= 0) and GET /api/config exposes it. Zero is stored
// as zero; the 900-second fallback (StuckRunningDefaultSeconds) is applied at
// consumption time via config.NormalizeStuckRunningSeconds.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func getStuckRunningSeconds(t *testing.T, s *Server) int {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d", rr.Code)
	}
	var got struct {
		Autonomous struct {
			StuckRunningSeconds int `json:"stuck_running_seconds"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	return got.Autonomous.StuckRunningSeconds
}

func TestConfigStuckRunning_PutThenGet(t *testing.T) {
	s := &Server{cfg: &config.Config{}, cfgPath: t.TempDir() + "/config.yaml"}
	body := strings.NewReader(`{"autonomous.stuck_running_seconds": 1200}`)
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", body))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if s.cfg.Autonomous.StuckRunningSeconds != 1200 {
		t.Fatalf("StuckRunningSeconds = %d, want 1200", s.cfg.Autonomous.StuckRunningSeconds)
	}
	if got := getStuckRunningSeconds(t, s); got != 1200 {
		t.Errorf("GET stuck_running_seconds = %d, want 1200", got)
	}
}

func TestConfigStuckRunning_RejectsNegative(t *testing.T) {
	cfg := &config.Config{}
	cfg.Autonomous.StuckRunningSeconds = 600
	applyConfigPatch(cfg, map[string]interface{}{"autonomous.stuck_running_seconds": -5})
	if cfg.Autonomous.StuckRunningSeconds != 600 {
		t.Fatalf("StuckRunningSeconds = %d after a negative patch, want 600 unchanged", cfg.Autonomous.StuckRunningSeconds)
	}
	applyConfigPatch(cfg, map[string]interface{}{"autonomous.stuck_running_seconds": 0})
	if cfg.Autonomous.StuckRunningSeconds != 0 {
		t.Fatalf("StuckRunningSeconds = %d after a zero patch, want 0", cfg.Autonomous.StuckRunningSeconds)
	}
}
