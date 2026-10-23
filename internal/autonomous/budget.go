package autonomous

import (
	"bufio"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/inference"
)

// budgetRetention is how long a ledger entry is kept. The longest budget
// window is 720 hours (30 days); one extra day covers a window that straddles
// the boundary.
const budgetRetention = 31 * 24 * time.Hour

// allocationBasis explains the estimated allocation figure to the reader.
const allocationBasis = "this daemon's spend in the window, priced at configured API rates, divided by the operator-declared notional allocation for the plan tier; an estimate, never used to block"

// budgetEntry is one persisted spend record, written as one JSON object per
// line.
type budgetEntry struct {
	LLM       string    `json:"llm"`
	At        time.Time `json:"at"`
	USD       float64   `json:"usd"`
	TokensIn  int       `json:"tokens_in"`
	TokensOut int       `json:"tokens_out"`
	// Session is the id of the session that incurred the spend. Entries
	// written before attribution existed have none and are never attributed.
	Session string `json:"session,omitempty"`
}

// BudgetAllocation is the optional estimated share-of-allocation figure in a
// BudgetStatus. Estimated is always true.
type BudgetAllocation struct {
	AllocationPct float64 `json:"allocation_pct"`
	Estimated     bool    `json:"estimated"`
	AllocationUSD float64 `json:"allocation_usd"`
	PlanTier      string  `json:"plan_tier"`
	Basis         string  `json:"basis"`
}

// BudgetStatus is one LLM's budget state as reported to operators.
type BudgetStatus struct {
	LLM          string            `json:"llm"`
	CeilingUSD   float64           `json:"ceiling_usd"`
	SpendUSD     float64           `json:"spend_usd"`
	PeerSpendUSD float64           `json:"peer_spend_usd"`
	RemainingUSD float64           `json:"remaining_usd"`
	Window       string            `json:"window"`
	WindowHours  int               `json:"window_hours,omitempty"`
	WindowStart  time.Time         `json:"window_start"`
	WindowEnd    time.Time         `json:"window_end"`
	RollsOverAt  *time.Time        `json:"rolls_over_at,omitempty"`
	Full         bool              `json:"full"`
	Allocation   *BudgetAllocation `json:"allocation,omitempty"`
	// Peers shows what peer spend is being counted: one line per opted-in
	// peer, plus registered peers that are not opted in.
	Peers []PeerBudgetStatus `json:"peers,omitempty"`
}

// BudgetLedger records dollar spend per LLM and answers whether an LLM's
// budget window is full. It is goroutine-safe. Spend persists to a file so a
// daemon restart does not forget it. Configure the function fields before
// first use.
type BudgetLedger struct {
	// Clock supplies the current time; defaults to time.Now.
	Clock func() time.Time
	// Budgets returns the LLM's current budget, or nil when it has none. It is
	// read at call time so registry edits take effect immediately.
	Budgets func(llm string) *inference.BudgetWindow
	// PlanTier returns the LLM's plan tier ("" when unset).
	PlanTier func(llm string) inference.PlanTier
	// PlanTierAllocations returns the autonomous.plan_tier_allocation_usd
	// table (tier name to notional dollars per window).
	PlanTierAllocations func() map[string]float64
	// LLMs lists the LLM names StatusAll should report on.
	LLMs func() []string
	// PeerSpend is an extension point for spend by federation peers opted in
	// to this budget. Nil means no peer spend. Blocked adds it to the local
	// spend; Status reports it separately.
	PeerSpend func(llm string) float64
	// PeerBreakdown lists the per-peer lines Status reports alongside
	// PeerSpend. Nil means no breakdown.
	PeerBreakdown func(llm string) []PeerBudgetStatus

	path string

	mu      sync.Mutex
	loaded  bool
	entries []budgetEntry
}

// NewBudgetLedger returns a ledger persisting to path. The file is read, and
// entries older than 31 days dropped, on first use.
func NewBudgetLedger(path string) *BudgetLedger {
	return &BudgetLedger{Clock: time.Now, path: path}
}

func (b *BudgetLedger) now() time.Time {
	if b.Clock == nil {
		return time.Now()
	}
	return b.Clock()
}

// ensureLoaded reads the persistence file once. Caller holds b.mu.
func (b *BudgetLedger) ensureLoaded() {
	if b.loaded {
		return
	}
	b.loaded = true
	if b.path == "" {
		return
	}
	f, err := os.Open(b.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[budget] read %s: %v", b.path, err)
		}
		return
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	cutoff := b.now().Add(-budgetRetention)
	pruned := false
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e budgetEntry
		if json.Unmarshal(line, &e) != nil || e.LLM == "" {
			pruned = true
			continue
		}
		if e.At.Before(cutoff) {
			pruned = true
			continue
		}
		b.entries = append(b.entries, e)
	}
	if pruned {
		b.rewriteLocked()
	}
}

// Record adds one spend entry that is not attributed to a session.
func (b *BudgetLedger) Record(llm string, at time.Time, usd float64, tokensIn, tokensOut int) {
	b.RecordSession(llm, "", at, usd, tokensIn, tokensOut)
}

// RecordSession adds one spend entry attributed to sessionID (empty for none)
// in memory and appends it to the file.
func (b *BudgetLedger) RecordSession(llm, sessionID string, at time.Time, usd float64, tokensIn, tokensOut int) {
	if llm == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ensureLoaded()
	e := budgetEntry{LLM: llm, At: at, USD: usd, TokensIn: tokensIn, TokensOut: tokensOut, Session: sessionID}
	b.entries = append(b.entries, e)

	cutoff := b.now().Add(-budgetRetention)
	kept := b.entries[:0]
	for _, x := range b.entries {
		if !x.At.Before(cutoff) {
			kept = append(kept, x)
		}
	}
	dropped := len(kept) != len(b.entries)
	b.entries = kept
	if b.path == "" {
		return
	}
	if dropped {
		b.rewriteLocked()
		return
	}
	if err := os.MkdirAll(filepath.Dir(b.path), 0o700); err != nil {
		log.Printf("[budget] create dir for %s: %v", b.path, err)
		return
	}
	f, err := os.OpenFile(b.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		log.Printf("[budget] append %s: %v", b.path, err)
		return
	}
	defer func() { _ = f.Close() }()
	line, _ := json.Marshal(e)
	if _, err := f.Write(append(line, '\n')); err != nil {
		log.Printf("[budget] append %s: %v", b.path, err)
	}
}

// rewriteLocked replaces the file with the in-memory entries. Caller holds
// b.mu.
func (b *BudgetLedger) rewriteLocked() {
	if b.path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(b.path), 0o700); err != nil {
		log.Printf("[budget] create dir for %s: %v", b.path, err)
		return
	}
	tmp := b.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		log.Printf("[budget] rewrite %s: %v", b.path, err)
		return
	}
	w := bufio.NewWriter(f)
	for _, e := range b.entries {
		line, _ := json.Marshal(e)
		_, _ = w.Write(line)
		_ = w.WriteByte('\n')
	}
	err = w.Flush()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, b.path)
	}
	if err != nil {
		log.Printf("[budget] rewrite %s: %v", b.path, err)
		_ = os.Remove(tmp)
	}
}

// windowEntries returns this daemon's entries for llm inside the window that
// contains now, oldest first, and the window bounds.
func (b *BudgetLedger) windowEntries(llm string, bw *inference.BudgetWindow, now time.Time) (es []budgetEntry, start, end time.Time) {
	start, end = bw.WindowAt(now)
	weekly := bw.Mode() == inference.BudgetModeWeekly
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ensureLoaded()
	for _, e := range b.entries {
		if e.LLM != llm {
			continue
		}
		if weekly {
			if e.At.Before(start) || !e.At.Before(end) {
				continue
			}
		} else if !e.At.After(start) {
			continue
		}
		es = append(es, e)
	}
	sort.Slice(es, func(i, j int) bool { return es[i].At.Before(es[j].At) })
	return es, start, end
}

func sumUSD(es []budgetEntry) float64 {
	var t float64
	for _, e := range es {
		t += e.USD
	}
	return t
}

func (b *BudgetLedger) budgetFor(llm string) *inference.BudgetWindow {
	if b.Budgets == nil {
		return nil
	}
	return b.Budgets(llm)
}

func (b *BudgetLedger) peerSpend(llm string) float64 {
	if b.PeerSpend == nil {
		return 0
	}
	return b.PeerSpend(llm)
}

// SpendInWindow sums this daemon's own recorded spend for llm inside its
// current budget window. It is zero when the LLM has no budget.
func (b *BudgetLedger) SpendInWindow(llm string) float64 {
	bw := b.budgetFor(llm)
	if bw == nil {
		return 0
	}
	es, _, _ := b.windowEntries(llm, bw, b.now())
	return sumUSD(es)
}

// SpendBetween sums this daemon's own recorded spend for llm from from to to,
// both inclusive. It never includes peer spend and does not require the LLM
// to have a budget, so a peer can ask for the spend inside its own window.
func (b *BudgetLedger) SpendBetween(llm string, from, to time.Time) float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ensureLoaded()
	var total float64
	for _, e := range b.entries {
		if e.LLM == llm && !e.At.Before(from) && !e.At.After(to) {
			total += e.USD
		}
	}
	return total
}

// SpendBySessions sums this daemon's own recorded spend attributed to any of
// sessionIDs, from from onward (a zero from means every retained entry). A
// non-empty llm restricts the sum to that LLM. It never includes peer spend.
func (b *BudgetLedger) SpendBySessions(sessionIDs []string, llm string, from time.Time) float64 {
	if len(sessionIDs) == 0 {
		return 0
	}
	set := make(map[string]struct{}, len(sessionIDs))
	for _, id := range sessionIDs {
		if id != "" {
			set[id] = struct{}{}
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ensureLoaded()
	var total float64
	for _, e := range b.entries {
		if e.Session == "" {
			continue
		}
		if _, ok := set[e.Session]; !ok {
			continue
		}
		if llm != "" && e.LLM != llm {
			continue
		}
		if !from.IsZero() && e.At.Before(from) {
			continue
		}
		total += e.USD
	}
	return total
}

// Blocked reports whether llm has a budget and local-plus-peer spend is at or
// above the ceiling. The reason starts with "budget window full:".
//
// Admission cannot know the cost of the work it is about to admit, so work
// already admitted when the ceiling is crossed may overshoot it. The
// estimated allocation figure is informational and is never read here.
func (b *BudgetLedger) Blocked(llm string) (bool, string) {
	st, ok := b.Status(llm)
	if !ok || !st.Full {
		return false, ""
	}
	return true, BudgetWaitText(b.now, llm, st.SpendUSD, st.PeerSpendUSD, st.CeilingUSD, st.RollsOverAt)
}

// Status returns llm's budget state; false when the LLM has no budget.
func (b *BudgetLedger) Status(llm string) (BudgetStatus, bool) {
	bw := b.budgetFor(llm)
	if bw == nil {
		return BudgetStatus{}, false
	}
	now := b.now()
	es, start, end := b.windowEntries(llm, bw, now)
	local := sumUSD(es)
	peer := b.peerSpend(llm)
	total := local + peer
	full := total >= bw.CeilingUSD

	st := BudgetStatus{
		LLM:          llm,
		CeilingUSD:   bw.CeilingUSD,
		SpendUSD:     local,
		PeerSpendUSD: peer,
		RemainingUSD: max(0, bw.CeilingUSD-total),
		Window:       bw.Mode(),
		WindowHours:  bw.Hours,
		WindowStart:  start,
		WindowEnd:    end,
		Full:         full,
	}
	if bw.Mode() == inference.BudgetModeWeekly {
		next := end
		st.RollsOverAt = &next
	} else if at, ok := rollingRollover(es, total, bw, full); ok {
		st.RollsOverAt = &at
	}
	st.Allocation = b.allocation(llm, bw, local)
	if b.PeerBreakdown != nil {
		st.Peers = b.PeerBreakdown(llm)
	}
	return st, true
}

// rollingRollover finds when a rolling window's spend next drops: below the
// ceiling when full, or when the oldest counted entry ages out otherwise.
// Entries without cost are skipped; false means nothing to report.
func rollingRollover(es []budgetEntry, total float64, bw *inference.BudgetWindow, full bool) (time.Time, bool) {
	window := time.Duration(bw.Hours) * time.Hour
	remaining := total
	for _, e := range es {
		if e.USD <= 0 {
			continue
		}
		if !full {
			return e.At.Add(window), true
		}
		remaining -= e.USD
		if remaining < bw.CeilingUSD {
			return e.At.Add(window), true
		}
	}
	return time.Time{}, false
}

// allocation builds the estimated allocation figure, or nil unless the budget
// opted in and the LLM's plan tier has a declared notional allocation.
func (b *BudgetLedger) allocation(llm string, bw *inference.BudgetWindow, spend float64) *BudgetAllocation {
	if !bw.AllocationPct {
		return nil
	}
	return b.AllocationFor(llm, spend)
}

// AllocationFor builds the estimated allocation figure for spend on llm, or
// nil when the LLM's plan tier is unset or has no declared notional
// allocation. The caller decides whether the figure was opted in to; it is
// always an estimate and is never used to block.
func (b *BudgetLedger) AllocationFor(llm string, spend float64) *BudgetAllocation {
	if b == nil || b.PlanTier == nil || b.PlanTierAllocations == nil {
		return nil
	}
	tier := string(b.PlanTier(llm))
	if tier == "" {
		return nil
	}
	usd := b.PlanTierAllocations()[tier]
	if usd <= 0 {
		return nil
	}
	return &BudgetAllocation{
		AllocationPct: spend / usd * 100,
		Estimated:     true,
		AllocationUSD: usd,
		PlanTier:      tier,
		Basis:         allocationBasis,
	}
}

// StatusAll returns the status of every LLM with a budget, sorted by name.
func (b *BudgetLedger) StatusAll() []BudgetStatus {
	if b.LLMs == nil {
		return nil
	}
	names := append([]string(nil), b.LLMs()...)
	sort.Strings(names)
	out := make([]BudgetStatus, 0, len(names))
	for _, n := range names {
		if st, ok := b.Status(n); ok {
			out = append(out, st)
		}
	}
	return out
}
