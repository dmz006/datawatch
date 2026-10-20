// Foreign-file drift — Configuration Accessibility round-trip:
// PUT /api/config then GET /api/config must show
// autonomous.block_on_file_drift, the live manager must receive it, and
// an Automaton's task JSON must carry foreign_file_drift for the PWA /
// MCP autonomous_prd_get readers.

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

func TestConfigRoundTrip_BlockOnFileDrift(t *testing.T) {
	spy := &autonomousConfigPropagationSpy{}
	s := &Server{
		cfg:           &config.Config{},
		cfgPath:       t.TempDir() + "/config.yaml",
		autonomousMgr: spy,
	}
	get := func() bool {
		t.Helper()
		rr := httptest.NewRecorder()
		s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET: want 200, got %d", rr.Code)
		}
		var got struct {
			Autonomous map[string]json.RawMessage `json:"autonomous"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode GET: %v", err)
		}
		raw, ok := got.Autonomous["block_on_file_drift"]
		if !ok {
			t.Fatalf("GET /api/config autonomous map lacks block_on_file_drift")
		}
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatalf("decode block_on_file_drift: %v", err)
		}
		return v
	}
	put := func(v bool) {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"autonomous.block_on_file_drift": v})
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT %v: want 200, got %d body=%s", v, rr.Code, rr.Body.String())
		}
	}

	if get() {
		t.Fatal("block_on_file_drift default must be false (warn-only)")
	}
	put(true)
	if !get() {
		t.Fatal("GET block_on_file_drift = false after PUT true")
	}
	var fwd map[string]json.RawMessage
	if err := json.Unmarshal(spy.lastSetConfig, &fwd); err != nil {
		t.Fatalf("decode propagated payload: %v", err)
	}
	if string(fwd["block_on_file_drift"]) != "true" {
		t.Errorf("block_on_file_drift not propagated to manager: %s", spy.lastSetConfig)
	}

	// Clearing back to false must reach the live manager too.
	put(false)
	if get() {
		t.Fatal("GET block_on_file_drift = true after PUT false")
	}
	fwd = nil
	if err := json.Unmarshal(spy.lastSetConfig, &fwd); err != nil {
		t.Fatalf("decode propagated payload: %v", err)
	}
	if string(fwd["block_on_file_drift"]) != "false" {
		t.Errorf("block_on_file_drift=false not propagated to manager: %s", spy.lastSetConfig)
	}
}

func TestConfigRoundTrip_DriftMode(t *testing.T) {
	spy := &autonomousConfigPropagationSpy{}
	s := &Server{
		cfg:           &config.Config{},
		cfgPath:       t.TempDir() + "/config.yaml",
		autonomousMgr: spy,
	}
	get := func() string {
		t.Helper()
		rr := httptest.NewRecorder()
		s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET: want 200, got %d", rr.Code)
		}
		var got struct {
			Autonomous map[string]json.RawMessage `json:"autonomous"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode GET: %v", err)
		}
		raw, ok := got.Autonomous["drift_mode"]
		if !ok {
			t.Fatalf("GET /api/config autonomous map lacks drift_mode")
		}
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatalf("decode drift_mode: %v", err)
		}
		return v
	}
	put := func(v string, wantStatus int) string {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"autonomous.drift_mode": v})
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
		if rr.Code != wantStatus {
			t.Fatalf("PUT %q: want %d, got %d body=%s", v, wantStatus, rr.Code, rr.Body.String())
		}
		return rr.Body.String()
	}

	// Default is block (rendered as the effective mode even on a
	// zero-valued config, matching the executor's resolveDriftMode).
	if got := get(); got != "block" {
		t.Fatalf("drift_mode default = %q, want \"block\"", got)
	}

	// Invalid values are rejected with a 400 and a clear error that
	// names the accepted set, without mutating the live config.
	for _, bad := range []string{"allow", "", "silent", "true"} {
		body := put(bad, http.StatusBadRequest)
		if !strings.Contains(body, `block" or "warn"`) {
			t.Errorf("PUT %q rejection does not name accepted values: %s", bad, body)
		}
		if got := get(); got != "block" {
			t.Errorf("drift_mode after rejected PUT %q = %q, want unchanged \"block\"", bad, got)
		}
	}

	// warn is accepted, reads back, and propagates to the live manager.
	put("warn", http.StatusOK)
	if got := get(); got != "warn" {
		t.Fatalf("GET drift_mode = %q after PUT warn", got)
	}
	var fwd map[string]json.RawMessage
	if err := json.Unmarshal(spy.lastSetConfig, &fwd); err != nil {
		t.Fatalf("decode propagated payload: %v", err)
	}
	if string(fwd["drift_mode"]) != `"warn"` {
		t.Errorf("drift_mode not propagated to manager: %s", spy.lastSetConfig)
	}

	// Switching back to block round-trips too.
	put("block", http.StatusOK)
	if got := get(); got != "block" {
		t.Fatalf("GET drift_mode = %q after PUT block", got)
	}
}

// TestDriftMode_RealManager surfaces exercise the real autonomous manager
// (same adapter main.go uses) so SetConfig merge, GH#180 sync-back and
// the config.yaml round-trip all run for real, on both REST config
// surfaces.
func TestDriftMode_RealManager(t *testing.T) {
	s, mgr, cfgPath := newMaxConcurrentTasksServer(t)

	// Default is block on every surface.
	if got := string(getAutonomousConfigMap(t, s)["drift_mode"]); got != `"block"` {
		t.Fatalf("GET /api/config drift_mode default = %q, want \"block\"", got)
	}
	if got := mgr.Config().DriftMode; got != "block" {
		t.Fatalf("live manager DriftMode default = %q, want block", got)
	}

	// warn is accepted via PUT /api/config, reads back, and reaches the
	// live manager + config.yaml.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.drift_mode": "warn"})
	if got := string(getAutonomousConfigMap(t, s)["drift_mode"]); got != `"warn"` {
		t.Fatalf("GET drift_mode = %q after PUT warn", got)
	}
	if got := mgr.Config().DriftMode; got != "warn" {
		t.Fatalf("live manager DriftMode = %q, want warn", got)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	if !strings.Contains(string(raw), "drift_mode: warn") {
		t.Fatalf("config.yaml lacks drift_mode: warn:\n%s", raw)
	}

	// Invalid values are rejected with 400 + a clear message on both
	// surfaces, without touching the live config.
	for _, bad := range []string{"allow", "", "silent"} {
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(`{"autonomous.drift_mode":`+fmt.Sprintf("%q", bad)+`}`)))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("PUT /api/config drift_mode=%q: want 400, got %d body=%s", bad, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), `block" or "warn"`) {
			t.Errorf("PUT /api/config drift_mode=%q rejection lacks accepted values: %s", bad, rr.Body.String())
		}

		rr = httptest.NewRecorder()
		s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodPut, "/api/autonomous/config", strings.NewReader(`{"drift_mode":`+fmt.Sprintf("%q", bad)+`}`)))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("PUT /api/autonomous/config drift_mode=%q: want 400, got %d body=%s", bad, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), `block" or "warn"`) {
			t.Errorf("PUT /api/autonomous/config drift_mode=%q rejection lacks accepted values: %s", bad, rr.Body.String())
		}

		if got := s.cfg.Autonomous.DriftMode; got != "warn" {
			t.Errorf("cfg drift_mode = %q after rejected PUT %q, want unchanged \"warn\"", got, bad)
		}
		if got := mgr.Config().DriftMode; got != "warn" {
			t.Errorf("manager drift_mode = %q after rejected PUT %q, want unchanged \"warn\"", got, bad)
		}
	}

	// warn is accepted via PUT /api/autonomous/config and syncs back to
	// s.cfg.Autonomous (GH#180).
	putJSON(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"drift_mode": "warn"})
	if got := mgr.Config().DriftMode; got != "warn" {
		t.Fatalf("live manager DriftMode = %q, want warn", got)
	}
	if got := s.cfg.Autonomous.DriftMode; got != "warn" {
		t.Fatalf("s.cfg.Autonomous.DriftMode = %q, want warn (GH#180 sync-back)", got)
	}

	// Switch back to block and confirm every surface agrees.
	putJSON(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"drift_mode": "block"})
	if got := string(getAutonomousConfigMap(t, s)["drift_mode"]); got != `"block"` {
		t.Fatalf("GET drift_mode = %q after PUT block", got)
	}
	if got := mgr.Config().DriftMode; got != "block" {
		t.Fatalf("live manager DriftMode = %q, want block", got)
	}
}

func TestAutomatonTaskJSON_CarriesForeignFileDrift(t *testing.T) {
	prd := autonomous.PRD{
		ID: "a1",
		Story: []autonomous.Story{{
			ID: "s1",
			Tasks: []autonomous.Task{
				{ID: "t1", ForeignFileDrift: []string{"docs/notes.md", "internal/x.go"}},
				{ID: "t2"},
			},
		}},
	}
	b, err := json.Marshal(prd)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Stories []struct {
			Tasks []map[string]json.RawMessage `json:"tasks"`
		} `json:"stories"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	tasks := got.Stories[0].Tasks
	if string(tasks[0]["foreign_file_drift"]) != `["docs/notes.md","internal/x.go"]` {
		t.Errorf("task foreign_file_drift = %s", tasks[0]["foreign_file_drift"])
	}
	if _, ok := tasks[1]["foreign_file_drift"]; ok {
		t.Errorf("task without drift must omit foreign_file_drift: %v", tasks[1])
	}
}
