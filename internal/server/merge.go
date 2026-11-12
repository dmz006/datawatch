// Merge read routes: GET /api/autonomous/merge and
// GET /api/autonomous/safe-to-delete.
//
// The first reports the merge-pending mode in effect, the alert threshold, one
// entry per completed Automaton with a work branch, and the shadow decision
// list. The second reports which Automaton branches and worktrees are safe to
// delete. Both are reads: neither merges, deletes or pushes, and the Automaton
// state they show comes from the Coordinator's in-memory cache.

package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// mergeSafeDeleteTimeout bounds the git work of one safe-to-delete read.
const mergeSafeDeleteTimeout = 60 * time.Second

// mergeReader is the narrow Manager surface the merge read routes need.
type mergeReader interface {
	MergeStatus() autonomous.MergeStatus
	MergeSafeToDelete(ctx context.Context) (autonomous.SafeDeleteReport, error)
}

// mergeReadGate runs the checks both merge routes share: only GET is served,
// the Automata must be enabled, a federated peer needs autonomous:read, and
// the Manager must expose the merge reads. It writes the refusal itself and
// reports whether the route may go on.
func (s *Server) mergeReadGate(w http.ResponseWriter, r *http.Request) (mergeReader, bool) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return nil, false
	}
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return nil, false
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return nil, false
	}
	mr, ok := s.autonomousMgr.(mergeReader)
	if !ok {
		http.Error(w, "Automaton merge status unavailable", http.StatusServiceUnavailable)
		return nil, false
	}
	return mr, true
}

// handleAutonomousMerge serves GET /api/autonomous/merge: the merge-pending
// mode in effect ("mode": off, shadow or on), the alert threshold in whole
// minutes ("alert_minutes"), one entry per completed Automaton with a work
// branch ("automata"), and the shadow decision list, newest first
// ("shadow_decisions"). While the mode is off both lists are empty. Any other
// method is a 405.
func (s *Server) handleAutonomousMerge(w http.ResponseWriter, r *http.Request) {
	mr, ok := s.mergeReadGate(w, r)
	if !ok {
		return
	}
	writeJSONOK(w, mr.MergeStatus())
}

// handleAutonomousSafeToDelete serves GET /api/autonomous/safe-to-delete: the
// report of Automaton branches that are safe to delete and those held back.
// It only reads; deleting stays a separate, operator-approved step. Any other
// method is a 405.
func (s *Server) handleAutonomousSafeToDelete(w http.ResponseWriter, r *http.Request) {
	mr, ok := s.mergeReadGate(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), mergeSafeDeleteTimeout)
	defer cancel()
	report, err := mr.MergeSafeToDelete(ctx)
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, autonomous.ErrMergeNoBaseRepo) {
			code = http.StatusServiceUnavailable
		}
		http.Error(w, err.Error(), code)
		return
	}
	writeJSONOK(w, report)
}

// MergeRESTOperation is one merge REST route.
type MergeRESTOperation struct {
	Name   string
	Method string
	Path   string
}

// MergeRESTOperations is the single list of merge REST operations: two reads,
// with no write siblings.
func MergeRESTOperations() []MergeRESTOperation {
	return []MergeRESTOperation{
		{"merge", http.MethodGet, "/api/autonomous/merge"},
		{"safe-to-delete", http.MethodGet, "/api/autonomous/safe-to-delete"},
	}
}
