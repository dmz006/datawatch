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
	writeJSONOK(w, autonomous.BuildTimeline(s.timelineClock().UTC(), prds, timelineFilterFromQuery(r.URL.Query())))
}
