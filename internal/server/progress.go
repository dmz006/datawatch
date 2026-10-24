// REST surface for the progress metrics.
//
//   GET /api/autonomous/progress        the metrics object
//                                       (query: automation_id, tag_id, since, until)
//   GET /api/autonomous/progress/trend  the saved daily snapshots, reduced to a trend
//                                       (query: days, default 30, 1..365)
//
// Both are read-only and return the same object the MCP tools, the CLI, the
// comm channels and the PWA read.

package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

const (
	progressDateLayout       = "2006-01-02"
	progressTrendDefaultDays = 30
	progressTrendMaxDays     = 365
	// progressWindowsFile is the explicit operator-wait windows file, kept next
	// to the backlog file.
	progressWindowsFile = "operator-wait-windows.tsv"
)

// ProgressRESTOperation is one progress REST operation: its name and method.
type ProgressRESTOperation struct {
	Name   string
	Method string
}

// progressRESTOperations is the single list of progress REST operations, read
// by the parity test.
var progressRESTOperations = []ProgressRESTOperation{
	{"progress", http.MethodGet},
	{"progress_trend", http.MethodGet},
}

// ProgressRESTOperations returns the progress REST operations.
func ProgressRESTOperations() []ProgressRESTOperation {
	return append([]ProgressRESTOperation(nil), progressRESTOperations...)
}

// progressProvider is the optional AutonomousAPI extension that supplies the
// record source and the snapshot store. Kept off AutonomousAPI so test doubles
// keep compiling; a manager without it counts no records.
type progressProvider interface {
	ProgressSource() autonomous.ProgressSource
	ProgressSnapshots() *autonomous.ProgressSnapshotStore
}

func (s *Server) progressClock() time.Time {
	if s.progressNow != nil {
		return s.progressNow()
	}
	return time.Now()
}

// progressDateParam reads an optional YYYY-MM-DD query value.
func progressDateParam(q map[string][]string, name string) (time.Time, bool, error) {
	vals := q[name]
	if len(vals) == 0 || strings.TrimSpace(vals[0]) == "" {
		return time.Time{}, false, nil
	}
	t, err := time.Parse(progressDateLayout, strings.TrimSpace(vals[0]))
	if err != nil {
		return time.Time{}, false, &progressParamError{name + ": " + strconv.Quote(vals[0]) + " is not a valid date (use YYYY-MM-DD)"}
	}
	return t, true, nil
}

type progressParamError struct{ msg string }

func (e *progressParamError) Error() string { return e.msg }

// progressOptions builds the engine options from the query and the progress
// config. since falls back to the configured since date.
func (s *Server) progressOptions(q map[string][]string, now time.Time) (autonomous.ProgressOptions, error) {
	opts := autonomous.ProgressOptions{Now: now.UTC()}
	if s.cfg != nil {
		p := s.cfg.Autonomous.Progress
		opts.Lanes = p.EffectiveLanes()
		opts.GapHours = p.EffectiveInactiveGapHours()
		if t, ok := p.SinceTime(); ok {
			opts.Since = t
		}
	}
	since, haveSince, err := progressDateParam(q, "since")
	if err != nil {
		return opts, err
	}
	if haveSince {
		opts.Since = since
	}
	until, haveUntil, err := progressDateParam(q, "until")
	if err != nil {
		return opts, err
	}
	if haveUntil {
		// until names a calendar day and includes all of it.
		opts.Until = until.Add(24*time.Hour - time.Nanosecond)
	}
	if haveSince && haveUntil && until.Before(since) {
		return opts, &progressParamError{"until: must not be before since"}
	}
	if v := q["automation_id"]; len(v) > 0 {
		opts.AutomationID = strings.TrimSpace(v[0])
	}
	if v := q["tag_id"]; len(v) > 0 {
		opts.TagID = strings.TrimSpace(v[0])
	}
	return opts, nil
}

// progressBacklogPath is the resolved backlog file location, "" when no config
// is wired.
func (s *Server) progressBacklogPath() string {
	if s.cfg == nil {
		return ""
	}
	return s.cfg.Autonomous.Progress.ResolveBacklogPath(s.cfg.Autonomous.WorktreeBaseRepo)
}

// loadProgressBacklog reads the backlog rows. A missing or unreadable file gives
// no rows, and the engine then reports backlog_available=false.
func loadProgressBacklog(path string) []autonomous.BacklogRow {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	rows, err := autonomous.LoadBacklog(path)
	if err != nil {
		return nil
	}
	return rows
}

// loadProgressWindows reads the explicit windows file that sits next to the
// backlog file. A missing or unreadable file gives no windows.
func loadProgressWindows(backlogPath string) []autonomous.InactiveWindow {
	if strings.TrimSpace(backlogPath) == "" {
		return nil
	}
	f, err := os.Open(filepath.Join(filepath.Dir(backlogPath), progressWindowsFile)) // #nosec G304 -- next to the operator's backlog file
	if err != nil {
		return nil
	}
	defer f.Close() //nolint:errcheck
	ws, err := autonomous.ParseWindowsTSV(f)
	if err != nil {
		return nil
	}
	return ws
}

// computeProgress is the one place the metrics object is built for the routes.
func (s *Server) computeProgress(opts autonomous.ProgressOptions) autonomous.ProgressMetrics {
	var src autonomous.ProgressSource
	if p, ok := s.autonomousMgr.(progressProvider); ok {
		src = p.ProgressSource()
	}
	backlogPath := s.progressBacklogPath()
	return autonomous.CollectProgress(src, loadProgressBacklog(backlogPath), loadProgressWindows(backlogPath), opts)
}

// handleAutonomousProgress serves GET /api/autonomous/progress.
func (s *Server) handleAutonomousProgress(w http.ResponseWriter, r *http.Request) {
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
	opts, err := s.progressOptions(r.URL.Query(), s.progressClock())
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	writeJSONOK(w, s.computeProgress(opts))
}

// progressTrendResponse is the body of GET /api/autonomous/progress/trend.
type progressTrendResponse struct {
	Days      int                             `json:"days"`
	Points    []autonomous.ProgressTrendPoint `json:"points"`
	Direction string                          `json:"direction"`
}

// handleAutonomousProgressTrend serves GET /api/autonomous/progress/trend.
func (s *Server) handleAutonomousProgressTrend(w http.ResponseWriter, r *http.Request) {
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
	days := progressTrendDefaultDays
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > progressTrendMaxDays {
			http.Error(w, "bad request: days: must be a whole number from 1 to "+strconv.Itoa(progressTrendMaxDays), http.StatusBadRequest)
			return
		}
		days = n
	}
	var snaps []autonomous.ProgressSnapshot
	if p, ok := s.autonomousMgr.(progressProvider); ok {
		if store := p.ProgressSnapshots(); store != nil {
			snaps = store.List(days, s.progressClock())
		}
	}
	t := autonomous.Trend(snaps)
	writeJSONOK(w, progressTrendResponse{Days: days, Points: t.Points, Direction: t.Direction})
}
