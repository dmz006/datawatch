package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func burnGuardConfigServer(t *testing.T) *Server {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.DataDir = t.TempDir()
	return &Server{hostname: "testhost", cfg: cfg, cfgPath: filepath.Join(t.TempDir(), "config.yaml")}
}

func doBurnGuard(s *Server, method, path, body string, h http.HandlerFunc, caps ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if len(caps) > 0 {
		req = withUsageCaps(req, caps...)
	}
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr
}

func TestBurnGuardConfigGetAndPut(t *testing.T) {
	s := burnGuardConfigServer(t)
	rr := doBurnGuard(s, http.MethodGet, "/api/usage/burn-guard/config", "", s.handleBurnGuardConfig)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET = %d: %s", rr.Code, rr.Body.String())
	}
	var got config.BurnGuardConfig
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil || got.IntervalMinutes != config.BurnGuardIntervalMinutesDefault {
		t.Fatalf("GET body = %s (%v)", rr.Body.String(), err)
	}

	rr = doBurnGuard(s, http.MethodPut, "/api/usage/burn-guard/config",
		`{"margin_points": 3.5, "interval_minutes": 30, "enabled": false, "ladder": ["lower_inflight","hold_claude"], "retry_limit_claude": 0}`, s.handleBurnGuardConfig)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT = %d: %s", rr.Code, rr.Body.String())
	}
	bg := s.cfg.Usage.BurnGuard
	if bg.MarginPoints != 3.5 || bg.IntervalMinutes != 30 || bg.Enabled || bg.RetryLimitClaude != 0 ||
		len(bg.Ladder) != 2 || bg.Ladder[1] != config.BurnGuardRungHoldClaude {
		t.Fatalf("stored = %+v", bg)
	}
	// A prefixed key is accepted the same way.
	rr = doBurnGuard(s, http.MethodPut, "/api/usage/burn-guard/config", `{"usage.burn_guard.step_back": false}`, s.handleBurnGuardConfig)
	if rr.Code != http.StatusOK || s.cfg.Usage.BurnGuard.StepBack {
		t.Fatalf("prefixed PUT = %d step_back=%v", rr.Code, s.cfg.Usage.BurnGuard.StepBack)
	}
}

func TestBurnGuardConfigPutRejectsBadValuesAndStoresNothing(t *testing.T) {
	for name, body := range map[string]string{
		"interval too big":  `{"interval_minutes": 5000}`,
		"interval fraction": `{"interval_minutes": 1.5}`,
		"margin negative":   `{"margin_points": -1}`,
		"unknown rung":      `{"ladder": ["kill_sessions"]}`,
		"empty ladder":      `{"ladder": []}`,
		"retry too big":     `{"retry_limit_claude": 21}`,
		"price zero":        `{"cache_write_points_per_million": 0}`,
		"not a bool":        `{"enabled": "maybe"}`,
		"unknown key":       `{"nope": 1}`,
		"effort ceiling":    `{"effort_ceiling": {"docs": "low"}}`,
		"good plus bad":     `{"margin_points": 9, "interval_minutes": 0}`,
		"empty body":        `{}`,
	} {
		s := burnGuardConfigServer(t)
		before := s.cfg.Usage.BurnGuard
		rr := doBurnGuard(s, http.MethodPut, "/api/usage/burn-guard/config", body, s.handleBurnGuardConfig)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: PUT = %d (%s), want 400", name, rr.Code, rr.Body.String())
		}
		if s.cfg.Usage.BurnGuard.MarginPoints != before.MarginPoints || s.cfg.Usage.BurnGuard.IntervalMinutes != before.IntervalMinutes {
			t.Errorf("%s: a rejected PUT stored something", name)
		}
	}
}

func TestBurnGuardConfigAndRunCapabilityGates(t *testing.T) {
	s := burnGuardConfigServer(t)
	if rr := doBurnGuard(s, http.MethodGet, "/x", "", s.handleBurnGuardConfig, usageCapabilityWrite); rr.Code != http.StatusForbidden {
		t.Errorf("GET config with only usage:write = %d, want 403", rr.Code)
	}
	if rr := doBurnGuard(s, http.MethodPut, "/x", `{"margin_points":1}`, s.handleBurnGuardConfig, usageCapabilityRead); rr.Code != http.StatusForbidden {
		t.Errorf("PUT config with only usage:read = %d, want 403", rr.Code)
	}
	if rr := doBurnGuard(s, http.MethodPost, "/x", "", s.handleBurnGuardRun, usageCapabilityRead); rr.Code != http.StatusForbidden {
		t.Errorf("POST run with only usage:read = %d, want 403", rr.Code)
	}
	if rr := doBurnGuard(s, http.MethodGet, "/x", "", s.handleBurnGuardStatus, usageCapabilityWrite); rr.Code != http.StatusForbidden {
		t.Errorf("GET status with only usage:write = %d, want 403", rr.Code)
	}
	if rr := doBurnGuard(s, http.MethodDelete, "/x", "", s.handleBurnGuardConfig); rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE config = %d, want 405", rr.Code)
	}
}

func TestBurnGuardRunEndpoint(t *testing.T) {
	s := burnGuardConfigServer(t)
	if rr := doBurnGuard(s, http.MethodPost, "/api/usage/burn-guard/run", "", s.handleBurnGuardRun); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("unwired run = %d, want 503", rr.Code)
	}
	if rr := doBurnGuard(s, http.MethodGet, "/api/usage/burn-guard/run", "", s.handleBurnGuardRun); rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET run = %d, want 405", rr.Code)
	}

	cfg := rungFixtureConfig()
	g, adm, _, sessions := burnGuardRig(t, &cfg)
	s.burnGuard = g
	rr := doBurnGuard(s, http.MethodPost, "/api/usage/burn-guard/run", "", s.handleBurnGuardRun)
	if rr.Code != http.StatusOK {
		t.Fatalf("run = %d: %s", rr.Code, rr.Body.String())
	}
	var out struct {
		Decision map[string]any  `json:"decision"`
		Status   BurnGuardStatus `json:"status"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Decision["direction"] == nil || out.Status.LastDecision == nil || out.Status.Counters.Ticks < 1 {
		t.Fatalf("run body = %s", rr.Body.String())
	}
	if len(adm.calls) == 0 && out.Status.Level > 0 {
		t.Errorf("a rung is in force but admission was never touched")
	}
	sessions.assertNoEnd(t)
}

func TestBurnGuardStatusCarriesRungLineAndMargin(t *testing.T) {
	s := &Server{}
	cfg := rungFixtureConfig()
	g, _, _, _ := burnGuardRig(t, &cfg)
	g.Tick(context.Background())
	s.burnGuard = g
	rr := doBurnGuard(s, http.MethodGet, "/api/usage/burn-guard", "", s.handleBurnGuardStatus)
	var st BurnGuardStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Line == nil || st.Margin == nil || st.LastProjection == nil {
		t.Fatalf("status = %s", rr.Body.String())
	}
	if *st.Line != st.LastProjection.Line || *st.Margin != st.LastProjection.Margin {
		t.Errorf("line/margin %v/%v differ from the projection %v/%v", *st.Line, *st.Margin, st.LastProjection.Line, st.LastProjection.Margin)
	}
	if st.Level > 0 && st.Rung != st.Applied[len(st.Applied)-1] {
		t.Errorf("rung = %q, applied = %v", st.Rung, st.Applied)
	}
}

// GET /api/config carries the usage.burn_guard block so the Settings page and every
// other reader of the config see the live values.
func TestGetConfigCarriesBurnGuard(t *testing.T) {
	s := burnGuardConfigServer(t)
	s.cfg.Usage.BurnGuard.MarginPoints = 4.5
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config = %d", rr.Code)
	}
	var got struct {
		Usage struct {
			BurnGuard config.BurnGuardConfig `json:"burn_guard"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Usage.BurnGuard.MarginPoints != 4.5 {
		t.Errorf("usage.burn_guard.margin_points = %v, want 4.5", got.Usage.BurnGuard.MarginPoints)
	}
}
