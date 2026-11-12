// The daemon's cost-report REST surface (cost_report block): one route,
// GET /api/cost/report, running the costreport engine over the daemon's
// recorded sessions — token totals per group_by key always, USD estimates
// where the daemon has a rate configured for the group's backends, and the
// row's billing-mode fields where the operator has configured usage.billing
// profiles.
//
// group_by / since / until are all optional query params; absent values
// fall back to the daemon's cost_report config block (EffectiveGroupBy,
// EffectiveWindowDays, EffectiveShowUSDEstimates, ...), the same config the
// CLI and the MCP cost_report tool read. The REST response is the same
// costreport.Report shape the engine returns; the engine flags a request
// that spans a wider window than the cost ledger keeps (Report.
// LifetimeFallbackApplied) rather than silently zeroing it.
//
// Attribution: every session the cost report prices is also tried for an
// owner. When the daemon's autonomous manager is wired in the shape the
// daemon uses (autonomous.NewAPI(mgr)), a session is attributed to the PRD
// (automaton) that owns it, the owning PRD's automation (by PRD.AutomationID
// or by member scan), and the release the owner shipped in — the same
// session→PRD linkage PRDCostUSD uses (Manager.PRDSessionIDs, which walks
// worker, verifier and judge sessions) — so a cost report row for
// group_by=automaton prices the same spend the PRD view prices. Sessions
// with no recorded owner land in the engine's "unattributed" row, and work
// whose owner has not shipped lands in "unreleased" for group_by=release.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/costreport"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/spendledger"
	"github.com/dmz006/datawatch/internal/trace"
)

// managerCostSource feeds costreport.Build with the daemon's recorded
// sessions: one costreport.Record per session that has usage, stamped
// with the session's last update time and owned (automaton / automation /
// release) where the autonomous manager resolves one. The same adapter
// shape the MCP cost_report tool uses, plus the ShippedSource method
// (ShippedUnits) for group_by=release.
type managerCostSource struct {
	ledger spendLedgerReader
	owner  map[string]costOwner
	// ownerByAutomaton — automaton id → owner, so ledger entries of
	// deleted sessions still resolve automation/release.
	ownerByAutomaton map[string]costOwner
	// auto — the daemon's autonomous manager for the shipped-state reads
	// (nil when not wired: released grouping then reports only the
	// "unreleased" row and rows are unattributed).
	auto *autonomous.Manager
}

// costOwner is what an adapter knows a session's owner to be; empty fields
// mean "no recorded owner" (unattributed / unshipped).
type costOwner struct {
	automaton  string // prd id — the automaton
	automation string // owning automation id
	release    string // release the owner shipped in ("" = unshipped)
}

// spendLedgerReader is the slice of spendledger.Ledger the source reads;
// tests supply a fake.
type spendLedgerReader interface {
	Entries() []spendledger.Entry
}

// Records reads the spend ledger, not live session records, so spend of
// sessions that were since deleted still counts. Ownership comes from the
// entry's own automaton id (resolved to automation/release through the
// PRD map); an entry with no automaton id falls back to the session→owner
// map and otherwise stays unattributed. Ledger entries are this daemon's
// own spend, so none is marked Peer; peer spend, when a peer source is
// merged in, is flagged Record.Peer and filtered by the engine.
func (src managerCostSource) Records(opts costreport.SourceOptions) (*costreport.SourceResult, error) {
	res := &costreport.SourceResult{}
	if src.ledger == nil {
		return res, nil
	}
	for _, e := range src.ledger.Entries() {
		at := e.EndedAt
		if at.IsZero() {
			at = e.StartedAt
		}
		if !inWindow(at, opts.Since, opts.Until) {
			continue
		}
		rec := costreport.Record{
			SessionID:         e.SessionID,
			FromLedger:        true,
			LedgerAutomatonID: e.AutomatonID,
			AutomatonID:       e.AutomatonID,
			TaskID:            e.TaskID,
			Role:              e.Role,
			Backend:           e.Backend,
			At:                at,
			TokensIn:          int(e.TokensIn),
			TokensOut:         int(e.TokensOut),
			CacheRead:         int(e.CacheRead),
			CacheCreation:     int(e.CacheCreation),
			ToolCalls:         int(e.ToolCalls),
			FirstActivity:     e.StartedAt,
			LastActivity:      e.EndedAt,
			ActiveSeconds:     e.ActiveSeconds,
		}
		o, ok := costOwner{}, false
		if e.AutomatonID != "" {
			o, ok = src.ownerByAutomaton[e.AutomatonID]
			if !ok {
				o, ok = costOwner{automaton: e.AutomatonID}, true
			}
		} else {
			o, ok = src.owner[e.SessionID]
		}
		if ok {
			rec.AutomatonID = o.automaton
			rec.AutomationID = o.automation
			rec.Release = o.release
		}
		res.Records = append(res.Records, rec)
	}
	return res, nil
}

// ShippedUnits implements costreport.ShippedSource: every PRD and
// Automation the daemon knows shipped (MergedAt recorded), for
// group_by=release. The unit list is non-nil even when empty (the
// contract ShippedResult documents); when the manager is not wired it is
// empty and the engine's release rows all land under "unreleased".
func (src managerCostSource) ShippedUnits() (*costreport.ShippedResult, error) {
	res := &costreport.ShippedResult{Units: []costreport.ShippedUnit{}}
	if src.auto == nil {
		return res, nil
	}
	for _, p := range src.auto.Store().ListPRDs() {
		if p == nil || !p.IsShipped() {
			continue
		}
		res.Units = append(res.Units, costreport.ShippedUnit{
			UnitID:  p.ID,
			Kind:    "prd",
			Release: p.MergedInRelease,
		})
	}
	for _, a := range src.auto.ListAutomations() {
		if a == nil || !a.IsShipped() {
			continue
		}
		res.Units = append(res.Units, costreport.ShippedUnit{
			UnitID:  a.ID,
			Kind:    "automation",
			Release: a.MergedInRelease,
		})
	}
	return res, nil
}

// inWindow — a zero bound means "no bound on that side"; a record with a
// zero At is always included (there is no better time, and excluding it
// would silently drop spend).
func inWindow(at, since, until time.Time) bool {
	if at.IsZero() {
		return true
	}
	if !since.IsZero() && at.Before(since) {
		return false
	}
	if !until.IsZero() && at.After(until) {
		return false
	}
	return true
}

// SetCostReportLedger wires the spend ledger the cost report reads.
func (s *Server) SetCostReportLedger(l *spendledger.Ledger) {
	if l == nil {
		s.costReportLedger = nil
		return
	}
	s.costReportLedger = l
}

// costReportAutonomousManager returns the daemon's autonomous manager when
// it is wired in the shape the daemon uses (autonomous.NewAPI(mgr), see
// main.go's SetAutonomousAPI call); nil otherwise. The cost report's
// PRD/automation ownership and shipped-state reads go through the Manager
// (Store().ListPRDs, PRDSessionIDs, ListAutomations), not the
// AutonomousAPI surface: the manager is where the PRD ownership and the
// shipped state live.
func (s *Server) costReportAutonomousManager() *autonomous.Manager {
	if s == nil || s.autonomousMgr == nil {
		return nil
	}
	if api, ok := s.autonomousMgr.(*autonomous.API); ok {
		return api.M
	}
	return nil
}

// costReportOwners maps session id → costOwner (automaton / automation /
// release): every PRD's sessions (worker, verifier and judge — exactly the
// set Manager.PRDSessionIDs sums for PRDCostUSD) get the PRD as their
// automaton, the owning automation (PRD.AutomationID, else the automation
// the PRD is a member of), and the release the owner shipped in. First PRD
// in store order wins a session's ownership (deterministic).
func (s *Server) costReportOwners(mgr *autonomous.Manager) map[string]costOwner {
	owners := map[string]costOwner{}
	if mgr == nil {
		return owners
	}

	// automationByPrd — member scan: a PRD that is an automation member but
	// has no AutomationID field set still owns the automation's spend.
	automationByPrd := map[string]*autonomous.Automation{}
	for _, a := range mgr.ListAutomations() {
		if a == nil {
			continue
		}
		for _, mem := range a.Members {
			if mem.PRDID != "" {
				if _, ok := automationByPrd[mem.PRDID]; !ok {
					automationByPrd[mem.PRDID] = a
				}
			}
		}
	}

	prds := mgr.Store().ListPRDs() // store order (insertion), deterministic
	for _, p := range prds {
		if p == nil {
			continue
		}
		o := costOwner{automaton: p.ID}
		var auto *autonomous.Automation
		if p.AutomationID != "" {
			o.automation = p.AutomationID
			if got, ok := mgr.GetAutomation(p.AutomationID); ok {
				auto = got
			}
		}
		if auto == nil {
			auto = automationByPrd[p.ID]
			if auto != nil {
				o.automation = auto.ID
			}
		}
		o.release = earliestShippedRelease(p, auto)
		for _, sid := range mgr.PRDSessionIDs(p.ID) {
			if sid == "" {
				continue
			}
			if _, ok := owners[sid]; !ok {
				owners[sid] = o
			}
		}
	}
	return owners
}

// earliestShippedRelease picks the release of the earliest-shipped owner:
// the PRD's own shipped release when it merged first, else its
// automation's (a tie goes to the PRD). An unshipped owner contributes
// nothing; both unshipped → "" (the record lands in the "unreleased" row).
func earliestShippedRelease(p *autonomous.PRD, auto *autonomous.Automation) string {
	prdRel, prdAt := "", (*time.Time)(nil)
	if p != nil && p.IsShipped() {
		prdRel, prdAt = p.MergedInRelease, p.MergedAt
	}
	autoRel, autoAt := "", (*time.Time)(nil)
	if auto != nil && auto.IsShipped() {
		autoRel, autoAt = auto.MergedInRelease, auto.MergedAt
	}
	switch {
	case prdRel == "" && autoRel == "":
		return ""
	case autoRel == "":
		return prdRel
	case prdRel == "":
		return autoRel
	default:
		if prdAt != nil && (autoAt == nil || !prdAt.After(*autoAt)) {
			return prdRel
		}
		return autoRel
	}
}

// handleCostReport — GET /api/cost/report. Validate the optional
// group_by (must be in the engine's vocabulary), since/until bounds,
// resolve the window from the request (falling back to
// costreport.cfg.EffectiveWindowDays when the request bounded neither
// side, and now as the upper bound when it omitted until), run
// costreport.Build over the daemon's sessions and return the Report as
// JSON.
func (s *Server) handleCostReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAnalyticsRead) {
		return
	}
	if s.manager == nil {
		http.Error(w, "manager not available", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()

	req := costreport.Request{}
	if v := strings.TrimSpace(q.Get("group_by")); v != "" {
		gb := costreport.GroupBy(v)
		switch gb {
		case costreport.GroupByAutomaton, costreport.GroupByAutomation, costreport.GroupByBackend,
			costreport.GroupByRelease, costreport.GroupByWeek:
			req.GroupBy = gb
		default:
			http.Error(w, `group_by must be one of automaton, automation, backend, release, week`, http.StatusBadRequest)
			return
		}
	}
	if v := q.Get("since"); v != "" {
		t, err := parseCostBound(v)
		if err != nil {
			http.Error(w, "since must be an RFC3339 or YYYY-MM-DD timestamp", http.StatusBadRequest)
			return
		}
		req.Since = t
	}
	if v := q.Get("until"); v != "" {
		t, err := parseCostBound(v)
		if err != nil {
			http.Error(w, "until must be an RFC3339 or YYYY-MM-DD timestamp", http.StatusBadRequest)
			return
		}
		req.Until = t
	}

	mgr := s.costReportAutonomousManager()
	owners := s.costReportOwners(mgr)
	byAutomaton := map[string]costOwner{}
	for _, o := range owners {
		if _, ok := byAutomaton[o.automaton]; !ok {
			byAutomaton[o.automaton] = o
		}
	}
	src := managerCostSource{
		ledger:           s.costReportLedger,
		owner:            owners,
		ownerByAutomaton: byAutomaton,
		auto:             mgr,
	}

	cr := config.CostReportConfig{}
	var billing costreport.BillingFunc
	if s.cfg != nil {
		cr = s.cfg.CostReport
		billing = s.resolvedBilling()
	}

	rep, err := costreport.Build(cr, req, src, src, s.exposedRate, billing, time.Now())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(costReportResponse{Report: rep, TraceUsage: s.traceUsageSummary()})
}

// costReportResponse is the Report plus the additive trace_usage field.
// Every existing Report field and number is unchanged; trace_usage is
// omitted when no trace store is wired or it holds no model requests.
type costReportResponse struct {
	*costreport.Report
	TraceUsage *trace.UsageSummary `json:"trace_usage,omitempty"`
}

// traceUsageSummary totals traced model requests and tokens, or nil when
// there is nothing to report.
func (s *Server) traceUsageSummary() *trace.UsageSummary {
	if s.traceStore == nil {
		return nil
	}
	evs, err := s.traceStore.ReadAll()
	if err != nil {
		return nil
	}
	sum := trace.SummarizeUsage(evs)
	if sum.Total.ModelRequests == 0 {
		return nil
	}
	return &sum
}

// parseCostBound — since/until accept RFC3339 or bare YYYY-MM-DD (the two
// forms the CLI's help text documents). A bare YYYY-MM-DD is the start of
// that day UTC.
func parseCostBound(v string) (time.Time, error) {
	raw := strings.TrimSpace(v)
	if raw == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("invalid bound %q: want RFC3339 or YYYY-MM-DD", raw)
}

// resolvedBilling wires the cost report to each backend's LLM entry billing
// mode (config.ResolveBillingMode, which falls back to usage.billing).
func (s *Server) resolvedBilling() costreport.BillingFunc {
	return costreport.ResolvedBilling(s.cfg, func(backend string) (inference.LLM, bool) {
		if s.inferenceReg == nil {
			return inference.LLM{}, false
		}
		l, err := s.inferenceReg.Get(backend)
		if err != nil || l == nil {
			return inference.LLM{}, false
		}
		return *l, true
	})
}
