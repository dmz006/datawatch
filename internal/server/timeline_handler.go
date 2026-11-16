// REST surface for the timeline chart.
//
//   GET /api/autonomous/timeline  the interval chart of the Automata that
//                                 moved in the last N days (query: project,
//                                 automaton, stage, kind, tag)
//
// Read-only; returns the same object the MCP tool, the CLI, the comm
// channels and the PWA read.

package server

import (
	"net/http"
	"net/url"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// TimelineRESTOperation is one timeline REST operation: the name and method.
type TimelineRESTOperation struct {
	Name   string
	Method string
}

// timelineRESTOperations is the single list of timeline REST operations, read
// by the parity test.
var timelineRESTOperations = []TimelineRESTOperation{
	{"timeline", http.MethodGet},
}

// TimelineRESTOperations returns the timeline REST operations.
func TimelineRESTOperations() []TimelineRESTOperation {
	return append([]TimelineRESTOperation(nil), timelineRESTOperations...)
}

// NewTimelineHandler builds the timeline route alone, for the parity test.
// now is the injected clock (nil = time.Now), like NewProgressHandler.
func NewTimelineHandler(mgr AutonomousAPI, now func() time.Time) http.Handler {
	s := &Server{autonomousMgr: mgr}
	if now != nil {
		s.timelineNow = now
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/autonomous/timeline", s.handleAutonomousTimeline)
	return mux
}

// timelineProvider is the optional AutonomousAPI extension that supplies the
// records the timeline is drawn from. Kept off AutonomousAPI so test doubles
// keep compiling; a manager without it draws an empty chart.
type timelineProvider interface {
	TimelineRecords() []*autonomous.PRD
}

// timelineClock is the clock the timeline route reads; nil means time.Now.
func (s *Server) timelineClock() time.Time {
	if s.timelineNow != nil {
		return s.timelineNow()
	}
	return time.Now()
}

// timelineFilterFromQuery builds the chart filter from the query parameters.
func timelineFilterFromQuery(q url.Values) autonomous.TimelineFilter {
	return autonomous.TimelineFilter{
		Project:   q.Get("project"),
		Automaton: q.Get("automaton"),
		Stage:     q.Get("stage"),
		Kind:      q.Get("kind"),
		Tag:       q.Get("tag"),
	}
}

// handleAutonomousTimeline serves GET /api/autonomous/timeline.
func (s *Server) handleAutonomousTimeline(w http.ResponseWriter, r *http.Request) {
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
	var prds []*autonomous.PRD
	if p, ok := s.autonomousMgr.(timelineProvider); ok {
		prds = p.TimelineRecords()
	}
	filter := timelineFilterFromQuery(r.URL.Query())
	if s.cfg != nil && len(s.cfg.Projects) > 0 {
		filter.Projects = rollupProjectDirs(s.cfg.Projects)
		for _, raw := range s.autonomousMgr.ListAutomations() {
			if a, ok := raw.(*autonomous.Automation); ok {
				filter.Automations = append(filter.Automations, a)
			}
		}
	}
	writeJSONOK(w, autonomous.BuildTimeline(s.timelineClock().UTC(), prds, filter))
}
