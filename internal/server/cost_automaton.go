// Per-Automaton cost on the Automaton detail response. The detail view
// (GET /api/autonomous/prds/{id}) carries a `cost` object read from the same
// costreport engine and spend ledger GET /api/cost/report uses, so the two
// surfaces always agree on tokens, time, sessions and tool calls.

package server

import (
	"encoding/json"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/costreport"
)

// automatonCost is the `cost` field of the Automaton detail response.
type automatonCost struct {
	costreport.Row
	// WindowNote is the report-level window note (ledger retention), when set.
	ReportWindowNote string `json:"report_window_note,omitempty"`
}

// automatonCostFor builds the cost of one Automaton over the whole ledger,
// or nil when no ledger or manager is wired. An Automaton with no recorded
// spend gets a zero row (key = id), never an absent field.
func (s *Server) automatonCostFor(id string) *automatonCost {
	if s == nil || id == "" || s.costReportLedger == nil {
		return nil
	}
	mgr := s.costReportAutonomousManager()
	owners := s.costReportOwners(mgr)
	byAutomaton := map[string]costOwner{}
	for _, o := range owners {
		if _, ok := byAutomaton[o.automaton]; !ok {
			byAutomaton[o.automaton] = o
		}
	}
	src := managerCostSource{ledger: s.costReportLedger, owner: owners, ownerByAutomaton: byAutomaton, auto: mgr}
	cr := config.CostReportConfig{}
	var billing costreport.BillingFunc
	if s.cfg != nil {
		cr = s.cfg.CostReport
		billing = func(backend string) config.EffectiveBillingProfile {
			return s.cfg.Usage.EffectiveBillingProfile(backend)
		}
	}
	now := time.Now()
	rep, err := costreport.Build(cr, costreport.Request{GroupBy: costreport.GroupByAutomaton, Since: time.Unix(0, 0), Until: now},
		src, src, s.exposedRate, billing, now)
	if err != nil || rep == nil {
		return nil
	}
	out := &automatonCost{Row: costreport.Row{Key: id}, ReportWindowNote: rep.WindowNote}
	for _, r := range rep.Rows {
		if r.Key == id {
			out.Row = r
			break
		}
	}
	return out
}

// withAutomatonCost adds `cost` to an Automaton detail payload. The payload
// is returned unchanged when there is no cost to report or it cannot be
// re-encoded.
func (s *Server) withAutomatonCost(payload any, id string) any {
	c := s.automatonCostFor(id)
	if c == nil {
		return payload
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return payload
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil || doc == nil {
		return payload
	}
	doc["cost"] = c
	return doc
}
