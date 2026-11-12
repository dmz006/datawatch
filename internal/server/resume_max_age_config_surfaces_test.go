// Configuration Accessibility round-trip for
// autonomous.resume_max_age_seconds (B165 boot-resume age limit, default
// 3600; 0 = never resume, -1 = no age limit) on both REST config surfaces.
// Modeled on max_concurrent_tasks_config_surfaces_test.go, with the
// sentinel twist: 0 and -1 are meaningful values and must survive the
// round trip.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// newResumeMaxAgeServer wires a real autonomous manager (through
// autonomous.API, the same adapter main.go uses) so the live-manager
// assertions exercise the real SetConfig merge, not a fake.
func newResumeMaxAgeServer(t *testing.T) (*Server, *autonomous.Manager, string) {
	t.Helper()
	dir := t.TempDir()
	mgr, err := autonomous.NewManager(filepath.Join(dir, "autonomous"), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	cfgPath := filepath.Join(dir, "config.yaml")
	s := &Server{
		cfg:           config.DefaultConfig(),
		cfgPath:       cfgPath,
		autonomousMgr: newTestAPI(mgr),
	}
	return s, mgr, cfgPath
}

func getResumeMaxAgeFromConfigMap(t *testing.T, s *Server) int {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d", rr.Code)
	}
	var got struct {
		Autonomous map[string]json.RawMessage `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	raw, ok := got.Autonomous["resume_max_age_seconds"]
	if !ok {
		t.Fatalf("GET /api/config autonomous map lacks resume_max_age_seconds: %s", rr.Body.String())
	}
	var v int
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode resume_max_age_seconds: %v", err)
	}
	return v
}

func TestResumeMaxAge_GetConfigDefaultIs3600(t *testing.T) {
	s, mgr, _ := newResumeMaxAgeServer(t)
	if got := getResumeMaxAgeFromConfigMap(t, s); got != 3600 {
		t.Fatalf("GET /api/config autonomous.resume_max_age_seconds = %d, want 3600", got)
	}
	if got := mgr.Config().ResumeMaxAgeSeconds; got != 3600 {
		t.Fatalf("live manager ResumeMaxAgeSeconds = %d, want 3600", got)
	}
	// The autonomous config endpoint serves the manager config directly.
	rr := httptest.NewRecorder()
	s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodGet, "/api/autonomous/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/autonomous/config: want 200, got %d", rr.Code)
	}
	var mgrCfg map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &mgrCfg); err != nil {
		t.Fatalf("decode autonomous GET: %v", err)
	}
	var v int
	if err := json.Unmarshal(mgrCfg["resume_max_age_seconds"], &v); err != nil {
		t.Fatalf("decode autonomous resume_max_age_seconds: %v", err)
	}
	if v != 3600 {
		t.Fatalf("GET /api/autonomous/config resume_max_age_seconds = %d, want 3600", v)
	}
}

func TestResumeMaxAge_PutConfigRoundTrip(t *testing.T) {
	s, mgr, cfgPath := newResumeMaxAgeServer(t)

	// Normal value round-trips everywhere: s.cfg, live manager, GET, disk.
	resp := putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.resume_max_age_seconds": 7200})
	if _, ok := resp["deprecated_fields"]; ok {
		t.Errorf("deprecated_fields present for a non-deprecated key: %v", resp)
	}
	if got := s.cfg.Autonomous.ResumeMaxAgeSeconds; got != 7200 {
		t.Fatalf("s.cfg.Autonomous.ResumeMaxAgeSeconds = %d, want 7200", got)
	}
	if got := mgr.Config().ResumeMaxAgeSeconds; got != 7200 {
		t.Fatalf("live manager ResumeMaxAgeSeconds = %d, want 7200", got)
	}
	if got := getResumeMaxAgeFromConfigMap(t, s); got != 7200 {
		t.Fatalf("GET after PUT = %d, want 7200", got)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	if !strings.Contains(string(raw), "resume_max_age_seconds: 7200") {
		t.Fatalf("config.yaml lacks resume_max_age_seconds: 7200:\n%s", raw)
	}

	// 0 is a meaningful sentinel (never resume), not the zero value: it
	// must be stored and reach the live manager.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.resume_max_age_seconds": 0})
	if got := s.cfg.Autonomous.ResumeMaxAgeSeconds; got != 0 {
		t.Fatalf("0 must be stored, got s.cfg.Autonomous.ResumeMaxAgeSeconds = %d", got)
	}
	if got := mgr.Config().ResumeMaxAgeSeconds; got != 0 {
		t.Fatalf("0 must reach the live manager, got %d", got)
	}
	if got := getResumeMaxAgeFromConfigMap(t, s); got != 0 {
		t.Fatalf("GET after PUT 0 = %d, want 0", got)
	}
	raw, _ = os.ReadFile(cfgPath)
	if !strings.Contains(string(raw), "resume_max_age_seconds: 0") {
		t.Fatalf("config.yaml lacks resume_max_age_seconds: 0:\n%s", raw)
	}

	// -1 is the "no age limit" sentinel: stored as-is.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.resume_max_age_seconds": -1})
	if got := s.cfg.Autonomous.ResumeMaxAgeSeconds; got != -1 {
		t.Fatalf("-1 must be stored, got s.cfg.Autonomous.ResumeMaxAgeSeconds = %d", got)
	}
	if got := mgr.Config().ResumeMaxAgeSeconds; got != -1 {
		t.Fatalf("-1 must reach the live manager, got %d", got)
	}

	// Values below -1 are skipped, not stored.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.resume_max_age_seconds": -5})
	if got := s.cfg.Autonomous.ResumeMaxAgeSeconds; got != -1 {
		t.Fatalf("-5 must be skipped: s.cfg.Autonomous.ResumeMaxAgeSeconds = %d, want -1", got)
	}
	if got := mgr.Config().ResumeMaxAgeSeconds; got != -1 {
		t.Fatalf("-5 must be skipped by the live manager too: got %d, want -1", got)
	}
}

func TestResumeMaxAge_PutAutonomousConfigPersists(t *testing.T) {
	s, mgr, cfgPath := newResumeMaxAgeServer(t)
	resp := putJSON(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"resume_max_age_seconds": 0})
	if string(resp["status"]) != `"ok"` {
		t.Errorf("status = %s", resp["status"])
	}
	if got := mgr.Config().ResumeMaxAgeSeconds; got != 0 {
		t.Fatalf("live manager ResumeMaxAgeSeconds = %d, want 0", got)
	}
	if got := s.cfg.Autonomous.ResumeMaxAgeSeconds; got != 0 {
		t.Fatalf("s.cfg.Autonomous.ResumeMaxAgeSeconds = %d, want 0 (GH#180 sync-back)", got)
	}
	if got := getResumeMaxAgeFromConfigMap(t, s); got != 0 {
		t.Fatalf("GET /api/config = %d, want 0", got)
	}
	raw, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(raw), "resume_max_age_seconds: 0") {
		t.Fatalf("config.yaml lacks resume_max_age_seconds: 0:\n%s", raw)
	}
}
