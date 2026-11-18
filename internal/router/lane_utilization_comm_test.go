package router

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// laneUtilizationFixedNow is the fixed clock every fixture is built from.
var laneUtilizationFixedNow = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

// laneUtilizationCommBody builds a lane utilization body ending at the fixed
// clock. idle sets the claude lane idle with eligible work.
func laneUtilizationCommBody(windowSeconds int, idle bool) string {
	flag := `"idle_with_eligible_work":false,"idle_with_eligible_seconds":0`
	if idle {
		flag = `"idle_with_eligible_work":true,"idle_with_eligible_seconds":725,"first_eligible_item":"Add paging"`
	}
	end := laneUtilizationFixedNow.Format(time.RFC3339)
	return `{"window_end":"` + end + `","window_seconds":` + itoa(windowSeconds) + `,"bucket_count":24,` +
		`"lanes":[{"id":"gpu-1","type":"gpu","busy_share":0.42,"runs":[],"queue_wait":{},"idle_with_eligible_work":false},` +
		`{"id":"claude-1","type":"claude","busy_share":0.1,"runs":[],"queue_wait":{},` + flag + `}],` +
		`"backend_mix":[],"lane_fill":{"mode":"shadow","decisions":[` +
		`{"at":"` + end + `","lane_id":"claude-1","action":"start_prd","prd_id":"p9","reason":"fill"},` +
		`{"at":"2026-11-12T11:00:00Z","lane_id":"gpu-1","action":"older","prd_id":"p1"}]},` +
		`"plan_usage":{"plan_points":100,"points":37.5,"tokens":123456,"percent_of_plan":37.5}}`
}

func itoa(n int) string { return fmtInt(n) }

func fmtInt(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func TestLaneUtilization_DefaultWindow(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, laneUtilizationCommBody(86400, false))
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous lane-utilization"), " ")
	if log.path != "/api/autonomous/lane-utilization" || strings.Contains(log.query, "window=") {
		t.Errorf("path=%s query=%q, want the route with no window", log.path, log.query)
	}
	for _, want := range []string{
		"window 24h0m0s", "gpu-1: 42% busy", "claude-1: 10% busy",
		"Lane fill mode: shadow", "Newest decision: 12:00:00 claude-1 start_prd p9: fill",
		"Plan usage: 37.5 of 100.0 points (37.5%), 123456 tokens",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
	if strings.Contains(got, "older") || strings.Contains(got, "$") || strings.Contains(got, "IDLE") {
		t.Errorf("reply must show one decision, no dollars, no idle flag: %q", got)
	}
}

func TestLaneUtilization_CustomWindow(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, laneUtilizationCommBody(21600, false))
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous lane-utilization 6h"), " ")
	if !strings.Contains(log.query, "window=6h") {
		t.Errorf("query=%q, want window=6h", log.query)
	}
	if !strings.Contains(got, "window 6h0m0s") {
		t.Errorf("reply lacks the 6h window: %q", got)
	}
}

func TestLaneUtilization_BadWindow(t *testing.T) {
	for _, w := range []string{"banana", "30m", "99d", "0d"} {
		log, port := laneFillCommDaemon(t, http.StatusOK, laneUtilizationCommBody(86400, false))
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage("autonomous lane-utilization "+w), " ")
		if log.calls != 0 {
			t.Errorf("window %q: bad window must be refused before anything is sent", w)
		}
		if !strings.Contains(got, "failed") || !strings.Contains(got, "usage: autonomous lane-utilization [window]") {
			t.Errorf("window %q: reply lacks refusal and usage: %q", w, got)
		}
	}
}

func TestLaneUtilization_FlaggedLane(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK, laneUtilizationCommBody(86400, true))
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous lane-utilization"), " ")
	if !strings.Contains(got, "claude-1: 10% busy [IDLE with eligible work 12m5s, first Add paging]") {
		t.Errorf("reply lacks the flagged lane: %q", got)
	}
	if strings.Contains(got, "gpu-1: 42% busy [IDLE") {
		t.Errorf("gpu lane must not be flagged: %q", got)
	}
}

func TestLaneUtilization_AutonomousDisabled(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusServiceUnavailable, "autonomous disabled")
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous lane-utilization"), " ")
	if !strings.Contains(got, "failed") || !strings.Contains(got, "autonomous disabled") {
		t.Errorf("reply must report autonomous disabled: %q", got)
	}
}

func TestLaneUtilization_ReplyUnderFifteenLines(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK, laneUtilizationCommBody(86400, true))
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous lane-utilization"), "\n")
	if n := strings.Count(got, "\n") + 1; n > 15 {
		t.Errorf("reply is %d lines, want at most 15", n)
	}
}
