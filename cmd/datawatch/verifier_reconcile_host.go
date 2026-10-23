package main

import (
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/session"
)

// verifierReconcileHost backs the startup verifier reconcile with the real
// session manager, capacity ledger and verifier registry.
type verifierReconcileHost struct {
	mgr *session.Manager
	led *capacity.Ledger
	reg *verifierRegistry
}

// LiveVerifiers lists unfinished "autonomous:verify:<task id>" sessions.
func (h *verifierReconcileHost) LiveVerifiers() []autonomous.VerifierSession {
	var out []autonomous.VerifierSession
	for _, s := range h.mgr.ListSessions() {
		taskID, ok := verifierTaskFromSessionName(s.Name)
		if !ok || !sessionRunning(h.mgr, s.FullID) {
			continue
		}
		out = append(out, autonomous.VerifierSession{ID: s.FullID, TaskID: taskID, Started: s.CreatedAt})
	}
	return out
}

// End ends the session through the manager's normal kill path; the reason is
// recorded by the reconcile's task decision and audit event.
func (h *verifierReconcileHost) End(sessionID, _ string) error {
	return h.mgr.Kill(sessionID)
}

// BindLease points the verifier's lease and registry entry at sessionID.
func (h *verifierReconcileHost) BindLease(holder, sessionID string) {
	h.reg.bindSession(holder, sessionID)
	h.led.Bind(holder, sessionID)
}
