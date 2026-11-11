package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
)

// seriesT0 is a fixed clock: 12:30 UTC, so the current hour is 12:00-12:30.
var seriesT0 = time.Date(2026, 11, 10, 12, 30, 0, 0, time.UTC)

func seriesAt(m int) time.Time { return seriesT0.Add(time.Duration(m) * time.Minute) }

func seriesLaneBusy(lane string, from, to time.Time) capacity.LaneInterval {
	return capacity.LaneInterval{Kind: capacity.LaneIntervalBusy, Holder: "task", Pools: []string{"llm:" + lane}, Node: "n1", Start: from, End: to}
}

func TestDashboardSeries_UtilizationIsBusyShareOfHour(t *testing.T) {
	first := seriesT0.Truncate(time.Hour).Add(-time.Hour) // 11:00
	ivs := []capacity.LaneInterval{
		seriesLaneBusy("claude-code", seriesAt(-50), seriesAt(-15)), // 11:40-12:15 spans the 11:00 and 12:00 hours
		// Two more leases overlap that span inside the current hour; the union counts once.
		seriesLaneBusy("claude-code", seriesAt(-28), seriesAt(-20)),
		seriesLaneBusy("claude-code", seriesAt(-25), seriesAt(-15)),
	}
	lanes := buildLaneUtilization(ivs, nil, first, 2, seriesT0)
	if len(lanes) != 1 || lanes[0].Lane != "claude-code" || len(lanes[0].Hours) != 2 {
		t.Fatalf("lanes: %+v", lanes)
	}
	h11, h12 := lanes[0].Hours[0], lanes[0].Hours[1]
	// 11:00 hour: busy 11:40-11:59:59 = 20 minutes of 60.
	if h11.BusyFraction == nil || !approx(*h11.BusyFraction, 20.0/60.0) {
		t.Fatalf("11:00 fraction %v, want 1/3", h11.BusyFraction)
	}
	// 12:00 hour so far: busy 12:00-12:15 = 15 min, over the 30 min elapsed.
	if h12.BusyFraction == nil || !approx(*h12.BusyFraction, 0.5) {
		t.Fatalf("12:00 fraction %v, want 0.5 of the elapsed half hour", h12.BusyFraction)
	}
}

func TestDashboardSeries_UtilizationIgnoresWaitingAndNullBeforeRecording(t *testing.T) {
	first := seriesT0.Truncate(time.Hour).Add(-time.Hour)
	recording := seriesT0.Truncate(time.Hour) // history began at 12:00
	ivs := []capacity.LaneInterval{
		{Kind: capacity.LaneIntervalWaiting, Pools: []string{"llm:claude-code"}, Start: seriesAt(-40), End: seriesAt(-10)},
		seriesLaneBusy("claude-code", seriesAt(-40), seriesAt(-10)), // busy 11:50-12:20, but 11:00 predates history
	}
	lanes := buildLaneUtilization(ivs, &recording, first, 2, seriesT0)
	if len(lanes) != 1 {
		t.Fatalf("lanes: %+v", lanes)
	}
	if lanes[0].Hours[0].BusyFraction != nil {
		t.Fatalf("11:00 hour predates recording and must be null, got %v", *lanes[0].Hours[0].BusyFraction)
	}
	if got := lanes[0].Hours[1].BusyFraction; got == nil || !approx(*got, 20.0/30.0) {
		t.Fatalf("12:00 fraction %v, want 20 of 30 elapsed minutes", got)
	}
}

func TestDashboardSeries_UtilizationEmptyWithoutLedger(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleDashboardSeriesLaneUtilization(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard/series/lane-utilization", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body struct {
		BucketSeconds int64 `json:"bucket_seconds"`
		Lanes         []any `json:"lanes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Lanes) != 0 || body.BucketSeconds != 3600 {
		t.Fatalf("empty body: %+v", body)
	}
}

func TestDashboardSeries_UtilizationFromReleasedLease(t *testing.T) {
	s := &Server{}
	l := capacity.New(capacity.Options{})
	s.SetCapacity(l)
	if err := l.Acquire(context.Background(), capacity.Request{Holder: "task-a", PRDID: "prd-a", Pools: []string{"llm:claude-code"}, Node: "n1"}, 0, nil, nil); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	l.Release("task-a")

	rec := httptest.NewRecorder()
	s.handleDashboardSeriesLaneUtilization(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard/series/lane-utilization?window=2h", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var body laneUtilizationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Lanes) != 1 || body.Lanes[0].Lane != "claude-code" || len(body.Lanes[0].Hours) != 2 {
		t.Fatalf("lanes: %+v", body.Lanes)
	}
	last := body.Lanes[0].Hours[len(body.Lanes[0].Hours)-1]
	if last.BusyFraction == nil || *last.BusyFraction <= 0 {
		t.Fatalf("the current hour should show the released lease, got %v", last.BusyFraction)
	}
}

func TestDashboardSeries_RejectsBadWindowsAndMethods(t *testing.T) {
	s := &Server{}
	for _, q := range []string{"?window=abc", "?window=30m", "?window=169h"} {
		rec := httptest.NewRecorder()
		s.handleDashboardSeriesLaneUtilization(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard/series/lane-utilization"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("utilization %s: status %d, want 400", q, rec.Code)
		}
	}
	for _, h := range []http.HandlerFunc{s.handleDashboardSeriesLaneUtilization, s.handleDashboardSeriesVerifierHealth, s.handleDashboardSeriesThroughputForecast} {
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(http.MethodPost, "/api/dashboard/series/x", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST: status %d, want 405", rec.Code)
		}
	}
}

func TestDashboardSeries_RoutesAreMounted(t *testing.T) {
	mux := http.NewServeMux()
	(&Server{}).registerDashboardSeriesRoutes(mux)
	for _, p := range []string{"/api/dashboard/series/lane-utilization", "/api/dashboard/series/throughput-forecast", "/api/dashboard/series/verifier-health"} {
		if _, pattern := mux.Handler(httptest.NewRequest(http.MethodGet, p, nil)); pattern != p {
			t.Errorf("%s not mounted (matched %q)", p, pattern)
		}
	}
}

func TestForecast_PointsKeepCompletionRange(t *testing.T) {
	low, high := 2.5, 4.0
	pts := buildThroughputForecast([]autonomous.ProgressTrendPoint{
		{Date: "2026-11-09", Throughput3d: 3, CompletionLowDays: &low, CompletionHighDays: &high, KnownTasks: 12, EstimatedTasks: 9},
		{Date: "2026-11-10", Throughput3d: 0},
	})
	if len(pts) != 2 {
		t.Fatalf("points: %+v", pts)
	}
	if pts[0].CompletionLowDays == nil || *pts[0].CompletionLowDays != 2.5 || pts[0].CompletionHighDays == nil || *pts[0].CompletionHighDays != 4 {
		t.Fatalf("range lost: %+v", pts[0])
	}
	if pts[1].CompletionLowDays != nil || pts[1].CompletionHighDays != nil {
		t.Fatalf("a point without a range must keep null bounds: %+v", pts[1])
	}
	if pts[0].Throughput3d != 3 || pts[0].KnownTasks != 12 || pts[0].EstimatedTasks != 9 || pts[0].Date != "2026-11-09" {
		t.Fatalf("fields: %+v", pts[0])
	}
}

func TestForecast_EmptyWithoutManagerAndRejectsBadDays(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleDashboardSeriesThroughputForecast(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard/series/throughput-forecast", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body struct {
		Points []any `json:"points"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Points == nil || len(body.Points) != 0 {
		t.Fatalf("points must be an empty array, got %s", rec.Body.String())
	}
	for _, q := range []string{"?days=0", "?days=400", "?days=x", "?days=3.5"} {
		rec := httptest.NewRecorder()
		s.handleDashboardSeriesThroughputForecast(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard/series/throughput-forecast"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", q, rec.Code)
		}
	}
}

func TestVerifierHealth_WaitsAndRunsPerNode(t *testing.T) {
	from, now := seriesAt(-60), seriesT0
	verifierPools := func(node string) []string { return []string{capacity.VerifierPool(node)} }
	ivs := []capacity.LaneInterval{
		{Kind: capacity.LaneIntervalBusy, Pools: verifierPools("n1"), Node: "n1", Start: seriesAt(-30), End: seriesAt(-20)},    // 10 min run
		{Kind: capacity.LaneIntervalBusy, Pools: verifierPools("n1"), Node: "n1", Start: seriesAt(-10), End: seriesAt(-5)},     // 5 min run
		{Kind: capacity.LaneIntervalWaiting, Pools: verifierPools("n1"), Node: "n1", Start: seriesAt(-40), End: seriesAt(-30)}, // 10 min wait
		{Kind: capacity.LaneIntervalBusy, Pools: verifierPools("n2"), Node: "n2", Start: seriesAt(-50), End: seriesAt(-45)},    // 5 min run
		// A worker lease on the executor pool is not verifier work.
		{Kind: capacity.LaneIntervalBusy, Pools: []string{"node:n1", "llm:claude-code"}, Node: "n1", Start: seriesAt(-30), End: seriesAt(-20)},
		// A run that began before the window counts only from the window start.
		{Kind: capacity.LaneIntervalBusy, Pools: verifierPools("n2"), Node: "n2", Start: seriesAt(-90), End: seriesAt(-55)}, // 5 min inside
	}
	nodes := buildVerifierNodes(ivs, from, now)
	if len(nodes) != 2 || nodes[0].Node != "n1" || nodes[1].Node != "n2" {
		t.Fatalf("nodes: %+v", nodes)
	}
	n1 := nodes[0]
	if n1.Runs != 2 || n1.Waits != 1 || !approx(n1.RunTotalSeconds, 15*60) || !approx(n1.RunAvgSeconds, 7.5*60) || !approx(n1.WaitAvgSeconds, 10*60) {
		t.Fatalf("n1: %+v", n1)
	}
	n2 := nodes[1]
	if n2.Runs != 2 || n2.Waits != 0 || !approx(n2.RunTotalSeconds, 10*60) {
		t.Fatalf("n2: %+v", n2)
	}
}

func TestVerifierHealth_OutcomesPassRateAcrossNodes(t *testing.T) {
	got := buildVerifierOutcomes(autonomous.VerifierStatsView{
		Runs:       4,
		Categories: map[string]int{autonomous.VerifierCatPass: 3, autonomous.VerifierCatInfra: 1},
	})
	if got.PassRate == nil || !approx(*got.PassRate, 0.75) || got.Runs != 4 {
		t.Fatalf("pass rate: %+v", got)
	}
	none := buildVerifierOutcomes(autonomous.VerifierStatsView{})
	if none.PassRate != nil || none.Categories == nil {
		t.Fatalf("no runs must give a null pass rate and non-nil categories: %+v", none)
	}
}

func TestVerifierHealth_EmptyWithoutLedgerAndRejectsBadWindow(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleDashboardSeriesVerifierHealth(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard/series/verifier-health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body verifierHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Nodes) != 0 || body.WindowSeconds != int64((24*time.Hour)/time.Second) {
		t.Fatalf("empty body: %+v", body)
	}
	rec = httptest.NewRecorder()
	s.handleDashboardSeriesVerifierHealth(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard/series/verifier-health?window=10s", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("10s window: status %d, want 400", rec.Code)
	}
}

// approx reports whether two floats agree to a small tolerance.
func approx(a, b float64) bool {
	d := a - b
	return d < 1e-6 && d > -1e-6
}
