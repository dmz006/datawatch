// /api/federation/fingerprint-matches — the match records surfaced
// during fingerprint reconciliation, exposed for operator decision
// (docs/plans/2026-10-14-concurrency-and-budget-design.md §3c). A match
// is a *suggestion* that two daemons hold the same resource; only an
// operator confirmation or rejection ever moves the state, and nothing
// acts on a match here besides answering the operator.
//
// Route map:
//   GET  /api/federation/fingerprint-matches           — list (optional ?state=pending|confirmed|rejected)
//   POST /api/federation/fingerprint-matches/{id}/confirm — confirm one record
//   POST /api/federation/fingerprint-matches/{id}/reject  — reject one record
//
// Order of checks follows the Federation-Parity Rule (AGENT.md): the
// method guard first, then the fedCap guard immediately after, then the
// store nil-check.

package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/fingerprint"
)

// SetFingerprintMatchStore wires the match store behind
// /api/federation/fingerprint-matches. Nil (unwired) answers 503 — same
// convention as the other optional collaborators (e.g. SetFedGroupStore).
func (s *Server) SetFingerprintMatchStore(ms *fingerprint.MatchStore) { s.fingerprintMatchStore = ms }

func (s *Server) handleFingerprintMatches(w http.ResponseWriter, r *http.Request) {
	// Method guard.
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Federation-Parity Rule — capability guard immediately after the
	// method guard, before any other business logic.
	if !s.fedCap(w, r, federation.CapFederationList) {
		return
	}
	if s.fingerprintMatchStore == nil {
		http.Error(w, "fingerprint match store not configured", http.StatusServiceUnavailable)
		return
	}
	state := r.URL.Query().Get("state")
	if state != "" &&
		state != fingerprint.StatePending &&
		state != fingerprint.StateConfirmed &&
		state != fingerprint.StateRejected {
		http.Error(w, "unknown state: expected pending, confirmed or rejected", http.StatusBadRequest)
		return
	}
	matches := s.fingerprintMatchStore.List(state) // non-nil, so JSON says [] not null
	writeJSONOK(w, map[string]any{"matches": matches})
}

func (s *Server) handleFingerprintMatchDecision(w http.ResponseWriter, r *http.Request) {
	// Method guard.
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Federation-Parity Rule — capability guard immediately after the
	// method guard, before any other business logic.
	if !s.fedCap(w, r, federation.CapFederationWrite) {
		return
	}
	if s.fingerprintMatchStore == nil {
		http.Error(w, "fingerprint match store not configured", http.StatusServiceUnavailable)
		return
	}
	// {id}/confirm or {id}/reject.
	tail := strings.TrimPrefix(r.URL.Path, "/api/federation/fingerprint-matches/")
	parts := strings.SplitN(tail, "/", 2)
	if len(parts) != 2 || parts[0] == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	id, action := parts[0], parts[1]
	var m fingerprint.Match
	var err error
	switch action {
	case "confirm":
		m, err = s.fingerprintMatchStore.Confirm(id)
	case "reject":
		m, err = s.fingerprintMatchStore.Reject(id)
	default:
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		if errors.Is(err, fingerprint.ErrUnknownMatch) {
			http.Error(w, "no such match", http.StatusNotFound)
			return
		}
		http.Error(w, "cannot decide this match", http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, m)
}
