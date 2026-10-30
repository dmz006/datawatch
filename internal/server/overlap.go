// Overlap read route: GET /api/autonomous/overlap.
//
// The read an operator or an agent runs before it creates an Automaton:
// the same finder the create route runs, given a title and a spec, with no
// Automaton excluded. The route writes nothing. It works in every
// create-search mode, because it is a read; the mode in force is reported
// beside the warnings so the caller can tell whether the create route would
// act on them.

package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// Limits on the number of warnings the overlap read returns.
const (
	overlapReadDefaultLimit = 10
	overlapReadMaxLimit     = 50
)

// overlapReadReply is the body of GET /api/autonomous/overlap.
type overlapReadReply struct {
	// Warnings are the overlaps found, best match first, at most the limit.
	Warnings []autonomous.OverlapWarning `json:"warnings"`
	// Threshold is the match threshold in force.
	Threshold float64 `json:"threshold"`
	// Mode is the search-before-create mode in force (off, shadow or on).
	Mode string `json:"mode"`
}

// handleAutonomousOverlap serves GET /api/autonomous/overlap.
//
// Query parameters:
//
//	title — the title of the plan about to be created
//	spec  — the spec of the plan about to be created
//	limit — optional, the most warnings returned: 1 to 50, default 10
//
// At least one of title and spec must be non-blank; both blank is a 400, as
// is a limit that is not a whole number from 1 to 50. The route is a read, so
// a federated peer needs the autonomous read capability, and a search that
// fails answers 500 rather than an empty list.
func (s *Server) handleAutonomousOverlap(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}

	q := r.URL.Query()
	title := strings.TrimSpace(q.Get("title"))
	spec := strings.TrimSpace(q.Get("spec"))
	if title == "" && spec == "" {
		http.Error(w, "title or spec is required", http.StatusBadRequest)
		return
	}
	limit := overlapReadDefaultLimit
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > overlapReadMaxLimit {
			http.Error(w, "limit must be a whole number from 1 to 50", http.StatusBadRequest)
			return
		}
		limit = n
	}

	// No ID is set, so nothing is excluded, and no project directory is
	// set, so the finder reads the worktree base repository the way the
	// create route does for an Automaton that names none.
	settings := s.createSearchSettings()
	threshold := settings.EffectiveCreateSearchThreshold()
	warnings, ok := s.runCreateSearch(&autonomous.PRD{Title: title, Spec: spec}, threshold)
	if !ok {
		http.Error(w, "overlap search failed", http.StatusInternalServerError)
		return
	}
	if len(warnings) > limit {
		warnings = warnings[:limit]
	}

	writeJSONOK(w, overlapReadReply{
		Warnings:  warnings,
		Threshold: threshold,
		Mode:      settings.EffectiveCreateSearchMode(),
	})
}
