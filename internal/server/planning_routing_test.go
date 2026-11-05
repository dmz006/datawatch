package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

func planningRoutingServer(t *testing.T) (*Server, *autonomous.Manager) {
	t.Helper()
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.Config{},
		func(autonomous.DecomposeRequest) (string, error) { return "", nil })
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	s := &Server{
		cfg:           &config.Config{},
		cfgPath:       t.TempDir() + "/config.yaml",
		autonomousMgr: newTestAPI(mgr),
	}
	return s, mgr
}

func planningRoutingDo(t *testing.T, h http.HandlerFunc, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr
}

const planningRoutingValidBody = `{
  "enabled": true,
  "shadow": false,
  "rules": [{"name":"sec","match":{"tags":["security"]},"planners":[{"backend":"claude-code","model":"opus"},{"backend":"pi-prompt","model":"qwen"}],"ladder":"strict"}],
  "ladders": {"strict":{"touch_up_max":2,"replan_max":1,"escalate_after_attempts":3}}
}`

func TestPlanningRoutingGetDefaults(t *testing.T) {
	s, _ := planningRoutingServer(t)
	rr := planningRoutingDo(t, s.handleAutonomousPlanningRouting, http.MethodGet, "/api/autonomous/planning-routing", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET = %d: %s", rr.Code, rr.Body.String())
	}
	var v planningRoutingView
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Enabled || v.Shadow {
		t.Errorf("routing must be off by default: %+v", v)
	}
	if !v.Validation.Valid {
		t.Errorf("default config must validate: %+v", v.Validation)
	}
	if _, ok := v.Ladders["default"]; !ok {
		t.Error("the built-in default ladder must show")
	}
	if _, ok := v.Classes["security"]; !ok {
		t.Error("the built-in security class must show")
	}
	if v.Rules == nil {
		t.Error("rules must be [] not null")
	}
}

func TestPlanningRoutingPutValidPersistsAndReads(t *testing.T) {
	s, mgr := planningRoutingServer(t)
	rr := planningRoutingDo(t, s.handleAutonomousPlanningRouting, http.MethodPut, "/api/autonomous/planning-routing", planningRoutingValidBody)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT = %d: %s", rr.Code, rr.Body.String())
	}
	live := mgr.Config().PlanningRouting
	if !live.Enabled || len(live.Rules) != 1 || live.Rules[0].Name != "sec" {
		t.Errorf("live manager config not updated: %+v", live)
	}
	if live.Ladders["strict"].TouchUpMax != 2 {
		t.Errorf("ladder not applied: %+v", live.Ladders)
	}
	persisted := s.cfg.Autonomous.PlanningRouting
	if !persisted.Enabled || len(persisted.Rules) != 1 {
		t.Errorf("YAML-facing config not updated: %+v", persisted)
	}
	rr = planningRoutingDo(t, s.handleAutonomousPlanningRouting, http.MethodGet, "/api/autonomous/planning-routing", "")
	var v planningRoutingView
	_ = json.Unmarshal(rr.Body.Bytes(), &v)
	if !v.Enabled || !v.Validation.Valid || len(v.Rules) != 1 {
		t.Errorf("readback after PUT wrong: %+v", v)
	}

	// A second PUT replaces: the ladder it leaves out is gone.
	rr = planningRoutingDo(t, s.handleAutonomousPlanningRouting, http.MethodPut, "/api/autonomous/planning-routing", `{"enabled":false,"shadow":true}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("second PUT = %d: %s", rr.Code, rr.Body.String())
	}
	live = mgr.Config().PlanningRouting
	if live.Enabled || !live.Shadow || len(live.Rules) != 0 {
		t.Errorf("replace did not apply: %+v", live)
	}
	if _, ok := live.Ladders["strict"]; ok {
		t.Error("PUT must replace ladders, strict should be gone")
	}
}

func TestPlanningRoutingPutInvalidBodiesRejected(t *testing.T) {
	cases := map[string]string{
		"not json":       `{`,
		"unknown field":  `{"enabeld":true}`,
		"wrong type":     `{"enabled":"yes"}`,
		"no planners":    `{"rules":[{"name":"a","match":{},"planners":[]}]}`,
		"empty backend":  `{"rules":[{"name":"a","match":{},"planners":[{"backend":"","model":"m"}]}]}`,
		"duplicate name": `{"rules":[{"name":"a","planners":[{"backend":"b","model":"m"}]},{"name":"a","planners":[{"backend":"b","model":"m"}]}]}`,
		"unknown ladder": `{"rules":[{"name":"a","planners":[{"backend":"b","model":"m"}],"ladder":"nope"}]}`,
		"unknown class":  `{"rules":[{"name":"a","match":{"risk_class":["nope"]},"planners":[{"backend":"b","model":"m"}]}]}`,
		"size min>max":   `{"rules":[{"name":"a","match":{"spec_size_min":10,"spec_size_max":5},"planners":[{"backend":"b","model":"m"}]}]}`,
		"negative count": `{"ladders":{"x":{"touch_up_max":-1}}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			s, mgr := planningRoutingServer(t)
			rr := planningRoutingDo(t, s.handleAutonomousPlanningRouting, http.MethodPut, "/api/autonomous/planning-routing", body)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("PUT = %d, want 400: %s", rr.Code, rr.Body.String())
			}
			if live := mgr.Config().PlanningRouting; live.Enabled || len(live.Rules) != 0 {
				t.Errorf("a rejected PUT must change nothing: %+v", live)
			}
			if len(s.cfg.Autonomous.PlanningRouting.Rules) != 0 {
				t.Error("a rejected PUT must not touch the persisted config")
			}
		})
	}
}

func TestPlanningRoutingMethodAndDisabledManager(t *testing.T) {
	s, _ := planningRoutingServer(t)
	for target, h := range map[string]http.HandlerFunc{
		"/api/autonomous/planning-routing":           s.handleAutonomousPlanningRouting,
		"/api/autonomous/planning-routing/test":      s.handleAutonomousPlanningRoutingTest,
		"/api/autonomous/planning-routing/scorecard": s.handleAutonomousPlanningScorecard,
	} {
		if rr := planningRoutingDo(t, h, http.MethodDelete, target, ""); rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("DELETE %s = %d, want 405", target, rr.Code)
		}
	}
	off := &Server{}
	for target, h := range map[string]http.HandlerFunc{
		"/api/autonomous/planning-routing":           off.handleAutonomousPlanningRouting,
		"/api/autonomous/planning-routing/test":      off.handleAutonomousPlanningRoutingTest,
		"/api/autonomous/planning-routing/scorecard": off.handleAutonomousPlanningScorecard,
	} {
		if rr := planningRoutingDo(t, h, http.MethodGet, target, ""); rr.Code != http.StatusServiceUnavailable {
			t.Errorf("%s with autonomous off = %d, want 503", target, rr.Code)
		}
	}
}

func planningRoutingTestCall(t *testing.T, s *Server, body string) planningRouteDecisionView {
	t.Helper()
	rr := planningRoutingDo(t, s.handleAutonomousPlanningRoutingTest, http.MethodPost, "/api/autonomous/planning-routing/test", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("test = %d: %s", rr.Code, rr.Body.String())
	}
	var v planningRouteDecisionView
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestPlanningRoutingTestEndpointDisabledThenEnabled(t *testing.T) {
	s, _ := planningRoutingServer(t)
	const in = `{"kind":"feature","tags":["security"],"spec_bytes":500,"files":["a.go"],"project":"p"}`

	d := planningRoutingTestCall(t, s, in)
	if d.Enabled || !d.Fallback || d.Reason != autonomous.RouteReasonDisabled || len(d.Planners) != 0 {
		t.Errorf("disabled routing must fall back: %+v", d)
	}
	if d.Classes == nil || d.Planners == nil {
		t.Error("classes and planners must be [] not null")
	}

	if rr := planningRoutingDo(t, s.handleAutonomousPlanningRouting, http.MethodPut, "/api/autonomous/planning-routing", planningRoutingValidBody); rr.Code != http.StatusOK {
		t.Fatalf("PUT = %d: %s", rr.Code, rr.Body.String())
	}
	d = planningRoutingTestCall(t, s, in)
	if !d.Enabled || d.Fallback || d.Rule != "sec" || d.Reason != autonomous.RouteReasonRuleMatched {
		t.Errorf("enabled routing must match the rule: %+v", d)
	}
	if len(d.Planners) != 2 || d.Planners[0].Backend != "claude-code" || d.Ladder != "strict" {
		t.Errorf("planners/ladder wrong: %+v", d)
	}

	d = planningRoutingTestCall(t, s, `{"kind":"feature","tags":["docs"]}`)
	if !d.Fallback || d.Reason != autonomous.RouteReasonNoMatch {
		t.Errorf("no matching rule must fall back: %+v", d)
	}

	// Shadow: the decision is computed and flagged, the planner is unchanged.
	if rr := planningRoutingDo(t, s.handleAutonomousPlanningRouting, http.MethodPut, "/api/autonomous/planning-routing",
		strings.Replace(planningRoutingValidBody, `"shadow": false`, `"shadow": true`, 1)); rr.Code != http.StatusOK {
		t.Fatalf("PUT = %d: %s", rr.Code, rr.Body.String())
	}
	d = planningRoutingTestCall(t, s, in)
	if !d.Shadow || d.Rule != "sec" {
		t.Errorf("shadow decision wrong: %+v", d)
	}
}

func TestPlanningRoutingTestEndpointBadInput(t *testing.T) {
	s, _ := planningRoutingServer(t)
	for name, body := range map[string]string{"not json": `{`, "negative size": `{"spec_bytes":-1}`} {
		rr := planningRoutingDo(t, s.handleAutonomousPlanningRoutingTest, http.MethodPost, "/api/autonomous/planning-routing/test", body)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, rr.Code)
		}
	}
}

func TestPlanningRoutingScorecardWindowParameter(t *testing.T) {
	s, mgr := planningRoutingServer(t)
	now := time.Now()
	for _, age := range []time.Duration{time.Hour, 10 * 24 * time.Hour} {
		p, err := mgr.Store().CreatePRD("spec", "", "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		p.PlanningRecord = &autonomous.PlanningRecord{
			RuleName: "sec", Classes: []string{"security"}, PlannerBackend: "claude-code", PlannerModel: "opus",
			RecordedAt: now.Add(-age),
			Attempts:   []autonomous.AttemptRecord{{Kind: autonomous.LadderAttemptPlan, Outcome: autonomous.LadderOutcomePassed, Backend: "claude-code", Model: "opus"}},
		}
		if err := mgr.Store().SavePRD(p); err != nil {
			t.Fatal(err)
		}
	}
	decomposes := func(window string) (int, string) {
		t.Helper()
		target := "/api/autonomous/planning-routing/scorecard"
		if window != "" {
			target += "?window=" + window
		}
		rr := planningRoutingDo(t, s.handleAutonomousPlanningScorecard, http.MethodGet, target, "")
		if rr.Code != http.StatusOK {
			t.Fatalf("window %q = %d: %s", window, rr.Code, rr.Body.String())
		}
		var out struct {
			Window string `json:"window"`
			Gate   struct {
				Decomposes int `json:"decomposes"`
			} `json:"gate"`
			Report string `json:"report"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.Report, "Planning gate report") {
			t.Errorf("report text missing: %q", out.Report)
		}
		return out.Gate.Decomposes, out.Window
	}
	if n, w := decomposes("24h"); n != 1 || w != "24h0m0s" {
		t.Errorf("24h window: decomposes=%d window=%s, want 1 and 24h0m0s", n, w)
	}
	if n, _ := decomposes("30d"); n != 2 {
		t.Errorf("30d window: decomposes=%d, want 2", n)
	}
	if n, w := decomposes(""); n != 1 || w != "168h0m0s" {
		t.Errorf("default window must be 7d: decomposes=%d window=%s", n, w)
	}
	if n, _ := decomposes("0"); n != 2 {
		t.Errorf("window 0 means all records: decomposes=%d, want 2", n)
	}
	for _, bad := range []string{"abc", "-5h", "xd"} {
		rr := planningRoutingDo(t, s.handleAutonomousPlanningScorecard, http.MethodGet, "/api/autonomous/planning-routing/scorecard?window="+bad, "")
		if rr.Code != http.StatusBadRequest {
			t.Errorf("window %q = %d, want 400", bad, rr.Code)
		}
	}
}

func TestPlanningRoutingApplyConfigPatchKeys(t *testing.T) {
	cfg := &config.Config{}
	skipped := applyConfigPatch(cfg, map[string]interface{}{
		"autonomous.planning_routing.enabled": true,
		"autonomous.planning_routing.shadow":  true,
	})
	if len(skipped) != 0 {
		t.Errorf("skipped = %v", skipped)
	}
	if !cfg.Autonomous.PlanningRouting.Enabled || !cfg.Autonomous.PlanningRouting.Shadow {
		t.Errorf("patch keys not applied: %+v", cfg.Autonomous.PlanningRouting)
	}
	applyConfigPatch(cfg, map[string]interface{}{"autonomous.planning_routing.enabled": false})
	if cfg.Autonomous.PlanningRouting.Enabled || !cfg.Autonomous.PlanningRouting.Shadow {
		t.Errorf("enabled=false must not touch shadow: %+v", cfg.Autonomous.PlanningRouting)
	}
}

func TestPlanningRoutingParseWindow(t *testing.T) {
	cases := map[string]time.Duration{"": 168 * time.Hour, "36h": 36 * time.Hour, "7d": 168 * time.Hour, "0": 0, " 2d ": 48 * time.Hour}
	for in, want := range cases {
		got, err := parsePlanningWindow(in)
		if err != nil || got != want {
			t.Errorf("parsePlanningWindow(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
}
