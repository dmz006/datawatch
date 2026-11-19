// Archived-unmerged REST surface.
//
//	GET  /api/autonomous/archived-unmerged                    list archived Automata whose branch still holds unmerged commits
//	POST /api/autonomous/archived-unmerged/{id}/carry-forward  start a new draft that reuses the branch; returns the draft
//
// Both are read-only toward git: nothing here deletes or merges a branch, and
// the archived record is never changed. A carry-forward creates a new,
// unapproved draft only.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

const (
	archivedUnmergedPath         = "/api/autonomous/archived-unmerged"
	archivedUnmergedCarrySuffix  = "/carry-forward"
	archivedUnmergedDefaultActor = "operator"
)

// archivedUnmergedFinder is the optional manager surface for the finder; a
// manager without it answers 501.
type archivedUnmergedFinder interface {
	ArchivedUnmerged(ctx context.Context) []autonomous.ArchivedUnmergedEntry
}

// archivedCarrier is the optional manager surface for carry-forward.
type archivedCarrier interface {
	CarryForwardArchivedForced(id, actor string, force bool) (*autonomous.PRD, error)
}

// handleArchivedUnmerged serves the list route and the carry-forward route.
func (s *Server) handleArchivedUnmerged(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, archivedUnmergedPath), "/")
	if rest == "" {
		s.archivedUnmergedList(w, r)
		return
	}
	id, ok := strings.CutSuffix(rest, archivedUnmergedCarrySuffix)
	if !ok || id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	s.archivedUnmergedCarry(w, r, id)
}

func (s *Server) archivedUnmergedList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	f, ok := s.autonomousMgr.(archivedUnmergedFinder)
	if !ok {
		http.Error(w, "archived-unmerged finder not supported by this manager", http.StatusNotImplemented)
		return
	}
	entries := f.ArchivedUnmerged(r.Context())
	if entries == nil {
		entries = []autonomous.ArchivedUnmergedEntry{}
	}
	writeJSONOK(w, map[string]any{"archived_unmerged": entries, "total": len(entries)})
}

func (s *Server) archivedUnmergedCarry(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousWrite) {
		return
	}
	c, ok := s.autonomousMgr.(archivedCarrier)
	if !ok {
		http.Error(w, "carry-forward not supported by this manager", http.StatusNotImplemented)
		return
	}
	var req struct {
		Actor string `json:"actor"`
		Force bool   `json:"force"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	actor := strings.TrimSpace(req.Actor)
	if actor == "" {
		actor = archivedUnmergedDefaultActor
	}
	draft, err := c.CarryForwardArchivedForced(id, actor, req.Force)
	if err != nil {
		code := http.StatusConflict
		if strings.Contains(err.Error(), "not found") {
			code = http.StatusNotFound
		}
		http.Error(w, err.Error(), code)
		return
	}
	s.audit(r.Context(), "carry_forward_archived", "automaton", id, map[string]any{"draft": draft.ID, "force": req.Force})
	writeJSONOK(w, draft)
}
