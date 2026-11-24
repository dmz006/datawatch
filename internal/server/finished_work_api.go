// Finished-work routes: GET /api/autonomous/finished-work (the digest of
// finished work waiting to land, each row with its owner and open question)
// and POST /api/autonomous/finished-work/run (run one bounded sweep pass or
// the audit now, which is also what a datawatch schedule calls). The archive
// and cancel routes take an optional keep or drop decision and reason; see
// archiveAutomaton and cancelAutomaton.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

type finishedWorkReader interface {
	FinishedWorkDigest() autonomous.FinishedWorkDigest
}

type finishedWorkRunner interface {
	RunFinishedWorkSweep(ctx context.Context) autonomous.FinishedWorkPass
	RunFinishedWorkAudit(ctx context.Context) (autonomous.FinishedWorkAuditResult, error)
}

type cancelDecider interface {
	CancelDecided(id, decision, reason string) error
}

type archiveDecider interface {
	ArchiveDecided(id, decision, reason string) (any, error)
}

// cancelAutomaton cancels with an optional landing decision.
func (s *Server) cancelAutomaton(id, decision, reason string) error {
	if strings.TrimSpace(decision) == "" {
		return s.autonomousMgr.Cancel(id)
	}
	d, ok := s.autonomousMgr.(cancelDecider)
	if !ok {
		return errors.New("cancel with a decision is not supported by this daemon")
	}
	return d.CancelDecided(id, decision, reason)
}

// archiveAutomaton archives with an optional landing decision.
func (s *Server) archiveAutomaton(id, decision, reason string) (any, error) {
	if strings.TrimSpace(decision) == "" {
		return s.autonomousMgr.Archive(id)
	}
	d, ok := s.autonomousMgr.(archiveDecider)
	if !ok {
		return nil, errors.New("archive with a decision is not supported by this daemon")
	}
	return d.ArchiveDecided(id, decision, reason)
}

// handleFinishedWork serves GET /api/autonomous/finished-work. Read-only: no
// git, no question, no change.
func (s *Server) handleFinishedWork(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	fr, ok := s.autonomousMgr.(finishedWorkReader)
	if !ok {
		http.Error(w, "finished work unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSONOK(w, fr.FinishedWorkDigest())
}

// handleFinishedWorkRun serves POST /api/autonomous/finished-work/run with
// {"run": "sweep"|"audit"}. It reads git read-only; in mode on a sweep asks its
// questions and the audit writes its decision record, in shadow it records
// findings only.
func (s *Server) handleFinishedWorkRun(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousWrite) {
		return
	}
	fr, ok := s.autonomousMgr.(finishedWorkRunner)
	if !ok {
		http.Error(w, "finished work unavailable", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Run string `json:"run"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	switch strings.ToLower(strings.TrimSpace(req.Run)) {
	case "sweep":
		pass := fr.RunFinishedWorkSweep(r.Context())
		s.audit(r.Context(), "finished_work_sweep", "automaton", "", map[string]any{"found": pass.Found, "asked": pass.Asked, "mode": pass.Mode})
		writeJSONOK(w, pass)
	case "audit":
		res, err := fr.RunFinishedWorkAudit(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.audit(r.Context(), "finished_work_audit", "automaton", "", map[string]any{"scanned": res.Scanned, "findings": len(res.Findings), "mode": res.Mode})
		writeJSONOK(w, res)
	default:
		http.Error(w, `run must be "sweep" or "audit"`, http.StatusBadRequest)
	}
}
