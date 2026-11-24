// Closed work outcome routes. The outcome class of a finished Automaton is
// recorded with POST /api/autonomous/prds/{id}/outcome, or in the same step as
// the archive and the cancel (an optional body {outcome, delivered_via, note}
// on POST .../archive and POST .../cancel). GET /api/autonomous/outcome-check
// reads the standing check of closed work and POST runs it now.
// POST /api/autonomous/deploy/record writes a deploy record onto every
// Automaton shipped in a release (the operator's own deploy script calls it).
// A federated peer needs the autonomous read capability for the check read and
// the write capability for the rest.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// closedOutcomeBody is the outcome part of a request body.
type closedOutcomeBody struct {
	Outcome      string `json:"outcome"`
	DeliveredVia string `json:"delivered_via"`
	Note         string `json:"note"`
	Actor        string `json:"actor"`
}

func (b closedOutcomeBody) input() autonomous.OutcomeInput {
	return autonomous.OutcomeInput{Outcome: b.Outcome, DeliveredVia: b.DeliveredVia, Note: b.Note}
}

// decodeOptionalOutcome reads an optional JSON body; an empty body is fine.
func decodeOptionalOutcome(r *http.Request) (closedOutcomeBody, error) {
	var b closedOutcomeBody
	if r.Body == nil {
		return b, nil
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil && !errors.Is(err, io.EOF) {
		return b, err
	}
	return b, nil
}

type outcomeSetter interface {
	SetClosedOutcome(id, actor string, in autonomous.OutcomeInput) (any, error)
}

type outcomeArchiver interface {
	ArchiveWithOutcome(id string, in autonomous.OutcomeInput) (any, error)
}

type outcomeCanceller interface {
	CancelWithOutcome(id string, in autonomous.OutcomeInput) error
}

type outcomeChecker interface {
	OutcomeCheck() autonomous.OutcomeCheckReport
	RunOutcomeCheck(ctx context.Context) autonomous.OutcomeCheckReport
}

type deployRecorder interface {
	RecordReleaseDeployed(rec autonomous.DeployRecord) ([]string, error)
}

// handleClosedOutcomeSet serves POST /api/autonomous/prds/{id}/outcome.
func (s *Server) handleClosedOutcomeSet(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousWrite) {
		return
	}
	setter, ok := s.autonomousMgr.(outcomeSetter)
	if !ok {
		http.Error(w, "closed outcome unavailable", http.StatusServiceUnavailable)
		return
	}
	b, err := decodeOptionalOutcome(r)
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if b.Actor == "" {
		b.Actor = "operator"
	}
	updated, err := setter.SetClosedOutcome(id, b.Actor, b.input())
	if err != nil {
		if _, exists := s.autonomousMgr.GetPRD(id); !exists {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.audit(r.Context(), "set_closed_outcome", "automaton", id, map[string]any{
		"outcome": b.Outcome, "delivered_via": b.DeliveredVia,
	})
	s.writeAutonomousResult(w, r, updated, "", "", "")
}

// handleOutcomeCheck serves GET (read) and POST (run now)
// /api/autonomous/outcome-check.
func (s *Server) handleOutcomeCheck(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	oc, ok := s.autonomousMgr.(outcomeChecker)
	if !ok {
		http.Error(w, "outcome check unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !s.fedCap(w, r, federation.CapAutonomousRead) {
			return
		}
		writeJSONOK(w, oc.OutcomeCheck())
	case http.MethodPost:
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		rep := oc.RunOutcomeCheck(r.Context())
		s.audit(r.Context(), "outcome_check_run", "automaton", "", map[string]any{"findings": len(rep.Findings)})
		writeJSONOK(w, rep)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleDeployRecord serves POST /api/autonomous/deploy/record.
func (s *Server) handleDeployRecord(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousWrite) {
		return
	}
	dr, ok := s.autonomousMgr.(deployRecorder)
	if !ok {
		http.Error(w, "deploy record unavailable", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Release string    `json:"release"`
		Version string    `json:"version"`
		At      time.Time `json:"at"`
		Host    string    `json:"host"`
		Health  string    `json:"health"`
		Detail  string    `json:"detail"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Version == "" {
		req.Version = req.Release
	}
	ids, err := dr.RecordReleaseDeployed(autonomous.DeployRecord{
		Release: req.Release, Version: req.Version, At: req.At, Host: req.Host, Health: req.Health, Detail: req.Detail,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if ids == nil {
		ids = []string{}
	}
	s.audit(r.Context(), "record_deploy", "release", req.Release, map[string]any{"version": req.Version, "health": req.Health, "automata": len(ids)})
	writeJSONOK(w, map[string]any{"release": req.Release, "version": req.Version, "automata": ids})
}
