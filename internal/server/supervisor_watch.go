// Supervisor watch read route: GET /api/autonomous/supervisor-watch.
//
// The body shows the whole supervisor watch state at once: the mode in
// effect (`mode`: off, shadow or on — an unset mode reads shadow, the
// release default: reports only, never acts), the instant the
// coordinator last polled (`last_poll_at`, the zero time when it never
// has), the stored findings (`findings`, open first, most recently
// seen first: kind, subject, the operator line, first and last seen,
// the shadow would-alert mark and the raised-alert instant), and the
// comparison view (`comparison`: the last recorded comparison against
// the script's record file, the days of full agreement, and the last
// comparison's one-sided differences).
//
// The route is read-only: the findings are written by the
// coordinator's pass alone, which is why the narrow reader interface
// here exposes a single getter — the read view — and nothing else, and
// why no request method other than GET is accepted. A federated peer
// needs the autonomous read capability (federation.CapAutonomousRead)
// to call it.
package server

import (
	"net/http"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// supervisorWatchReader is the narrow read surface of the
// orchestrator's autonomous manager this route uses: the supervisor
// watch's current read view (mode, last poll instant, findings,
// comparison). The full *autonomous.API satisfies it by its
// SupervisorWatchStatus accessor, so the fake in the tests can stand in
// without the manager.
type supervisorWatchReader interface {
	SupervisorWatchStatus() autonomous.SupervisorWatchStatus
}

// supervisorWatchStatsReader is the narrow read surface the stats route
// uses: the watch's numeric monitoring view.
type supervisorWatchStatsReader interface {
	SupervisorWatchStats() autonomous.SupervisorWatchStats
}

// handleSupervisorWatch serves GET /api/autonomous/supervisor-watch.
// The body is the read view, unfiltered: the store already orders the
// findings (open first, most recently seen first, then the resolved
// ones) and the comparison view already holds just the last comparison
// with the agreement count.
func (s *Server) handleSupervisorWatch(w http.ResponseWriter, r *http.Request) {
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
	tr, ok := s.autonomousMgr.(supervisorWatchReader)
	if !ok {
		http.Error(w, "supervisor watch unavailable", http.StatusServiceUnavailable)
		return
	}

	snap := tr.SupervisorWatchStatus()
	if snap.Findings == nil {
		snap.Findings = []*autonomous.SupervisorWatchFinding{}
	}
	if snap.Comparison.OnlyWatch == nil {
		snap.Comparison.OnlyWatch = []string{}
	}
	if snap.Comparison.OnlyScript == nil {
		snap.Comparison.OnlyScript = []string{}
	}
	writeJSONOK(w, snap)
}

// handleSupervisorWatchStats serves GET /api/autonomous/supervisor-watch/stats:
// the numeric monitoring view (mode, seconds since the last poll, the
// unresolved finding counts and the days of full agreement). Read-only.
func (s *Server) handleSupervisorWatchStats(w http.ResponseWriter, r *http.Request) {
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
	sr, ok := s.autonomousMgr.(supervisorWatchStatsReader)
	if !ok {
		http.Error(w, "supervisor watch unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSONOK(w, sr.SupervisorWatchStats())
}
