// Read route for the merged-member decisions of the Automata.
//
//	GET /api/automations/merged-members       — every Automaton's decisions
//	GET /api/automations/{id}/merged-members  — one Automaton's decisions
//
// Each row names the member, its branch, the default branch it was proved
// merged into, the proving commit, the policy in force, the outcome (advanced
// or blocked) and the time. The route only reads the Automation decision log.
package server

import (
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// mergedMemberLister is the optional AutonomousAPI extension that lists the
// merged-member decisions; an empty id lists every Automaton's.
type mergedMemberLister interface {
	MergedMembers(id string) ([]autonomous.MergedMemberRow, error)
}

func (s *Server) handleMergedMembers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	lister, ok := s.autonomousMgr.(mergedMemberLister)
	if !ok {
		http.Error(w, "merged-member view not available", http.StatusServiceUnavailable)
		return
	}
	// /api/automations/merged-members lists all; /api/automations/{id}/merged-members one.
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/automations"), "/")
	id := ""
	if rest != "merged-members" {
		id = strings.TrimSuffix(rest, "/merged-members")
	}
	rows, err := lister.MergedMembers(id)
	if err != nil {
		code, msg := automationErrStatus(err)
		http.Error(w, msg, code)
		return
	}
	if rows == nil {
		rows = []autonomous.MergedMemberRow{}
	}
	writeJSONOK(w, map[string]any{"automation_id": id, "merged_members": rows, "count": len(rows)})
}
