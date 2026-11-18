package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// laneUtilSpy records the arguments the route passes to the read model.
type laneUtilSpy struct {
	fakeOrchAutonomous
	calls   int
	window  time.Duration
	buckets int
	now     time.Time
	result  autonomous.LaneUtilization
}

func (l *laneUtilSpy) LaneUtilization(w time.Duration, b int, now time.Time) autonomous.LaneUtilization {
	l.calls++
	l.window, l.buckets, l.now = w, b, now
	return l.result
}

var laneUtilFixedNow = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

func laneUtilFixture(t *testing.T) (*Server, *laneUtilSpy) {
	t.Helper()
	old := laneUtilizationNow
	laneUtilizationNow = func() time.Time { return laneUtilFixedNow }
	t.Cleanup(func() { laneUtilizationNow = old })
	spy := &laneUtilSpy{}
	return &Server{autonomousMgr: spy}, spy
}

func getLaneUtil(s *Server, method, query string, caps []string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/autonomous/lane-utilization"+query, nil)
	if caps != nil {
		req = req.WithContext(context.WithValue(req.Context(), sessionCapsKey, caps))
	}
	rec := httptest.NewRecorder()
	s.handleAutonomousLaneUtilization(rec, req)
	return rec
}

func TestLaneUtilizationDefaults(t *testing.T) {
	s, spy := laneUtilFixture(t)
	spy.result = autonomous.LaneUtilization{BucketCount: 24, Lanes: []autonomous.LaneUtilizationLane{{ID: "gpu-1"}}}
	rec := getLaneUtil(s, http.MethodGet, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if spy.window != 24*time.Hour || spy.buckets != autonomous.LaneUtilizationDefaultBuckets || !spy.now.Equal(laneUtilFixedNow) {
		t.Errorf("defaults: window=%s buckets=%d now=%s", spy.window, spy.buckets, spy.now)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, k := range []string{"window_start", "lanes", "backend_mix", "lane_fill", "plan_usage"} {
		if _, ok := got[k]; !ok {
			t.Errorf("response lacks %q", k)
		}
	}
}

func TestLaneUtilizationCustomParams(t *testing.T) {
	cases := map[string]time.Duration{"6h": 6 * time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "90m": 90 * time.Minute}
	for in, want := range cases {
		s, spy := laneUtilFixture(t)
		rec := getLaneUtil(s, http.MethodGet, "?window="+in+"&bucket=12&decisions=5", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("window %s: want 200, got %d: %s", in, rec.Code, rec.Body.String())
		}
		if spy.window != want || spy.buckets != 12 {
			t.Errorf("window %s: got window=%s buckets=%d", in, spy.window, spy.buckets)
		}
	}
}

func TestLaneUtilizationDecisionsTrim(t *testing.T) {
	s, spy := laneUtilFixture(t)
	for i := 0; i < 10; i++ {
		spy.result.LaneFill.Decisions = append(spy.result.LaneFill.Decisions, autonomous.LaneFillDecision{Title: "d"})
	}
	rec := getLaneUtil(s, http.MethodGet, "?decisions=3", nil)
	var got autonomous.LaneUtilization
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got.LaneFill.Decisions) != 3 {
		t.Fatalf("want 3 decisions, got %d (%v)", len(got.LaneFill.Decisions), err)
	}
}

func TestLaneUtilizationBadParams(t *testing.T) {
	bad := map[string]string{
		"window=banana":      "window",
		"window=":            "window",
		"window=30m":         "window",
		"window=9999h":       "window",
		"window=0d":          "window",
		"window=-6h":         "window",
		"bucket=0":           "bucket",
		"bucket=289":         "bucket",
		"bucket=abc":         "bucket",
		"bucket=1.5":         "bucket",
		"decisions=0":        "decisions",
		"decisions=21":       "decisions",
		"decisions=many":     "decisions",
		"window=6h&bucket=x": "bucket",
	}
	for q, field := range bad {
		s, spy := laneUtilFixture(t)
		rec := getLaneUtil(s, http.MethodGet, "?"+q, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", q, rec.Code)
			continue
		}
		if !strings.Contains(rec.Body.String(), field) {
			t.Errorf("%s: message %q does not name %s", q, rec.Body.String(), field)
		}
		if spy.calls != 0 {
			t.Errorf("%s: read model called on bad input", q)
		}
	}
}

func TestLaneUtilizationWrongMethod(t *testing.T) {
	s, spy := laneUtilFixture(t)
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		if rec := getLaneUtil(s, m, "", nil); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: want 405, got %d", m, rec.Code)
		}
	}
	if spy.calls != 0 {
		t.Error("read model called for a non-GET")
	}
}

func TestLaneUtilizationAutonomousDisabled(t *testing.T) {
	s := &Server{}
	if rec := getLaneUtil(s, http.MethodGet, "", nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}
	// Disabled wins over a wrong method, as for lane-fill.
	if rec := getLaneUtil(s, http.MethodPost, "", nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST while disabled: want 503, got %d", rec.Code)
	}
}

func TestLaneUtilizationFederationDenied(t *testing.T) {
	s, spy := laneUtilFixture(t)
	rec := getLaneUtil(s, http.MethodGet, "", []string{federation.CapHealthRead})
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), federation.CapAutonomousRead) {
		t.Fatalf("want 403 naming the capability, got %d %q", rec.Code, rec.Body.String())
	}
	if spy.calls != 0 {
		t.Error("read model called without the capability")
	}
	rec = getLaneUtil(s, http.MethodGet, "", []string{federation.CapAutonomousRead})
	if rec.Code != http.StatusOK {
		t.Fatalf("with the capability: want 200, got %d", rec.Code)
	}
}

func TestLaneUtilizationMCPToolCap(t *testing.T) {
	if got := federation.MCPToolCap["autonomous_lane_utilization"]; got != federation.CapAutonomousRead {
		t.Errorf("autonomous_lane_utilization = %q, want %q", got, federation.CapAutonomousRead)
	}
}
