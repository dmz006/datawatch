// Gate repair read route: GET /api/autonomous/gate-repairs.
//
// The body shows the gate repair engine's mode in effect (off, report or on)
// and its bounded records, newest first: for each failed gate the engine saw,
// the failure class it found, what it would do or did, and how to undo it.
// Read-only: the route runs no repair, raises no alert and cannot change the
// mode; the mode changes only through the existing config set paths
// (autonomous.gate_repair.mode). A federated peer needs the autonomous read
// capability (federation.CapAutonomousRead).
package server

import (
	"net/http"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// gateRepairsReader is the narrow read surface this route uses.
type gateRepairsReader interface {
	GateRepairerStatus() autonomous.GateRepairStatus
}

// handleGateRepairs serves GET /api/autonomous/gate-repairs.
func (s *Server) handleGateRepairs(w http.ResponseWriter, r *http.Request) {
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
	gr, ok := s.autonomousMgr.(gateRepairsReader)
	if !ok {
		http.Error(w, "gate repair unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSONOK(w, gr.GateRepairerStatus())
}
