package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func newLivenessServer(t *testing.T) *Server {
	t.Helper()
	return &Server{
		cfg:               config.DefaultConfig(),
		cfgPath:           filepath.Join(t.TempDir(), "config.yaml"),
		availableBackends: []string{"claude-code", "pi", "pi-prompt", "goose-prompt"},
	}
}

func livenessPut(s *Server, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	return rr
}

func livenessGet(t *testing.T, s *Server) (window int, overrides map[string]int) {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: %d", rr.Code)
	}
	var got struct {
		Session struct {
			Window    *int           `json:"liveness_window_seconds"`
			Overrides map[string]int `json:"liveness_idle_timeout_overrides"`
		} `json:"session"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Session.Window == nil {
		t.Fatalf("session map lacks liveness_window_seconds: %s", rr.Body.String())
	}
	return *got.Session.Window, got.Session.Overrides
}

func TestLivenessConfigAPIRoundTrip(t *testing.T) {
	s := newLivenessServer(t)
	rr := livenessPut(s, map[string]any{
		"session.liveness_window_seconds":         90,
		"session.liveness_idle_timeout_overrides": map[string]any{"pi": 0, "goose-prompt": 30},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rr.Code, rr.Body.String())
	}
	w, o := livenessGet(t, s)
	if w != 90 || o["pi"] != 0 || o["goose-prompt"] != 30 || len(o) != 2 {
		t.Fatalf("round trip: window=%d overrides=%v", w, o)
	}
	// Persisted to the YAML file too.
	loaded, err := config.Load(s.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Session.LivenessWindowSeconds != 90 || loaded.Session.LivenessIdleTimeoutOverrides["goose-prompt"] != 30 {
		t.Fatalf("saved config: %+v", loaded.Session)
	}
}

func TestLivenessConfigAPIZeroReadsBackAsUnset(t *testing.T) {
	s := newLivenessServer(t)
	livenessPut(s, map[string]any{"session.liveness_window_seconds": 90})
	if rr := livenessPut(s, map[string]any{"session.liveness_window_seconds": 0}); rr.Code != http.StatusOK {
		t.Fatalf("PUT 0: %d %s", rr.Code, rr.Body.String())
	}
	if w, _ := livenessGet(t, s); w != 0 {
		t.Errorf("window = %d, want 0 (unset)", w)
	}
	if got := s.cfg.LivenessWindowSecondsEffective(); got != config.DefaultLivenessWindowSeconds {
		t.Errorf("effective = %d, want default %d", got, config.DefaultLivenessWindowSeconds)
	}
}

func TestLivenessConfigAPINestedKeyForm(t *testing.T) {
	s := newLivenessServer(t)
	rr := livenessPut(s, map[string]any{"session.liveness_idle_timeout_overrides.pi-prompt": 15})
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT nested: %d %s", rr.Code, rr.Body.String())
	}
	if _, o := livenessGet(t, s); o["pi-prompt"] != 15 {
		t.Fatalf("overrides = %v", o)
	}
}

func TestLivenessConfigAPIRefusals(t *testing.T) {
	s := newLivenessServer(t)
	cases := map[string]struct {
		body map[string]any
		want string
	}{
		"unknown backend":   {map[string]any{"session.liveness_idle_timeout_overrides": map[string]any{"nope": 5}}, `"nope"`},
		"negative override": {map[string]any{"session.liveness_idle_timeout_overrides": map[string]any{"pi": -1}}, "pi"},
		"negative window":   {map[string]any{"session.liveness_window_seconds": -5}, "liveness_window_seconds"},
		"fractional window": {map[string]any{"session.liveness_window_seconds": 1.5}, "liveness_window_seconds"},
		"non-object":        {map[string]any{"session.liveness_idle_timeout_overrides": "pi=5"}, "object"},
	}
	for name, tc := range cases {
		rr := livenessPut(s, tc.body)
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), tc.want) {
			t.Errorf("%s: code=%d body=%q, want 400 containing %q", name, rr.Code, rr.Body.String(), tc.want)
		}
	}
	if w, o := livenessGet(t, s); w != 0 || len(o) != 0 {
		t.Errorf("refused PUTs must not change config: window=%d overrides=%v", w, o)
	}
}
