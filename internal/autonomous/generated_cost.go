// Ledger-backed cost for the merged change record: the Automaton's spend is
// read from the spend ledger through the same costreport engine the cost
// report uses, then attached with changerecord.AttachCost.

package autonomous

import (
	"sort"
	"time"

	"github.com/dmz006/datawatch/internal/changerecord"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/costreport"
	"github.com/dmz006/datawatch/internal/spendledger"
)

// LedgerEntries is the slice of spendledger.Ledger the cost reader needs.
type LedgerEntries interface {
	Entries() []spendledger.Entry
}

// ledgerCost implements changerecord.CostLedger over the spend ledger.
type ledgerCost struct {
	ledger  LedgerEntries
	cfg     func() config.CostReportConfig
	rate    costreport.RateFunc
	billing costreport.BillingFunc
}

type ledgerCostSource struct{ entries []spendledger.Entry }

func (s ledgerCostSource) Records(_ costreport.SourceOptions) (*costreport.SourceResult, error) {
	res := &costreport.SourceResult{}
	for _, e := range s.entries {
		last := e.EndedAt
		if last.IsZero() {
			last = e.StartedAt
		}
		res.Records = append(res.Records, costreport.Record{
			SessionID: e.SessionID, AutomatonID: e.AutomatonID, TaskID: e.TaskID, Role: e.Role,
			FromLedger: true, LedgerAutomatonID: e.AutomatonID, Backend: e.Backend, At: last,
			TokensIn: int(e.TokensIn), TokensOut: int(e.TokensOut), CacheRead: int(e.CacheRead),
			CacheCreation: int(e.CacheCreation), ToolCalls: int(e.ToolCalls),
			FirstActivity: e.StartedAt, LastActivity: last, ActiveSeconds: e.ActiveSeconds,
		})
	}
	return res, nil
}

func (l ledgerCost) AutomatonCost(id string) (*changerecord.CostSummary, error) {
	var mine []spendledger.Entry
	ids := map[string]bool{}
	for _, e := range l.ledger.Entries() {
		if e.AutomatonID == id {
			mine = append(mine, e)
			ids[e.SessionID] = true
		}
	}
	cr := config.CostReportConfig{}
	if l.cfg != nil {
		cr = l.cfg()
	}
	now := time.Now()
	rep, err := costreport.Build(cr, costreport.Request{GroupBy: costreport.GroupByAutomaton, Since: time.Unix(0, 0), Until: now},
		ledgerCostSource{entries: mine}, nil, l.rate, l.billing, now)
	if err != nil {
		return nil, err
	}
	out := &changerecord.CostSummary{}
	for _, r := range rep.Rows {
		if r.Key != id {
			continue
		}
		out.TokensIn, out.TokensOut, out.CacheRead, out.CacheCreation = r.TokensIn, r.TokensOut, r.CacheRead, r.CacheCreation
		out.ToolCalls, out.SpanSeconds = r.ToolCalls, r.SpanSeconds
		out.USDLabel = r.USDLabel
		out.APIEquivalentUSD = r.APIEquivalentUSD
		if out.APIEquivalentUSD == nil {
			out.USDLabel = ""
		}
		out.WindowNote = r.WindowNote
		out.UnknownNote = r.SpendUnknownNote
	}
	for s := range ids {
		out.SessionIDs = append(out.SessionIDs, s)
	}
	sort.Strings(out.SessionIDs)
	out.Sessions = len(out.SessionIDs)
	if out.UnknownNote == "" {
		out.UnknownNote = rep.UnknownSpendNote
	}
	return out, nil
}

// SetGeneratedCostLedger wires the spend ledger the merge report's cost
// section reads. Nil leaves the report without a cost section.
func (m *Manager) SetGeneratedCostLedger(l changerecord.CostLedger) {
	m.genArt.mu.Lock()
	defer m.genArt.mu.Unlock()
	m.genArt.cost = l
}

// NewSpendLedgerCost adapts a spend ledger (plus the cost-report config,
// rate and billing lookups) to changerecord.CostLedger.
func NewSpendLedgerCost(l LedgerEntries, cfg func() config.CostReportConfig, rate costreport.RateFunc, billing costreport.BillingFunc) changerecord.CostLedger {
	return ledgerCost{ledger: l, cfg: cfg, rate: rate, billing: billing}
}

// attachGeneratedCost reads the Automaton's cost onto rec. A ledger error is
// logged and leaves the record without a cost section.
func (m *Manager) attachGeneratedCost(rec *changerecord.ChangeRecord) {
	m.genArt.mu.Lock()
	l := m.genArt.cost
	m.genArt.mu.Unlock()
	if l == nil || rec == nil {
		return
	}
	if err := changerecord.AttachCost(rec, l); err != nil {
		logGeneratedCostErr(rec.AutomatonID, err)
	}
}
