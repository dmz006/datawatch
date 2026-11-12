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
	// ender ends sessions in place of mgr when set. Tests set it to record calls.
	ender sessionEnder
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

// End ends the verifier session through the manager and records the reason the
// reconcile gives on the session.
func (h *verifierReconcileHost) End(sessionID, reason string) error {
	var ender sessionEnder = h.mgr
	if h.ender != nil {
		ender = h.ender
	}
	return endVerifierSession(ender, sessionID, reason)
}

// BindLease points the verifier's lease and registry entry at sessionID.
func (h *verifierReconcileHost) BindLease(holder, sessionID string) {
	h.reg.bindSession(holder, sessionID)
	h.led.Bind(holder, sessionID)
}
