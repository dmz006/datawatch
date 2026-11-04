package server

import (
	"github.com/dmz006/datawatch/internal/autonomous"
)

// planningRequeueOutcome reports whether prdID is already planning and,
// when it is, runs the shared planning-requeue helper (autonomous.RestartPlanning)
// so the re-requested decompose is answered with the structured outcome
// — requeued / deferred_to_next_run / unchanged — instead of a second
// decompose being spawned. The manager interface returns any, so the
// concrete *autonomous.PlanningRequeueResult is recovered from the
// any-typed result before it is embedded into the HTTP response.
func (s *Server) planningRequeueOutcome(prdID string) (any, bool) {
	if s.autonomousMgr == nil {
		return nil, false
	}
	out, found := s.autonomousMgr.GetPRD(prdID)
	if !found {
		return nil, false
	}
	p, ok := out.(*autonomous.PRD)
	if !ok || p.Status != autonomous.PRDPlanning {
		return nil, false
	}
	res, err := s.autonomousMgr.RestartPlanning(prdID)
	if err != nil {
		return nil, false
	}
	return res, true
}
