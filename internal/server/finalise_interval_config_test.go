package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func newFinaliseIntervalServer(t *testing.T) *Server {
	t.Helper()
	return &Server{
		cfg:     config.DefaultConfig(),
		cfgPath: filepath.Join(t.TempDir(), "config.yaml"),
	}
}

func finaliseIntervalPut(s *Server, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	return rr
}

func finaliseIntervalGet(t *testing.T, s *Server) int {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: %d", rr.Code)
	}
	var got struct {
		Session struct {
			Interval *int `json:"finalise_interval_minutes"`
		} `json:"session"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Session.Interval == nil {
		t.Fatalf("session map lacks finalise_interval_minutes: %s", rr.Body.String())
	}
	return *got.Session.Interval
}

func TestGetConfigReportsFinaliseInterval(t *testing.T) {
	s := newFinaliseIntervalServer(t)
	if got := finaliseIntervalGet(t, s); got != 10 {
		t.Fatalf("default finalise_interval_minutes=%d, want 10", got)
	}
	s.cfg.Session.FinaliseIntervalMinutes = 37
	if got := finaliseIntervalGet(t, s); got != 37 {
		t.Fatalf("finalise_interval_minutes=%d, want 37", got)
	}
}

func TestPutConfigSetsFinaliseInterval(t *testing.T) {
	s := newFinaliseIntervalServer(t)
	if rr := finaliseIntervalPut(s, map[string]any{"session.finalise_interval_minutes": 25}); rr.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rr.Code, rr.Body.String())
	}
	if got := finaliseIntervalGet(t, s); got != 25 {
		t.Fatalf("after PUT, GET reports %d, want 25", got)
	}
	if got := s.cfg.Session.FinaliseIntervalMinutes; got != 25 {
		t.Fatalf("live config holds %d, want 25", got)
	}
	// Zero switches scheduled runs off and is a valid value.
	if rr := finaliseIntervalPut(s, map[string]any{"session.finalise_interval_minutes": 0}); rr.Code != http.StatusOK {
		t.Fatalf("PUT 0: %d %s", rr.Code, rr.Body.String())
	}
	if got := finaliseIntervalGet(t, s); got != 0 {
		t.Fatalf("after PUT 0, GET reports %d, want 0", got)
	}
	loaded, err := config.Load(s.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Session.FinaliseIntervalMinutes; got != 0 {
		t.Fatalf("saved config holds %d, want 0", got)
	}
}

func TestPutConfigIgnoresNegativeFinaliseInterval(t *testing.T) {
	s := newFinaliseIntervalServer(t)
	if rr := finaliseIntervalPut(s, map[string]any{"session.finalise_interval_minutes": 25}); rr.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rr.Code, rr.Body.String())
	}
	if rr := finaliseIntervalPut(s, map[string]any{"session.finalise_interval_minutes": -3}); rr.Code != http.StatusOK {
		t.Fatalf("PUT -3: %d %s", rr.Code, rr.Body.String())
	}
	if got := finaliseIntervalGet(t, s); got != 25 {
		t.Fatalf("negative value changed the setting to %d, want it to stay 25", got)
	}
}
