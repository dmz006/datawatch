// Housekeeping repairs read route: GET /api/autonomous/housekeeping-repairs.
//
// The body shows, per repair group (task_chain, advance_judge, lane_fill,
// leftovers, planning), the mode in effect (off, report or on), what a report-mode pass
// would do (`would`), what an on-mode pass did (`did`), and the cases that
// failed or were skipped as unsafe. Read-only: the route runs no repair,
// raises no alert and cannot change a mode; a group's mode changes only
// through the existing config set paths. A federated peer needs the
// autonomous read capability (federation.CapAutonomousRead).
package server

import (
	"net/http"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// housekeepingRepairsReader is the narrow read surface this route uses.
type housekeepingRepairsReader interface {
	HousekeepingRepairsView() autonomous.HousekeepingRepairsView
}

// handleHousekeepingRepairs serves GET /api/autonomous/housekeeping-repairs.
func (s *Server) handleHousekeepingRepairs(w http.ResponseWriter, r *http.Request) {
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
	hr, ok := s.autonomousMgr.(housekeepingRepairsReader)
	if !ok {
		http.Error(w, "housekeeping repairs unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSONOK(w, hr.HousekeepingRepairsView())
}
