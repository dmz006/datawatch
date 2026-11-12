package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// laneUtilizationMaxWindow is the longest window the route accepts. The read
// model carries at most autonomous.LaneUtilizationDecisionLimit decisions, so
// that is also the largest decisions value.
const laneUtilizationMaxWindow = autonomous.LaneUtilizationMaxWindow

// laneUtilizationNow is the clock the route reads; tests replace it.
var laneUtilizationNow = time.Now

// handleAutonomousLaneUtilization serves GET /api/autonomous/lane-utilization:
// how the lanes were used over a window ending now.
func (s *Server) handleAutonomousLaneUtilization(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	q := r.URL.Query()
	window, err := laneUtilizationWindow(q)
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	buckets, err := laneUtilizationCount(q, "bucket", 1, autonomous.LaneUtilizationMaxBuckets, autonomous.LaneUtilizationDefaultBuckets)
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	decisions, err := laneUtilizationCount(q, "decisions", 1, autonomous.LaneUtilizationDecisionLimit, autonomous.LaneUtilizationDecisionLimit)
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	res := s.autonomousMgr.LaneUtilization(window, buckets, laneUtilizationNow())
	// The read model carries its newest decisions in LaneFill; trim to the ask.
	if len(res.LaneFill.Decisions) > decisions {
		res.LaneFill.Decisions = res.LaneFill.Decisions[:decisions]
	}
	writeJSONOK(w, res)
}

// laneUtilizationWindow reads the window query value: a duration such as 6h,
// 24h or 7d, from the minimum to the maximum window. Absent means 24h.
func laneUtilizationWindow(q url.Values) (time.Duration, error) {
	if !q.Has("window") {
		return autonomous.LaneUtilizationDefaultWindow, nil
	}
	raw := strings.TrimSpace(q.Get("window"))
	msg := fmt.Errorf("window must be a duration such as 6h, 24h or 7d, from %s to %s",
		autonomous.LaneUtilizationMinWindow, laneUtilizationMaxWindow)
	var d time.Duration
	if strings.HasSuffix(raw, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(raw, "d"))
		if err != nil || n < 1 || n > 365 {
			return 0, msg
		}
		d = time.Duration(n) * 24 * time.Hour
	} else {
		var err error
		d, err = time.ParseDuration(raw)
		if err != nil {
			return 0, msg
		}
	}
	if d < autonomous.LaneUtilizationMinWindow || d > laneUtilizationMaxWindow {
		return 0, msg
	}
	return d, nil
}

// laneUtilizationCount reads a whole-number query value from lo to hi; absent
// means def.
func laneUtilizationCount(q url.Values, name string, lo, hi, def int) (int, error) {
	if !q.Has(name) {
		return def, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(q.Get(name)))
	if err != nil || n < lo || n > hi {
		return 0, fmt.Errorf("%s must be a whole number from %d to %d", name, lo, hi)
	}
	return n, nil
}
