// Triage read route: GET /api/autonomous/triage.
//
// The single, self-contained triage record a failed or blocked
// Automaton carries is already stored on the Automaton itself
// (see internal/autonomous/triage_record.go); this route is the one
// read that collects them: every record currently in the store, the
// Automata that are waiting for operator attention, newest first,
// beside the alert threshold in effect. It writes nothing.

package server

import (
	"net/http"
	"sort"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// triageReader is the narrow Manager surface the triage read route
// needs: the stored records and the alert threshold in effect.
type triageReader interface {
	ListTriageRecords() []*autonomous.TriageRecord
	TriageAlertMinutes() int
}

// handleAutonomousTriage serves GET /api/autonomous/triage. The body is
// the triage records currently stored on the Automata that are waiting
// (failed or blocked), newest first, next to the alert threshold in
// whole minutes ("alert_minutes"): the number of minutes an Automation
// may sit in a waiting state before the triage alert is raised (0 when
// the alert is off).
//
// Optional filters, all exact matches and combinable:
//
//	automation_id — only the records of this Automation
//	prd_id        — only the record of this Automaton
//	status        — "failed" or "blocked" only; anything else is a 400
//
// A filter that matches nothing returns an empty list, never a 404; the
// route is a read, so a peer the federation policy does not grant
// autonomous:read gets a 403.
func (s *Server) handleAutonomousTriage(w http.ResponseWriter, r *http.Request) {
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
	tr, ok := s.autonomousMgr.(triageReader)
	if !ok {
		http.Error(w, "triage records unavailable", http.StatusServiceUnavailable)
		return
	}

	automID := r.URL.Query().Get("automation_id")
	prdID := r.URL.Query().Get("prd_id")
	status := r.URL.Query().Get("status")
	if status != "" && status != "failed" && status != "blocked" {
		http.Error(w, `status must be "failed" or "blocked"`, http.StatusBadRequest)
		return
	}

	records := tr.ListTriageRecords()
	out := make([]*autonomous.TriageRecord, 0, len(records))
	for _, rec := range records {
		if rec == nil {
			continue
		}
		if automID != "" && rec.AutomationID != automID {
			continue
		}
		if prdID != "" && rec.PRDID != prdID {
			continue
		}
		if status != "" && rec.Status != status {
			continue
		}
		out = append(out, rec)
	}
	// The store lists records in Automaton (PRD) order; the route
	// promises newest first, so re-sort by the record's own refresh time.
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})

	writeJSONOK(w, struct {
		Records      []*autonomous.TriageRecord `json:"records"`
		AlertMinutes int                        `json:"alert_minutes"`
		Held         autonomous.HeldGroup       `json:"held"`
	}{out, tr.TriageAlertMinutes(), triageHeldGroup(s.autonomousMgr, automID, prdID, status)})
}

// triageHeldReader is the optional read surface for the held group.
// *autonomous.API provides it; a manager without it reads as no held
// Automata.
type triageHeldReader interface {
	HeldAutomata() autonomous.HeldGroup
}

// triageHeldGroup returns the held group for the triage read, narrowed by
// the same filters as the records. A held Automaton is neither failed nor
// blocked, so a status filter leaves the group empty; the Automation and
// Automaton filters keep only the matching held Automata. The lists are
// never nil.
func triageHeldGroup(mgr any, automationID, prdID, status string) autonomous.HeldGroup {
	empty := autonomous.HeldGroup{Held: []autonomous.HeldView{}}
	hr, ok := mgr.(triageHeldReader)
	if !ok || status != "" {
		return empty
	}
	g := hr.HeldAutomata()
	out := empty
	for _, v := range g.Held {
		if automationID != "" && v.AutomationID != automationID {
			continue
		}
		if prdID != "" && v.ID != prdID {
			continue
		}
		out.Held = append(out.Held, v)
		out.Total++
		if v.Expired {
			out.Expired++
		}
	}
	return out
}

// TriageRESTOperation is one triage REST route, read by the parity test.
type TriageRESTOperation struct {
	Name   string
	Method string
	Path   string
}

// TriageRESTOperations is the single list of triage REST operations. The
// triage read is the only route in this list: a read, with no write
// siblings, and the parity test checks its surface decisions against it.
func TriageRESTOperations() []TriageRESTOperation {
	return []TriageRESTOperation{
		{"triage", http.MethodGet, "/api/autonomous/triage"},
	}
}
