package main

import (
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/session"
)

// judgeReconcileHost backs the startup guardrail-judge reconcile with the
// real session manager, the shared capacity ledger and the judges registry
// (created in wireCapacity, one shared instance). It mirrors
// verifierReconcileHost, which backs the verifier reconcile; the two host
// types only differ in which session names they treat as their own unit's
// session.
type judgeReconcileHost struct {
	mgr *session.Manager
	led *capacity.Ledger
	reg *verifierRegistry
}

// LiveJudges lists unfinished "autonomous:guardrail:<unit id>" sessions with
// the parsed unit id. Completed, failed and killed judges are dropped here,
// exactly as the verifier host drops its terminal sessions.
func (h *judgeReconcileHost) LiveJudges() []autonomous.JudgeSession {
	var out []autonomous.JudgeSession
	for _, s := range h.mgr.ListSessions() {
		unitID, ok := judgeUnitFromSessionName(s.Name)
		if !ok || !sessionRunning(h.mgr, s.FullID) {
			continue
		}
		out = append(out, autonomous.JudgeSession{ID: s.FullID, UnitID: unitID, Started: s.CreatedAt})
	}
	return out
}

// End ends the session through the manager's normal kill path.
func (h *judgeReconcileHost) End(sessionID, _ string) error {
	return h.mgr.Kill(sessionID)
}

// BindLease points the judge's lease and registry entry at sessionID so
// later reconcile turns and the lease reaper see the new session as the
// live one.
func (h *judgeReconcileHost) BindLease(holder, sessionID string) {
	h.reg.bindSession(holder, sessionID)
	h.led.Bind(holder, sessionID)
}
