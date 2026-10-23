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
	// Names supplies the readable names for admission entries: the waiting
	// task's title, its PRD's title, and the wait reason spoken readably.
	// nil leaves those details as their "(untitled <short id>)" or empty
	// fallbacks.
	Names AdmissionNamesFn
}

// AdmissionNames is the readable naming for one admission entry: the waiting
// task's title, its PRD's title, and the readable wording of the wait reason
// ("pool <p> full (u/l), held by <PRD title> / <task title> (<short id>)
// and …").
type AdmissionNames struct {
	TaskTitle string
	PRDTitle  string
	Readable  string
}

// AdmissionNamesFn resolves the readable names for one admission entry: the
// ids of the waiting task and its PRD, plus the wait reason being named
// ("" for an admission that is not waiting). It should resolve through the
// shared holder-title resolver (capacity_titles.go).
type AdmissionNamesFn func(taskID, prdID, reason string) AdmissionNames

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
// reason is not a known one); reason is the full wait text. The details keep
// their existing keys and add task_id, task_title, prd_title and readable
// names, resolved through Names (the shared holder-title resolver) when it
// is set, falling back to "(untitled <short id>)" when a title is missing.
func (r *CapacityAudit) AdmissionWait(taskID, prdID string, cause WaitCause, reason string, pools []string) {
	names := r.admissionNames(taskID, prdID, reason)
	details := map[string]any{
		"prd_id":     prdID,
		"cause":      string(cause),
		"reason":     reason,
		"pools":      sortedPools(pools),
		"task_id":    taskID,
		"task_title": titleOrUntitled(names.TaskTitle, shortID(taskID)),
		"prd_title":  titleOrUntitled(names.PRDTitle, shortID(prdID)),
		"readable":   names.Readable,
	}
	// A rate-ceiling wait also names the ceilinged pool and, when the window
	// will free it, the reset time.
	if cause == WaitCauseRateCeiling {
		if pool, at, ok := RateCeilingWaitFacts(reason); ok {
			details["pool"] = pool
			if !at.IsZero() {
				details["resets_at"] = at.UTC().Format(time.RFC3339)
			}
		}
	}
	r.Record(AuditActorAutonomous, audit.ActionCapacityWait, taskID, details)
}

// AdmissionAdmitted records that a waiting task was admitted after waiting
// since the given time, with the same readable naming added.
func (r *CapacityAudit) AdmissionAdmitted(taskID string, since time.Time, pools []string) {
	names := r.admissionNames(taskID, "", "")
	r.Record(AuditActorAutonomous, audit.ActionCapacityAdmitted, taskID, map[string]any{
		"waited_seconds": r.Now().Sub(since).Seconds(),
		"pools":          sortedPools(pools),
		"task_id":        taskID,
		"task_title":     titleOrUntitled(names.TaskTitle, shortID(taskID)),
		"prd_title":      titleOrUntitled(names.PRDTitle, shortID(taskID)),
		"readable":       names.Readable,
	})
}

// admissionNames resolves the readable names through the configured Names
// function; a nil recorder or no Names (a standalone recorder) leaves them
// unset so the recorded keys take their fallbacks.
func (r *CapacityAudit) admissionNames(taskID, prdID, reason string) AdmissionNames {
	if r == nil || r.Names == nil {
		return AdmissionNames{}
	}
	return r.Names(taskID, prdID, reason)
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
// releases (nil drops them). It also gives the recorder the manager's
// holder-title resolver, so admission entries carry readable names.
func (m *Manager) SetCapacityAudit(a *CapacityAudit) {
	if a != nil && a.Names == nil {
		a.Names = m.AdmissionNames
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.capAudit = a
}

// AdmissionNames resolves the readable names for one admission entry through
// the shared holder-title resolver (capacity_titles.go): the waiting task's
// title, its PRD's, and the wait reason spoken readably with the holders
// named. A blank reason (an admission event) leaves Readable empty; unknown
// titles stay blank so the recorders take their fallbacks.
func (m *Manager) AdmissionNames(taskID, prdID, reason string) AdmissionNames {
	out := AdmissionNames{}
	if holder := m.ResolveHolder(taskID); holder.PRDID != "" {
		out.TaskTitle, out.PRDTitle = holder.TaskTitle, holder.PRDTitle
	}
	if out.PRDTitle == "" && prdID != "" && m.store != nil {
		if p, ok := m.store.GetPRD(prdID); ok {
			out.PRDTitle = p.Title
		}
	}
	if reason != "" {
		out.Readable = m.ReadableWaitSentence(reason)
	}
	return out
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
