// api_dashboard_series.go — the series endpoints behind three dashboard cards:
//
//	GET /api/dashboard/series/lane-utilization?window=24h    dashboard:read
//	GET /api/dashboard/series/throughput-forecast?days=30    autonomous:read
//	GET /api/dashboard/series/verifier-health?window=24h     dashboard:read
//
// Every window is bounded: the utilization and verifier windows run from 1 hour
// (utilization) or 1 minute (verifier) to 168 hours, and the forecast runs from
// 1 to 365 days, the same bound the progress trend uses.
//
// Lane utilization is the share of each clock hour in which a lane held at
// least one lease. It reads the busy intervals of the capacity ledger, closed
// history plus the leases still open at the call. Waiting intervals are left
// out. An hour that began before the ledger started keeping history reports a
// null fraction, so it is never drawn as idle.
//
// The throughput forecast reduces the progress trend to one point per saved
// snapshot: the completion range in days, the throughput per day (completions
// per active day over the 24 hours before the snapshot) and the completion
// count behind it.
//
// Verifier health reports, per compute node, the verifier waits and runs seen
// in the window. Verifier leases take the node-verify:<node> pool, so a verifier
// is recognised by that pool name. No pass rate is reported: the ledger records
// no verifier verdict on its intervals, and the process-wide verifier outcome
// aggregate is not tied to a node, so neither can give a per-node pass rate.
package server

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/federation"
)

// Window bounds for the series endpoints.
const (
	seriesUtilDefaultWindow     = 24 * time.Hour
	seriesUtilMinWindow         = time.Hour
	seriesVerifierDefaultWindow = 24 * time.Hour
	seriesVerifierMinWindow     = time.Minute
	seriesMaxWindow             = 7 * 24 * time.Hour
)

// seriesHourPoint is one clock hour of a lane's utilization. BusyFraction is
// null when the hour began before the ledger kept history.
type seriesHourPoint struct {
	Start        time.Time `json:"start"`
	BusyFraction *float64  `json:"busy_fraction"`
}

// seriesLane is one lane's hourly utilization, oldest hour first.
type seriesLane struct {
	Lane  string            `json:"lane"`
	Hours []seriesHourPoint `json:"hours"`
}

// laneUtilizationResponse is the body of GET /api/dashboard/series/lane-utilization.
type laneUtilizationResponse struct {
	GeneratedAt   time.Time    `json:"generated_at"`
	WindowFrom    time.Time    `json:"window_from"`
	WindowTo      time.Time    `json:"window_to"`
	BucketSeconds int64        `json:"bucket_seconds"`
	RecordingFrom *time.Time   `json:"recording_from,omitempty"`
	Lanes         []seriesLane `json:"lanes"`
}

// forecastPoint is one saved progress snapshot reduced to the daily throughput
// and the completion range.
type forecastPoint struct {
	Date               string   `json:"date"`
	ThroughputPerDay   float64  `json:"throughput_per_day"`
	PRDsLast24h        int      `json:"prds_last_24h"`
	CompletionLowDays  *float64 `json:"completion_low_days"`
	CompletionHighDays *float64 `json:"completion_high_days"`
	KnownTasks         int      `json:"known_tasks"`
	EstimatedTasks     float64  `json:"estimated_tasks"`
}

// throughputForecastResponse is the body of GET /api/dashboard/series/throughput-forecast.
type throughputForecastResponse struct {
	GeneratedAt time.Time       `json:"generated_at"`
	Days        int             `json:"days"`
	Direction   string          `json:"direction"`
	Points      []forecastPoint `json:"points"`
}

// verifierNodeHealth is one compute node's verifier waits and runs in the window.
type verifierNodeHealth struct {
	Node             string  `json:"node"`
	Runs             int     `json:"runs"`
	Waits            int     `json:"waits"`
	WaitTotalSeconds float64 `json:"wait_total_seconds"`
	WaitAvgSeconds   float64 `json:"wait_avg_seconds"`
	RunTotalSeconds  float64 `json:"run_total_seconds"`
	RunAvgSeconds    float64 `json:"run_avg_seconds"`
}

// verifierHealthResponse is the body of GET /api/dashboard/series/verifier-health.
type verifierHealthResponse struct {
	GeneratedAt   time.Time            `json:"generated_at"`
	WindowFrom    time.Time            `json:"window_from"`
	WindowTo      time.Time            `json:"window_to"`
	WindowSeconds int64                `json:"window_seconds"`
	RecordingFrom *time.Time           `json:"recording_from,omitempty"`
	Nodes         []verifierNodeHealth `json:"nodes"`
}

// registerDashboardSeriesRoutes mounts the three series routes on the daemon's mux.
func (s *Server) registerDashboardSeriesRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/dashboard/series/lane-utilization", s.handleDashboardSeriesLaneUtilization)
	mux.HandleFunc("/api/dashboard/series/throughput-forecast", s.handleDashboardSeriesThroughputForecast)
	mux.HandleFunc("/api/dashboard/series/verifier-health", s.handleDashboardSeriesVerifierHealth)
}

// parseSeriesWindow reads a duration query value, defaulting when it is empty
// and refusing anything outside [lo, hi]. label names the parameter in the error.
func parseSeriesWindow(raw, label string, def, lo, hi time.Duration) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < lo || d > hi {
		return 0, fmt.Errorf("%s: a duration from %s to %s is needed", label, lo, hi)
	}
	return d, nil
}

// seriesWindowHours is the number of whole clock hours a window spans, rounded up.
func seriesWindowHours(window time.Duration) int {
	return int((window + time.Hour - 1) / time.Hour)
}

// buildLaneUtilization turns the busy intervals into hourly fractions. The
// buckets run from first, one per clock hour, to now; the last one is partial.
// Waiting intervals are ignored. Lanes with no busy interval are left out.
func buildLaneUtilization(ivs []capacity.LaneInterval, recording *time.Time, first time.Time, hours int, now time.Time) []seriesLane {
	busy := map[string][]span{}
	var order []string
	for _, iv := range ivs {
		if iv.Kind == capacity.LaneIntervalWaiting {
			continue
		}
		lane := lanePulseLane(iv.Pools)
		if _, ok := busy[lane]; !ok {
			order = append(order, lane)
		}
		busy[lane] = append(busy[lane], span{iv.Start, iv.End})
	}
	sort.Strings(order)
	out := make([]seriesLane, 0, len(order))
	for _, lane := range order {
		merged := mergeSpans(busy[lane])
		pts := make([]seriesHourPoint, 0, hours)
		for i := 0; i < hours; i++ {
			bs := first.Add(time.Duration(i) * time.Hour)
			be := bs.Add(time.Hour)
			if be.After(now) {
				be = now
			}
			pt := seriesHourPoint{Start: bs}
			if be.After(bs) && (recording == nil || be.After(*recording)) {
				var secs float64
				for _, sp := range merged {
					c := clipSpan(sp, bs, be)
					if c.b.After(c.a) {
						secs += c.b.Sub(c.a).Seconds()
					}
				}
				f := secs / be.Sub(bs).Seconds()
				pt.BusyFraction = &f
			}
			pts = append(pts, pt)
		}
		out = append(out, seriesLane{Lane: lane, Hours: pts})
	}
	return out
}

// handleDashboardSeriesLaneUtilization serves GET /api/dashboard/series/lane-utilization.
func (s *Server) handleDashboardSeriesLaneUtilization(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapDashboardRead) {
		return
	}
	window, err := parseSeriesWindow(r.URL.Query().Get("window"), "window", seriesUtilDefaultWindow, seriesUtilMinWindow, seriesMaxWindow)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	now := time.Now()
	hours := seriesWindowHours(window)
	first := now.Truncate(time.Hour).Add(-time.Duration(hours-1) * time.Hour)
	ivs, recording := s.lanePulseIntervals(first, now)
	writeJSONOK(w, laneUtilizationResponse{
		GeneratedAt: now, WindowFrom: first, WindowTo: now,
		BucketSeconds: int64(time.Hour / time.Second), RecordingFrom: recording,
		Lanes: buildLaneUtilization(ivs, recording, first, hours, now),
	})
}

// buildThroughputForecast reduces the trend points to the series fields.
func buildThroughputForecast(points []autonomous.ProgressTrendPoint) []forecastPoint {
	out := make([]forecastPoint, 0, len(points))
	for _, p := range points {
		out = append(out, forecastPoint{
			Date: p.Date, ThroughputPerDay: p.PRDsPerDay24h, PRDsLast24h: p.PRDsLast24h,
			CompletionLowDays: p.CompletionLowDays, CompletionHighDays: p.CompletionHighDays,
			KnownTasks: p.KnownTasks, EstimatedTasks: p.EstimatedTasks,
		})
	}
	return out
}

// handleDashboardSeriesThroughputForecast serves GET /api/dashboard/series/throughput-forecast.
func (s *Server) handleDashboardSeriesThroughputForecast(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	days := progressTrendDefaultDays
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > progressTrendMaxDays {
			http.Error(w, "bad request: days: must be a whole number from 1 to "+strconv.Itoa(progressTrendMaxDays), http.StatusBadRequest)
			return
		}
		days = n
	}
	now := s.progressClock()
	var snaps []autonomous.ProgressSnapshot
	if p, ok := s.autonomousMgr.(progressProvider); ok {
		if store := p.ProgressSnapshots(); store != nil {
			snaps = store.List(days, now)
		}
	}
	t := autonomous.Trend(snaps, "")
	writeJSONOK(w, throughputForecastResponse{
		GeneratedAt: now, Days: days, Direction: t.Direction,
		Points: buildThroughputForecast(t.Points),
	})
}

// buildVerifierNodes sums the verifier waits and runs per node from the
// intervals, clipped to [from, now]. An interval counts as a verifier when one
// of its pools is node-verify:<node>.
func buildVerifierNodes(ivs []capacity.LaneInterval, from, now time.Time) []verifierNodeHealth {
	prefix := capacity.VerifierPool("")
	byNode := map[string]*verifierNodeHealth{}
	for _, iv := range ivs {
		node := ""
		for _, p := range iv.Pools {
			if strings.HasPrefix(p, prefix) {
				node = strings.TrimPrefix(p, prefix)
				break
			}
		}
		if node == "" {
			continue
		}
		c := clipSpan(span{iv.Start, iv.End}, from, now)
		if !c.b.After(c.a) {
			continue
		}
		secs := c.b.Sub(c.a).Seconds()
		h := byNode[node]
		if h == nil {
			h = &verifierNodeHealth{Node: node}
			byNode[node] = h
		}
		if iv.Kind == capacity.LaneIntervalWaiting {
			h.Waits++
			h.WaitTotalSeconds += secs
		} else {
			h.Runs++
			h.RunTotalSeconds += secs
		}
	}
	out := make([]verifierNodeHealth, 0, len(byNode))
	for _, h := range byNode {
		if h.Waits > 0 {
			h.WaitAvgSeconds = h.WaitTotalSeconds / float64(h.Waits)
		}
		if h.Runs > 0 {
			h.RunAvgSeconds = h.RunTotalSeconds / float64(h.Runs)
		}
		out = append(out, *h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Node < out[j].Node })
	return out
}

// handleDashboardSeriesVerifierHealth serves GET /api/dashboard/series/verifier-health.
func (s *Server) handleDashboardSeriesVerifierHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapDashboardRead) {
		return
	}
	window, err := parseSeriesWindow(r.URL.Query().Get("window"), "window", seriesVerifierDefaultWindow, seriesVerifierMinWindow, seriesMaxWindow)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	now := time.Now()
	from := now.Add(-window)
	ivs, recording := s.lanePulseIntervals(from, now)
	writeJSONOK(w, verifierHealthResponse{
		GeneratedAt: now, WindowFrom: from, WindowTo: now,
		WindowSeconds: int64(window / time.Second), RecordingFrom: recording,
		Nodes: buildVerifierNodes(ivs, from, now),
	})
}
