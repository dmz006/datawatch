// Housekeeping repairs read route: GET /api/autonomous/housekeeping-repairs.
//
// The body shows, per repair group (task_chain, advance_judge, lane_fill,
// leftovers, planning), the mode in effect (off, report or on), what a report-mode pass
// would do (`would`), what an on-mode pass did (`did`), and the cases that
// failed or were skipped as unsafe. It also carries the refusal ledger
// (`refusals`: refused Automaton steps, newest first) and the finished-work
// sweep findings (`finished_work`, newest first); both are empty lists, never
// null. Read-only: the route runs no repair,
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

// housekeepingLedgerReader is the narrow read surface for the refusals and
// finished-work rows of the body.
type housekeepingLedgerReader interface {
	RefusalLedger() *autonomous.RefusalLedger
	FinishedWorkSweep() *autonomous.FinishedWorkSweep
}

// housekeepingRepairsBody is the body of the route: the repair groups and
// intermittent defects, plus the refusals and finished-work rows.
type housekeepingRepairsBody struct {
	autonomous.HousekeepingRepairsView
	Refusals     []autonomous.RefusalEntry        `json:"refusals"`
	FinishedWork []autonomous.FinishedWorkFinding `json:"finished_work"`
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
	body := housekeepingRepairsBody{
		HousekeepingRepairsView: hr.HousekeepingRepairsView(),
		Refusals:                []autonomous.RefusalEntry{},
		FinishedWork:            []autonomous.FinishedWorkFinding{},
	}
	if lr, ok := s.autonomousMgr.(housekeepingLedgerReader); ok {
		if l := lr.RefusalLedger(); l != nil {
			body.Refusals = l.List()
		}
		if sw := lr.FinishedWorkSweep(); sw != nil {
			body.FinishedWork = sw.SweepFindings()
		}
	}
	writeJSONOK(w, body)
}
