// Daily pending-branch alert settings: GET defaults, PUT /api/config
// validation and round trip, live apply into the autonomous manager, and the
// dedicated /api/autonomous/config set path syncing back into the daemon
// config.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

func TestUnlandedAlertConfig_GetDefaults(t *testing.T) {
	s := newRoundTripServer(t)
	auto := autonomousSection(t, getConfigView(t, s))
	if auto["unlanded_alert_enabled"] != true {
		t.Errorf("unlanded_alert_enabled = %v, want true", auto["unlanded_alert_enabled"])
	}
	if auto["unlanded_alert_threshold"] != float64(3) {
		t.Errorf("unlanded_alert_threshold = %v, want 3", auto["unlanded_alert_threshold"])
	}
}

func TestUnlandedAlertConfig_PutRoundTrip(t *testing.T) {
	s := newRoundTripServer(t)
	// Strings are what the CLI and comm channel send.
	for _, patch := range []map[string]interface{}{
		{unlandedAlertEnabledKey: false, unlandedAlertThresholdKey: 0},
		{unlandedAlertEnabledKey: "true", unlandedAlertThresholdKey: "9"},
	} {
		if rr := putPatch(t, s, patch); rr.Code != http.StatusOK {
			t.Fatalf("PUT %v = %d: %s", patch, rr.Code, rr.Body.String())
		}
	}
	auto := autonomousSection(t, getConfigView(t, s))
	if auto["unlanded_alert_enabled"] != true || auto["unlanded_alert_threshold"] != float64(9) {
		t.Fatalf("readback = %v / %v", auto["unlanded_alert_enabled"], auto["unlanded_alert_threshold"])
	}
	// Persisted to the config file.
	loaded, err := config.Load(s.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := config.UnlandedAlertThresholdEffective(loaded.Autonomous.UnlandedAlertThreshold); got != 9 {
		t.Fatalf("saved threshold = %d, want 9", got)
	}
	// An explicit 0 survives a round trip as 0, not as the default.
	if rr := putPatch(t, s, map[string]interface{}{unlandedAlertThresholdKey: 0}); rr.Code != http.StatusOK {
		t.Fatal(rr.Body.String())
	}
	if got := autonomousSection(t, getConfigView(t, s))["unlanded_alert_threshold"]; got != float64(0) {
		t.Fatalf("threshold after 0 = %v", got)
	}
}

func TestUnlandedAlertConfig_PutRejectsBadValues(t *testing.T) {
	for _, patch := range []map[string]interface{}{
		{unlandedAlertThresholdKey: -1},
		{unlandedAlertThresholdKey: 2.5},
		{unlandedAlertThresholdKey: "lots"},
		{unlandedAlertEnabledKey: "maybe"},
		{unlandedAlertEnabledKey: 1},
	} {
		s := newRoundTripServer(t)
		rr := putPatch(t, s, patch)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("PUT %v = %d, want 400", patch, rr.Code)
		}
		if s.cfg.Autonomous.UnlandedAlertThreshold != nil || s.cfg.Autonomous.UnlandedAlertEnabled != nil {
			t.Errorf("PUT %v stored a value despite the refusal", patch)
		}
	}
}

func TestApplyUnlandedAlertConfigKey(t *testing.T) {
	cfg := config.DefaultConfig()
	if h, a := applyUnlandedAlertConfigKey(cfg, "autonomous.other", 1); h || a {
		t.Fatal("unrelated key handled")
	}
	if _, a := applyUnlandedAlertConfigKey(cfg, unlandedAlertThresholdKey, -2); a {
		t.Fatal("negative applied")
	}
	if _, a := applyUnlandedAlertConfigKey(cfg, unlandedAlertEnabledKey, "nope"); a {
		t.Fatal("non-boolean applied")
	}
	if _, a := applyUnlandedAlertConfigKey(cfg, unlandedAlertThresholdKey, 4); !a || *cfg.Autonomous.UnlandedAlertThreshold != 4 {
		t.Fatal("int not applied")
	}
	if len(UnlandedAlertConfigKeys()) != 2 {
		t.Fatal("key list")
	}
}

// newUnlandedAlertManagerServer wires a real autonomous manager so both
// write paths can be checked against its live Config.
func newUnlandedAlertManagerServer(t *testing.T) (*Server, *autonomous.Manager) {
	t.Helper()
	dir := t.TempDir()
	mgr, err := autonomous.NewManager(filepath.Join(dir, "autonomous"), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.DefaultConfig(), cfgPath: filepath.Join(dir, "config.yaml"), autonomousMgr: autonomous.NewAPI(mgr)}
	return s, mgr
}

func TestUnlandedAlertConfig_LiveApplyIntoManager(t *testing.T) {
	s, mgr := newUnlandedAlertManagerServer(t)
	rr := putPatch(t, s, map[string]interface{}{unlandedAlertEnabledKey: false, unlandedAlertThresholdKey: 6})
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT = %d: %s", rr.Code, rr.Body.String())
	}
	c := mgr.Config()
	if c.UnlandedAlertEnabled == nil || *c.UnlandedAlertEnabled || c.UnlandedAlertThreshold == nil || *c.UnlandedAlertThreshold != 6 {
		t.Fatalf("manager config not live-applied: %v / %v", c.UnlandedAlertEnabled, c.UnlandedAlertThreshold)
	}
	if res := mgr.UnlandedAlertCheck(t.Context()); res.Outcome != autonomous.UnlandedAlertDisabled {
		t.Fatalf("check after disable: %+v", res)
	}
}

func TestUnlandedAlertConfig_AutonomousConfigSetPath(t *testing.T) {
	s, mgr := newUnlandedAlertManagerServer(t)
	put := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/autonomous/config", bytes.NewReader([]byte(body)))
		rr := httptest.NewRecorder()
		s.handleAutonomousConfig(rr, req)
		return rr
	}
	if rr := put(`{"unlanded_alert_threshold": 1, "unlanded_alert_enabled": true}`); rr.Code != http.StatusOK {
		t.Fatalf("PUT = %d: %s", rr.Code, rr.Body.String())
	}
	if got := mgr.Config().UnlandedAlertThreshold; got == nil || *got != 1 {
		t.Fatalf("manager threshold = %v", got)
	}
	// Synced back into the daemon config and so GET /api/config.
	if got := autonomousSection(t, getConfigView(t, s))["unlanded_alert_threshold"]; got != float64(1) {
		t.Fatalf("GET /api/config threshold = %v", got)
	}
	// GET /api/autonomous/config shows the stored value.
	rr := httptest.NewRecorder()
	s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodGet, "/api/autonomous/config", nil))
	var out map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if string(out["unlanded_alert_threshold"]) != "1" {
		t.Fatalf("GET /api/autonomous/config threshold = %s", out["unlanded_alert_threshold"])
	}
	// A negative threshold is refused and the live value stays.
	if rr := put(`{"unlanded_alert_threshold": -3}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("negative PUT = %d", rr.Code)
	}
	if got := mgr.Config().UnlandedAlertThreshold; *got != 1 {
		t.Fatalf("threshold after refused write = %d", *got)
	}
}
