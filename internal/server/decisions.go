// Decision line read route: GET /api/autonomous/decisions.
//
// The one read of operator decisions, newest first: each decision with its
// state, who asked and when, the tasks of its Automaton whose spec names it,
// the ones held on it right now, and the current answer verbatim. It reads
// the question store (see internal/autonomous/decision_line.go), so a
// withdrawn or expired decision shows too. It writes nothing.

package server

import (
	"net/http"
	"strconv"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// handleAutonomousDecisions serves GET /api/autonomous/decisions. The body
// is {"decisions": [...]}, newest first.
//
// Optional filters, all exact matches and combinable:
//
//	prd_id — only the decisions linked to this Automaton
//	state  — "open", "answered", "withdrawn" or "expired"; anything else is a 400
//	limit  — at most this many entries (default 100, maximum 1000)
//
// A filter that matches nothing returns an empty array, never a 404; the
// route is a read, so a peer the federation policy does not grant
// autonomous:read gets a 403.
func (s *Server) handleAutonomousDecisions(w http.ResponseWriter, r *http.Request) {
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
	if s.questionStore == nil {
		http.Error(w, "decisions unavailable", http.StatusServiceUnavailable)
		return
	}

	q := r.URL.Query()
	filter := autonomous.QuestionFilter{PRDID: q.Get("prd_id")}
	if v := q.Get("state"); v != "" {
		switch st := autonomous.QuestionState(v); st {
		case autonomous.QuestionOpen, autonomous.QuestionAnswered,
			autonomous.QuestionWithdrawn, autonomous.QuestionExpired:
			filter.State = st
		default:
			http.Error(w, `state must be one of "open", "answered", "withdrawn" or "expired"`, http.StatusBadRequest)
			return
		}
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			http.Error(w, "limit must be a positive integer", http.StatusBadRequest)
			return
		}
		filter.Limit = n // the store caps it at 1000
	}

	lookup := func(prdID string) (*autonomous.PRD, bool) {
		raw, ok := s.autonomousMgr.GetPRD(prdID)
		if !ok || isNilPRD(raw) {
			return nil, false
		}
		prd, err := toPRD(raw)
		if err != nil {
			return nil, false
		}
		return prd, true
	}
	writeJSONOK(w, struct {
		Decisions []autonomous.DecisionLineEntry `json:"decisions"`
	}{autonomous.BuildDecisionLine(filter, s.questionStore, lookup)})
}
