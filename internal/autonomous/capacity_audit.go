// Audit recorder for the capacity and budget machinery: admission waits and
// releases, usage back-off set and clear, budget breaches and budget window
// rollovers. It builds the audit entries (actor, subject, values) and hands
// them to an injected sink, so the decisions about what an event says live in
// one place and tests drive it with a fake clock and an in-memory sink.
package autonomous

import (
	"sort"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/inference"
)

// Actors recorded on capacity audit entries.
const (
	// AuditActorAutonomous marks an event the daemon produced on its own: a
	// wait, an admission, rate-limit back-off, expiry, a budget transition.
	AuditActorAutonomous = "autonomous"
	// AuditActorOperator marks a back-off set or cleared by hand through the
	// REST, MCP, CLI or comm-channel surface.
	AuditActorOperator = "operator"
)

// CapacityAudit records capacity and budget events. A nil *CapacityAudit and
// a nil Sink both drop events, so callers never need to guard a call.
type CapacityAudit struct {
	// Clock supplies the time stamped on each entry; defaults to time.Now.
	Clock func() time.Time
	// Sink receives each finished entry; nil drops it.
	Sink func(audit.Entry)
}

// NewCapacityAudit returns a recorder writing to sink on the wall clock.
func NewCapacityAudit(sink func(audit.Entry)) *CapacityAudit {
	return &CapacityAudit{Clock: time.Now, Sink: sink}
}

// Now returns the recorder's current time (the wall clock when none is set).
func (r *CapacityAudit) Now() time.Time {
	if r == nil || r.Clock == nil {
		return time.Now()
	}
	return r.Clock()
}

// Record writes one entry with the given actor, action, subject and details.
func (r *CapacityAudit) Record(actor, action, subject string, details map[string]any) {
	if r == nil || r.Sink == nil {
		return
	}
	r.Sink(audit.Entry{Timestamp: r.Now(), Actor: actor, Action: action, Subject: subject, Details: details})
}

// AdmissionWait records that a task started waiting for admission, or that
// its wait cause changed. cause is the classified cause (empty when the
// reason is not a known one); reason is the full wait text.
func (r *CapacityAudit) AdmissionWait(taskID, prdID string, cause WaitCause, reason string, pools []string) {
	r.Record(AuditActorAutonomous, audit.ActionCapacityWait, taskID, map[string]any{
		"prd_id": prdID,
		"cause":  string(cause),
		"reason": reason,
		"pools":  sortedPools(pools),
	})
}

// AdmissionAdmitted records that a waiting task was admitted after waiting
// since the given time.
func (r *CapacityAudit) AdmissionAdmitted(taskID string, since time.Time, pools []string) {
	r.Record(AuditActorAutonomous, audit.ActionCapacityAdmitted, taskID, map[string]any{
		"waited_seconds": r.Now().Sub(since).Seconds(),
		"pools":          sortedPools(pools),
	})
}

// BackoffSet records a back-off flag set on backend until the given time. The
// actor is AuditActorAutonomous for rate-limit detection and AuditActorOperator
// (or the operator-surface caller's own actor) for a flag set by hand; source
// is the Backoff source constant.
func (r *CapacityAudit) BackoffSet(actor, backend string, until time.Time, source string) {
	r.Record(actor, audit.ActionUsageBackoffSet, backend, map[string]any{
		"backend": backend,
		"until":   until.UTC().Format(time.RFC3339),
		"source":  source,
	})
}

// BackoffCleared records a back-off flag cleared by hand: one backend, or
// "all".
func (r *CapacityAudit) BackoffCleared(actor, backend string) {
	r.Record(actor, audit.ActionUsageBackoffCleared, backend, map[string]any{"backend": backend})
}

// BackoffAutoCleared records the sweeper clearing a back-off flag whose
// deadline passed.
func (r *CapacityAudit) BackoffAutoCleared(backend string, until time.Time) {
	r.Record(AuditActorAutonomous, audit.ActionUsageBackoffAutoCleared, backend, map[string]any{
		"backend": backend,
		"until":   until.UTC().Format(time.RFC3339),
	})
}

// BudgetBreach records an LLM's budget window filling. rollsOverAt is nil
// when the ledger cannot say when it frees up.
func (r *CapacityAudit) BudgetBreach(llm string, spendUSD, ceilingUSD float64, rollsOverAt *time.Time) {
	details := map[string]any{"llm": llm, "spend_usd": spendUSD, "ceiling_usd": ceilingUSD}
	if rollsOverAt != nil {
		details["rolls_over_at"] = rollsOverAt.UTC().Format(time.RFC3339)
	}
	r.Record(AuditActorAutonomous, audit.ActionBudgetCeilingWait, llm, details)
}

// BudgetResumed records an LLM's budget window no longer being full.
func (r *CapacityAudit) BudgetResumed(llm string, spendUSD, ceilingUSD float64) {
	r.Record(AuditActorAutonomous, audit.ActionBudgetWindowResumed, llm, map[string]any{
		"llm": llm, "spend_usd": spendUSD, "ceiling_usd": ceilingUSD,
	})
}

// BudgetRollover records the budget window boundary passing and spend
// resetting: previousSpendUSD is what the closed window had spent.
func (r *CapacityAudit) BudgetRollover(llm string, previousSpendUSD, ceilingUSD float64, newWindowStart time.Time) {
	r.Record(AuditActorAutonomous, audit.ActionBudgetWindowRollover, llm, map[string]any{
		"previous_spend_usd": previousSpendUSD,
		"ceiling_usd":        ceilingUSD,
		"new_window_start":   newWindowStart.UTC().Format(time.RFC3339),
	})
}

// sortedPools returns pools sorted, without touching the caller's slice, so
// the same pool set always records the same way.
func sortedPools(pools []string) []string {
	out := append([]string(nil), pools...)
	sort.Strings(out)
	return out
}

// SetCapacityAudit wires the recorder admit uses for admission waits and
// releases (nil drops them).
func (m *Manager) SetCapacityAudit(a *CapacityAudit) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.capAudit = a
}

func (m *Manager) capacityAudit() *CapacityAudit {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.capAudit
}

// BudgetRolloverWatcher notices a weekly budget window boundary passing and
// records the spend reset. Feed it the ledger's statuses on every check; it
// remembers each LLM's window start and last observed spend. A rolling window
// has no boundary (it slides continuously), so only weekly windows are
// watched, and the first sighting of an LLM only records its state.
type BudgetRolloverWatcher struct {
	Audit *CapacityAudit

	seen map[string]budgetWindowSeen
}

type budgetWindowSeen struct {
	start time.Time
	spend float64
}

// Observe compares statuses with the previous call and records one rollover
// per LLM whose window start moved forward. previous_spend_usd is the spend
// seen at the last check before the boundary. LLMs absent from statuses are
// forgotten.
func (w *BudgetRolloverWatcher) Observe(statuses []BudgetStatus) {
	if w.seen == nil {
		w.seen = map[string]budgetWindowSeen{}
	}
	present := map[string]bool{}
	for _, st := range statuses {
		if st.Window != inference.BudgetModeWeekly {
			continue
		}
		present[st.LLM] = true
		prev, ok := w.seen[st.LLM]
		if ok && st.WindowStart.After(prev.start) {
			w.Audit.BudgetRollover(st.LLM, prev.spend, st.CeilingUSD, st.WindowStart)
		}
		w.seen[st.LLM] = budgetWindowSeen{start: st.WindowStart, spend: st.SpendUSD + st.PeerSpendUSD}
	}
	for llm := range w.seen {
		if !present[llm] {
			delete(w.seen, llm)
		}
	}
}
