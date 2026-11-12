package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// branchRefreshSpy is the AutonomousAPI double for the override route: it
// keeps one PRD record and implements the optional override setter.
type branchRefreshSpy struct {
	fakeOrchAutonomous
	prd      *autonomous.PRD
	setCalls int
	lastSet  *bool
	cfg      autonomous.Config
}

func (b *branchRefreshSpy) GetPRD(id string) (any, bool) {
	if b.prd == nil || b.prd.ID != id {
		return nil, false
	}
	return b.prd, true
}

func (b *branchRefreshSpy) SetBranchRefreshOverride(id string, v *bool) error {
	b.setCalls++
	b.lastSet = v
	if v == nil {
		b.prd.BranchRefreshOverride = nil
		return nil
	}
	c := *v
	b.prd.BranchRefreshOverride = &c
	return nil
}

func (b *branchRefreshSpy) Config() any { return b.cfg }

func postBranchRefresh(s *Server, id, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/"+id+"/branch-refresh", strings.NewReader(body))
	s.handleBranchRefreshOverride(rec, req, id)
	return rec
}

func TestBranchRefreshOverride_SetClearAndRefuse(t *testing.T) {
	spy := &branchRefreshSpy{prd: &autonomous.PRD{ID: "p1", Title: "Parser"}}
	s := &Server{autonomousMgr: spy, cfg: &config.Config{}}

	rec := postBranchRefresh(s, "p1", `{"enabled": true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("set true: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["branch_refresh_override"] != true || got["branch_refresh_enabled"] != true {
		t.Fatalf("set true: want override and enabled true, got %v / %v", got["branch_refresh_override"], got["branch_refresh_enabled"])
	}
	if v, ok := got["branch_refresh"]; !ok || v != nil {
		t.Fatalf("set true: want branch_refresh present and null, got %v (present=%v)", v, ok)
	}

	rec = postBranchRefresh(s, "p1", `{"enabled": false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("set false: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["branch_refresh_override"] != false || got["branch_refresh_enabled"] != false {
		t.Fatalf("set false: want override and enabled false, got %v / %v", got["branch_refresh_override"], got["branch_refresh_enabled"])
	}

	// Clearing follows the daemon setting again.
	spy.cfg.BranchRefreshEnabled = true
	rec = postBranchRefresh(s, "p1", `{"enabled": null}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if v, ok := got["branch_refresh_override"]; !ok || v != nil {
		t.Fatalf("clear: want override present and null, got %v (present=%v)", v, ok)
	}
	if got["branch_refresh_enabled"] != true {
		t.Fatalf("clear: want enabled to follow the daemon setting (true), got %v", got["branch_refresh_enabled"])
	}
	if spy.setCalls != 3 || spy.lastSet != nil {
		t.Fatalf("want 3 setter calls ending in a clear, got %d (last=%v)", spy.setCalls, spy.lastSet)
	}

	for _, body := range []string{`{}`, `{"enabled": "yes"}`, `{"enabled": 1}`, `not json`} {
		rec = postBranchRefresh(s, "p1", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: want 400, got %d: %s", body, rec.Code, rec.Body.String())
		}
	}
	if rec = postBranchRefresh(s, "missing", `{"enabled": true}`); rec.Code != http.StatusNotFound {
		t.Errorf("unknown id: want 404, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.handleBranchRefreshOverride(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/p1/branch-refresh", nil), "p1")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: want 405, got %d", rec.Code)
	}
	if spy.setCalls != 3 {
		t.Errorf("refused calls must not reach the setter, got %d calls", spy.setCalls)
	}
}

func TestBranchRefreshOverride_ManagerWithoutSetter(t *testing.T) {
	s := &Server{autonomousMgr: &fakeOrchAutonomous{}, cfg: &config.Config{}}
	rec := postBranchRefresh(s, "p1", `{"enabled": true}`)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("want 501, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBranchRefresh_GETDetailCarriesState(t *testing.T) {
	on := true
	spy := &branchRefreshSpy{prd: &autonomous.PRD{
		ID: "p1", Title: "Parser",
		BranchRefreshOverride: &on,
		BranchRefresh:         &autonomous.BranchRefresh{CommitsBehind: 7, LastResult: autonomous.BranchRefreshClean, LastMainCommit: "abc123"},
	}}
	s := &Server{autonomousMgr: spy, cfg: &config.Config{}}
	rec := httptest.NewRecorder()
	s.writePRDDetail(rec, spy.prd, "full", 0, autonomous.ObjectTagSelection{})
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Override *bool                     `json:"branch_refresh_override"`
		Enabled  bool                      `json:"branch_refresh_enabled"`
		State    *autonomous.BranchRefresh `json:"branch_refresh"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Override == nil || !*got.Override || !got.Enabled {
		t.Fatalf("want override true and enabled true, got %+v", got)
	}
	if got.State == nil || got.State.CommitsBehind != 7 || got.State.LastResult != "clean" || got.State.LastMainCommit != "abc123" {
		t.Fatalf("want the refresh state on the payload, got %+v", got.State)
	}
}

func TestBranchRefresh_ConfigPatchKeys(t *testing.T) {
	spy := &autonomousConfigPropagationSpy{}
	s := &Server{cfg: &config.Config{}, cfgPath: t.TempDir() + "/config.yaml", autonomousMgr: spy}
	put := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.handlePutConfig(rec, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader([]byte(body))))
		return rec
	}
	// JSON booleans and numbers.
	if rec := put(`{"autonomous.branch_refresh_enabled": true, "autonomous.branch_refresh_behind_limit": 12}`); rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !s.cfg.Autonomous.BranchRefreshEnabled || s.cfg.Autonomous.BranchRefreshBehindLimit != 12 {
		t.Fatalf("stored values: enabled=%v limit=%d", s.cfg.Autonomous.BranchRefreshEnabled, s.cfg.Autonomous.BranchRefreshBehindLimit)
	}
	var forwarded map[string]json.RawMessage
	if err := json.Unmarshal(spy.lastSetConfig, &forwarded); err != nil {
		t.Fatalf("decode propagated payload: %v", err)
	}
	if string(forwarded["branch_refresh_enabled"]) != "true" || string(forwarded["branch_refresh_behind_limit"]) != "12" {
		t.Fatalf("live manager must get both keys, got %s", spy.lastSetConfig)
	}
	// The string forms the CLI and the comm channel send.
	if rec := put(`{"autonomous.branch_refresh_enabled": "false", "autonomous.branch_refresh_behind_limit": "0"}`); rec.Code != http.StatusOK {
		t.Fatalf("string forms: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if s.cfg.Autonomous.BranchRefreshEnabled || s.cfg.Autonomous.BranchRefreshBehindLimit != 0 {
		t.Fatalf("string forms stored: enabled=%v limit=%d", s.cfg.Autonomous.BranchRefreshEnabled, s.cfg.Autonomous.BranchRefreshBehindLimit)
	}
	// Bad values refuse the whole PUT and change nothing.
	s.cfg.Autonomous.BranchRefreshBehindLimit = 3
	for _, body := range []string{
		`{"autonomous.branch_refresh_enabled": "maybe"}`,
		`{"autonomous.branch_refresh_enabled": 1}`,
		`{"autonomous.branch_refresh_behind_limit": -1}`,
		`{"autonomous.branch_refresh_behind_limit": "five"}`,
		`{"autonomous.branch_refresh_behind_limit": 9, "autonomous.branch_refresh_enabled": "maybe"}`,
	} {
		rec := put(body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d: %s", body, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "autonomous.branch_refresh_") {
			t.Errorf("%s: refusal must name the key, got %q", body, rec.Body.String())
		}
	}
	if s.cfg.Autonomous.BranchRefreshBehindLimit != 3 || s.cfg.Autonomous.BranchRefreshEnabled {
		t.Fatalf("a refused PUT must change nothing: enabled=%v limit=%d", s.cfg.Autonomous.BranchRefreshEnabled, s.cfg.Autonomous.BranchRefreshBehindLimit)
	}
}

func TestBranchRefresh_ConfigGetListsKeys(t *testing.T) {
	s := &Server{cfg: &config.Config{}}
	s.cfg.Autonomous.BranchRefreshEnabled = true
	s.cfg.Autonomous.BranchRefreshBehindLimit = 8
	rec := httptest.NewRecorder()
	s.handleConfig(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Autonomous struct {
			Enabled bool `json:"branch_refresh_enabled"`
			Limit   int  `json:"branch_refresh_behind_limit"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Autonomous.Enabled || got.Autonomous.Limit != 8 {
		t.Fatalf("GET /api/config must list both keys, got %+v", got.Autonomous)
	}
}

func TestBranchRefresh_OpenAPIListsRoute(t *testing.T) {
	for _, p := range []string{"web/openapi.yaml", "../../docs/api/openapi.yaml"} {
		spec, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		for _, op := range BranchRefreshRESTOperations() {
			if !strings.Contains(string(spec), op.Path+":") {
				t.Errorf("%s must document %s %s", p, op.Method, op.Path)
			}
		}
	}
}

type branchRefreshStatsSpy struct {
	branchRefreshSpy
	stats autonomous.BranchRefreshStats
}

func (b *branchRefreshStatsSpy) BranchRefreshStats() autonomous.BranchRefreshStats { return b.stats }

func getBranchRefreshStats(s *Server, method string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handleBranchRefreshStats(rec, httptest.NewRequest(method, "/api/autonomous/branch-refresh/stats", nil))
	return rec
}

func TestBranchRefreshStats_Route(t *testing.T) {
	spy := &branchRefreshStatsSpy{stats: autonomous.BranchRefreshStats{Enabled: true, BehindLimit: 5, Tracked: 3, Clean: 1, Conflict: 1, Error: 1, Stopped: 1, OverrideOn: 2}}
	s := &Server{autonomousMgr: spy, cfg: &config.Config{}}
	rec := getBranchRefreshStats(s, http.MethodGet)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got autonomous.BranchRefreshStats
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got != spy.stats {
		t.Fatalf("got %+v want %+v", got, spy.stats)
	}
	if rec = getBranchRefreshStats(s, http.MethodPost); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: want 405, got %d", rec.Code)
	}
	// A manager without the numbers answers 501; no manager answers 503.
	s2 := &Server{autonomousMgr: &branchRefreshSpy{}, cfg: &config.Config{}}
	if rec = getBranchRefreshStats(s2, http.MethodGet); rec.Code != http.StatusNotImplemented {
		t.Fatalf("no statser: want 501, got %d", rec.Code)
	}
	if rec = getBranchRefreshStats(&Server{cfg: &config.Config{}}, http.MethodGet); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no manager: want 503, got %d", rec.Code)
	}
}
