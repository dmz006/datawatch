// Configuration accessibility round-trip for autonomous.snapshot_never_stage
// (path patterns the autonomous snapshot must never `git add`) on both REST
// config surfaces: /api/config (daemon config) and /api/autonomous/config
// (live manager). Modeled on resume_max_age_config_surfaces_test.go.
//
// Sentinels worth covering explicitly:
//   - unset (nil)  = the built-in defaults (config.DefaultSnapshotNeverStage)
//   - explicit []  = no protected patterns (the defaults do NOT apply)
//   - blank entries are rejected loudly (400) on both write surfaces.

package server

import (
	"bytes"
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

func newSnapshotNeverStageServer(t *testing.T) (*Server, *autonomous.Manager, string) {
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

func doGetSnapshotNeverStage(t *testing.T, s *Server) []string {
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
	raw, ok := got.Autonomous["snapshot_never_stage"]
	if !ok {
		t.Fatalf("GET /api/config autonomous map lacks snapshot_never_stage: %s", rr.Body.String())
	}
	var v []string
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode snapshot_never_stage: %v", err)
	}
	return v
}

func TestSnapshotNeverStage_GetConfigServesEffectiveDefaults(t *testing.T) {
	s, mgr, _ := newSnapshotNeverStageServer(t)
	want := config.DefaultSnapshotNeverStage

	got := doGetSnapshotNeverStage(t, s)
	if len(got) != len(want) {
		t.Fatalf("GET /api/config snapshot_never_stage = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("GET /api/config snapshot_never_stage = %v, want %v", got, want)
		}
	}
	// Fresh manager serves the same effective defaults.
	if got := mgr.Config().SnapshotNeverStage; len(got) != len(want) {
		t.Fatalf("live manager SnapshotNeverStage = %v, want %v", got, want)
	}
}

func TestSnapshotNeverStage_PutConfigRoundTrip(t *testing.T) {
	s, mgr, cfgPath := newSnapshotNeverStageServer(t)

	// JSON array round-trips everywhere: s.cfg, live manager, GET, disk.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.snapshot_never_stage": []any{".env", "node_modules/", "secrets/*.pem"},
	})
	want := []string{".env", "node_modules/", "secrets/*.pem"}

	if got := s.cfg.Autonomous.SnapshotNeverStage; len(got) != len(want) || got[0] != ".env" {
		t.Fatalf("s.cfg.Autonomous.SnapshotNeverStage = %v, want %v", got, want)
	}
	if got := mgr.Config().SnapshotNeverStage; len(got) != len(want) || got[0] != ".env" {
		t.Fatalf("live manager SnapshotNeverStage = %v, want %v", got, want)
	}
	if got := doGetSnapshotNeverStage(t, s); len(got) != len(want) || got[2] != "secrets/*.pem" {
		t.Fatalf("GET after PUT = %v, want %v", got, want)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	if !strings.Contains(string(raw), ".env") {
		t.Fatalf("config.yaml lacks snapshot_never_stage entries:\n%s", raw)
	}

	// An explicitly empty list is meaningful (disable every never-stage
	// rule) and must survive the round trip intact — it is NOT the nil /
	// unset state that reads the defaults.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.snapshot_never_stage": []any{},
	})
	if got := s.cfg.Autonomous.SnapshotNeverStage; got == nil {
		t.Fatal("explicit [] PUT must land as an empty (non-nil) list, not nil")
	}
	if len(s.cfg.Autonomous.SnapshotNeverStage) != 0 {
		t.Fatalf("explicit [] PUT: s.cfg = %v, want empty slice", s.cfg.Autonomous.SnapshotNeverStage)
	}
	if got := doGetSnapshotNeverStage(t, s); len(got) != 0 {
		t.Fatalf("GET after explicit [] PUT = %v, want empty list", got)
	}
}

func TestSnapshotNeverStage_PutConfigAcceptsCSVString(t *testing.T) {
	s, mgr, _ := newSnapshotNeverStageServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.snapshot_never_stage": ".env, node_modules",
	})
	want := []string{".env", "node_modules"}
	if got := s.cfg.Autonomous.SnapshotNeverStage; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("s.cfg.Autonomous.SnapshotNeverStage = %v, want %v", got, want)
	}
	if got := mgr.Config().SnapshotNeverStage; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("live manager SnapshotNeverStage = %v, want %v", got, want)
	}
}

func TestSnapshotNeverStage_PutConfigRejectsBlankPatterns(t *testing.T) {
	s, _, _ := newSnapshotNeverStageServer(t)
	before := append([]string(nil), s.cfg.Autonomous.SnapshotNeverStage...)

	// A blank entry (array form) rejects the whole PUT. Note the CSV
	// forms below drop blanks (splitCSV) the same way the other
	// comma-list keys do, so they validate as ordinary lists.
	cases := []any{
		[]any{".env", "   "}, // array form with a whitespace-only entry
		[]any{""},            // array form with an empty entry
	}
	for _, body := range cases {
		b, _ := json.Marshal(map[string]any{"autonomous.snapshot_never_stage": body})
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("PUT %s: want 400 (blank patterns rejected), got %d body=%s", b, rr.Code, rr.Body.String())
		}
	}
	// And nothing was applied: the value is exactly what it was before
	// the rejected writes.
	got := s.cfg.Autonomous.SnapshotNeverStage
	if len(got) != len(before) {
		t.Fatalf("rejected PUT should not have changed the value: %v (was %v)", got, before)
	}
	for i := range before {
		if got[i] != before[i] {
			t.Fatalf("rejected PUT should not have changed the value: %v (was %v)", got, before)
		}
	}
}

func TestSnapshotNeverStage_PutConfigRejectsWrongType(t *testing.T) {
	s, _, _ := newSnapshotNeverStageServer(t)
	cases := []any{42, 3.14, map[string]any{"a": 1}, true}
	for _, body := range cases {
		b, _ := json.Marshal(map[string]any{"autonomous.snapshot_never_stage": body})
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("PUT %s: want 400 (wrong type), got %d body=%s", b, rr.Code, rr.Body.String())
		}
	}
}

// --- /api/autonomous/config surface ----------------------------------------

func TestSnapshotNeverStage_AutonomousEndpointPutRoundTrip(t *testing.T) {
	s, mgr, _ := newSnapshotNeverStageServer(t)
	want := []string{".env", "data/"}

	body, _ := json.Marshal(map[string]any{"snapshot_never_stage": []any{".env", "data/"}})
	rr := httptest.NewRecorder()
	s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodPut, "/api/autonomous/config", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT /api/autonomous/config: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := mgr.Config().SnapshotNeverStage; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("live manager SnapshotNeverStage = %v, want %v", got, want)
	}
	// GET from the autonomous endpoint serves the stored list.
	rr = httptest.NewRecorder()
	s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodGet, "/api/autonomous/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/autonomous/config: want 200, got %d", rr.Code)
	}
	var mgrCfg map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &mgrCfg); err != nil {
		t.Fatalf("decode autonomous GET: %v", err)
	}
	var got []string
	if err := json.Unmarshal(mgrCfg["snapshot_never_stage"], &got); err != nil {
		t.Fatalf("decode autonomous snapshot_never_stage: %v", err)
	}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("GET /api/autonomous/config snapshot_never_stage = %v, want %v", got, want)
	}
}

func TestSnapshotNeverStage_AutonomousEndpointRejectsInvalid(t *testing.T) {
	s, _, _ := newSnapshotNeverStageServer(t)
	cases := []struct {
		name string
		body any
	}{
		{"blank entry", map[string]any{"snapshot_never_stage": []any{".env", "  "}}},
		{"wrong type", map[string]any{"snapshot_never_stage": 7}},
	}
	for _, tc := range cases {
		b, _ := json.Marshal(tc.body)
		rr := httptest.NewRecorder()
		s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodPut, "/api/autonomous/config", bytes.NewReader(b)))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d body=%s", tc.name, rr.Code, rr.Body.String())
		}
	}
}
