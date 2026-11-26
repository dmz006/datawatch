// The acceptance of an exact commit for merging:
// POST /api/autonomous/prds/{id}/landing/accept. It records who accepted and
// why, the way the landing keep and drop decisions do, and audits it. It does
// not prove who the actor is.

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// mergeAccepter is the optional Manager surface behind the accept action.
type mergeAccepter interface {
	AcceptForMerge(ctx context.Context, id, commit, actor, reason string) (*autonomous.PRD, error)
}

var _ mergeAccepter = (*autonomous.API)(nil)

// mergeAcceptActor is the actor recorded: a caller the daemon can identify as
// a session or a peer is recorded as that, whatever the body names, so it
// cannot sign as someone else. Otherwise the body's actor, default operator.
func (s *Server) mergeAcceptActor(ctx context.Context, named string) string {
	if who := s.auditActor(ctx); who != "operator" {
		return who
	}
	if named = strings.TrimSpace(named); named != "" {
		return named
	}
	return "operator"
}

func (s *Server) handleMergeAccept(w http.ResponseWriter, r *http.Request, id string) {
	var req struct{ Actor, Commit, Reason string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	acc, ok := s.autonomousMgr.(mergeAccepter)
	if !ok {
		http.Error(w, "merge acceptance is not supported by this daemon", http.StatusNotImplemented)
		return
	}
	actor := s.mergeAcceptActor(r.Context(), req.Actor)
	details := map[string]any{"commit": req.Commit, "reason": req.Reason, "actor": actor}
	updated, err := acc.AcceptForMerge(r.Context(), id, req.Commit, actor, req.Reason)
	if err != nil {
		details["outcome"], details["error"] = "refused", err.Error()
		s.audit(r.Context(), "merge_accept", "automaton", id, details)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	details["outcome"] = "accepted"
	s.audit(r.Context(), "merge_accept", "automaton", id, details)
	s.writeAutonomousResult(w, r, updated, "", "", "")
}
