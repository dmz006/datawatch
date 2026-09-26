package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
)

type prioritySpy struct {
	fakeOrchAutonomous
	id  string
	got int
}

func (p *prioritySpy) SetPRDPriority(id string, n int) (any, error) {
	p.id, p.got = id, n
	return map[string]any{"id": id, "priority": n}, nil
}

func TestHandleCapacity_EmptyAndPopulated(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"pools":[]`) || !strings.Contains(rec.Body.String(), `"waiting":[]`) {
		t.Fatalf("unwired ledger must return empty arrays: %d %s", rec.Code, rec.Body.String())
	}

	l := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	l.SetLimit("node:a", 1)
	_ = l.Acquire(context.Background(), capacity.Request{Holder: "t1", PRDID: "p1", Pools: []string{"node:a"}}, time.Second, nil, nil)
	s.SetCapacity(l)
	rec = httptest.NewRecorder()
	s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
	var st capacity.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Pools) != 1 || st.Pools[0].Limit != 1 || st.Pools[0].Held != 1 || len(st.Leases) != 1 {
		t.Fatalf("unexpected snapshot: %+v", st)
	}
	rec = httptest.NewRecorder()
	s.handleCapacity(rec, httptest.NewRequest(http.MethodPost, "/api/capacity", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST must be 405, got %d", rec.Code)
	}
}

func TestHandleAutonomousPRDs_SetPriority(t *testing.T) {
	spy := &prioritySpy{}
	s := &Server{autonomousMgr: spy}
	rec := httptest.NewRecorder()
	s.handleAutonomousPRDs(rec, httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/abc/set_priority", strings.NewReader(`{"priority":7}`)))
	if rec.Code != 200 || spy.id != "abc" || spy.got != 7 {
		t.Fatalf("set_priority not applied: code=%d id=%q got=%d body=%s", rec.Code, spy.id, spy.got, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	s.handleAutonomousPRDs(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/abc/set_priority", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET must be 405, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.handleAutonomousPRDs(rec, httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/abc/set_priority", strings.NewReader(`nope`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json must be 400, got %d", rec.Code)
	}
}

func TestApplyConfigPatch_CapacityKeys(t *testing.T) {
	cfg := &config.Config{}
	applyConfigPatch(cfg, map[string]interface{}{
		"autonomous.capacity_enabled":                 false,
		"autonomous.capacity_wait_timeout_seconds":    900,
		"autonomous.capacity_gpu_util_pct":            85,
		"session.reserved_interactive":                2,
	})
	if cfg.Autonomous.CapacityEnabled == nil || *cfg.Autonomous.CapacityEnabled {
		t.Fatal("capacity_enabled=false not applied")
	}
	if cfg.Autonomous.CapacityWaitTimeoutSeconds != 900 || cfg.Autonomous.CapacityGPUUtilPct != 85 {
		t.Fatalf("numeric capacity keys not applied: %+v", cfg.Autonomous)
	}
	if cfg.Session.ReservedInteractive == nil || *cfg.Session.ReservedInteractive != 2 {
		t.Fatal("session.reserved_interactive not applied")
	}
	applyConfigPatch(cfg, map[string]interface{}{"autonomous.capacity_gpu_util_pct": 150})
	if cfg.Autonomous.CapacityGPUUtilPct != 85 {
		t.Fatal("gpu util pct above 100 must be rejected")
	}
}

func TestEffectiveReservedInteractiveDefaults(t *testing.T) {
	if got := (config.SessionConfig{MaxSessions: 10}).EffectiveReservedInteractive(); got != 1 {
		t.Fatalf("default with max 10 = %d, want 1", got)
	}
	if got := (config.SessionConfig{MaxSessions: 2}).EffectiveReservedInteractive(); got != 0 {
		t.Fatalf("default with max 2 = %d, want 0", got)
	}
	five := 5
	if got := (config.SessionConfig{MaxSessions: 3, ReservedInteractive: &five}).EffectiveReservedInteractive(); got != 2 {
		t.Fatalf("clamped to max-1 = %d, want 2", got)
	}
}
