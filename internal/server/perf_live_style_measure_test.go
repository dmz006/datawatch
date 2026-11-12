package server

import (
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"testing"
	"time"
)

// TestEndpointMeasureLiveStyleBeforeAfter measures, against the live-style
// seeded fixture (SeedPerfServer: 3000 sessions incl. 150 with 128 KB tasks,
// 300 Automata incl. six 7-13 MB), both payload forms the PWA can take for
// its three load-time lists — the old full forms the 2026-10-23 live
// baseline recorded (339 MB PRD list, 32 MB session list) and the new
// first-page forms — for the
// "## Result against the live-style data set" section of
// docs/plans/2026-10-23-web-ui-load-performance.md.
//
// PERF_LIVE_REPORT_OUT=<path> writes the rows as JSON.
func TestEndpointMeasureLiveStyleBeforeAfter(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	runtime.GC()
	seed := SeedPerfServer(t, t.TempDir())
	s := seed.Server

	eps := []perfEndpoint{
		{"old_sessions_full", "/api/sessions", s.handleSessions},
		{"old_prds_full", "/api/autonomous/prds?full=1", s.handleAutonomousPRDs},
		{"old_automations", "/api/automations", s.handleAutomations},
		{"new_sessions_summary50", "/api/sessions?view=summary&limit=50", s.handleSessions},
		{"new_prds_firstpage", "/api/autonomous/prds", s.handleAutonomousPRDs},
		{"new_automations", "/api/automations", s.handleAutomations},
		{"alerts_50", "/api/alerts?limit=50", s.handleAlerts},
		{"observer_stats", "/api/observer/stats", s.handleObserverStats},
	}

	var rows []PerfRow
	for _, ep := range eps {
		start := time.Now()
		rr := perfGet(ep.Handler, ep.Path)
		elapsed := time.Since(start)
		body := rr.Body.Bytes()
		gzLen := perfGzipLen(body)
		row := PerfRow{Name: ep.Name, Path: ep.Path, Status: rr.Code, RawBytes: len(body), GzipBytes: gzLen, ServerMs: float64(elapsed.Microseconds()) / 1000}
		if len(body) > 0 {
			row.GzipRatioPc = 100 * float64(gzLen) / float64(len(body))
		}
		rows = append(rows, row)
	}

	perfLogTable(t, rows)
	for _, r := range rows {
		if r.Status != http.StatusOK {
			t.Errorf("%s: status %d, want 200", r.Name, r.Status)
		}
		if r.RawBytes == 0 {
			t.Errorf("%s: empty body", r.Name)
		}
	}

	out := os.Getenv("PERF_LIVE_REPORT_OUT")
	if out == "" {
		return
	}
	report := map[string]any{
		"seed": map[string]any{
			"sessions": seed.Sessions, "big_tasks": seed.BigTasks, "with_logs": seed.WithLogs,
			"automata": seed.Automata, "heavy_bytes": seed.HeavyBytes, "automations": seed.Automations, "alerts": seed.Alerts,
		},
		"endpoints": rows,
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		t.Fatalf("write PERF_LIVE_REPORT_OUT %s: %v", out, err)
	}
}
