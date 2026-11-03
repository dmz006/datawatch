package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

func getCfgSection(t *testing.T, s *Server, name string) map[string]interface{} {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var got struct {
		Autonomous map[string]json.RawMessage `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	raw, ok := got.Autonomous[name]
	if !ok {
		t.Fatalf("GET /api/config autonomous map lacks %q", name)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestChecksShipConfig_PutGetRoundTrip(t *testing.T) {
	s := &Server{cfg: config.DefaultConfig(), cfgPath: filepath.Join(t.TempDir(), "config.yaml")}
	if c := getCfgSection(t, s, "checks"); c["after_done"] != "on" || c["block_when_missing"] != true {
		t.Fatalf("defaults = %v", c)
	}
	if sh := getCfgSection(t, s, "ship"); sh["push"] != "on" || sh["tag_format"] != "v{version}" || sh["remote"] != "origin" {
		t.Fatalf("ship defaults = %v", sh)
	}
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.checks.after_done":         "Shadow",
		"autonomous.checks.block_when_missing": false,
		"autonomous.ship.push":                 "off",
		"autonomous.ship.tag_format":           "rel-{version}",
		"autonomous.ship.remote":               "upstream",
		"autonomous.ship.ci_workflows":         "release.yaml, ci.yaml",
	})
	c := getCfgSection(t, s, "checks")
	if c["after_done"] != "shadow" || c["block_when_missing"] != false {
		t.Fatalf("checks after PUT = %v", c)
	}
	sh := getCfgSection(t, s, "ship")
	wf, _ := sh["ci_workflows"].([]interface{})
	if sh["push"] != "off" || sh["tag_format"] != "rel-{version}" || sh["remote"] != "upstream" || len(wf) != 2 {
		t.Fatalf("ship after PUT = %v", sh)
	}
	if p := autonomous.ChecksPolicyFor(""); p.AfterDone != "shadow" || p.BlockWhenMissing {
		t.Fatalf("live checks policy not refreshed: %+v", p)
	}
	t.Cleanup(func() {
		autonomous.SetChecksConfig(config.AutonomousChecksConfig{})
		autonomous.SetShipConfig(config.AutonomousShipConfig{})
	})
}

func TestChecksShipConfig_PutRefusesBadValues(t *testing.T) {
	s := &Server{cfg: config.DefaultConfig(), cfgPath: filepath.Join(t.TempDir(), "config.yaml")}
	for name, patch := range map[string]map[string]any{
		"bad mode":        {"autonomous.checks.plan_rules": "maybe"},
		"non-string mode": {"autonomous.ship.tag": 1},
		"bad bool":        {"autonomous.checks.block_when_missing": "yes"},
		"no placeholder":  {"autonomous.ship.tag_format": "release"},
	} {
		pcPutBad(t, s.handlePutConfig, "/api/config", patch)
		_ = name
	}
	if c := getCfgSection(t, s, "checks"); c["plan_rules"] != "on" {
		t.Fatalf("a refused PUT must change nothing: %v", c)
	}
}

type checksShipSpy struct {
	fakeOrchAutonomous
	records []*autonomous.TriageRecord
}

func (s *checksShipSpy) ListTriageRecords() []*autonomous.TriageRecord { return s.records }
func (s *checksShipSpy) TriageAlertMinutes() int                       { return 0 }

func TestChecksShipStatusRoutes(t *testing.T) {
	t0 := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	spy := &checksShipSpy{records: []*autonomous.TriageRecord{
		{PRDID: "p1", Notes: []autonomous.TriageNote{
			{At: t0, Source: "after_done_checklist", Text: "checklist"},
			{At: t0, Source: "ship_step", Text: "pushed"},
		}},
		{PRDID: "p2", Notes: []autonomous.TriageNote{{At: t0, Source: "ship_ci_watch", Text: "ok"}}},
	}}
	s := &Server{cfg: config.DefaultConfig(), autonomousMgr: spy}

	rr := httptest.NewRecorder()
	s.handleAutonomousChecks(rr, httptest.NewRequest(http.MethodGet, "/api/autonomous/checks?prd_id=p1", nil))
	var cs autonomous.ChecksStatus
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &cs) != nil || len(cs.Findings) != 1 || cs.PRDID != "p1" {
		t.Fatalf("checks: %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	s.handleAutonomousShip(rr, httptest.NewRequest(http.MethodGet, "/api/autonomous/ship", nil))
	var ss autonomous.ShipStatus
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &ss) != nil || len(ss.Findings) != 2 || ss.Policy.Remote != "origin" {
		t.Fatalf("ship: %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	s.handleAutonomousShip(rr, httptest.NewRequest(http.MethodPost, "/api/autonomous/ship", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d, want 405", rr.Code)
	}
	if !strings.HasPrefix(ChecksShipRESTOperations()[0].Path, "/api/autonomous/") || len(ChecksShipRESTOperations()) != 2 {
		t.Fatal("REST operations list is wrong")
	}
}
