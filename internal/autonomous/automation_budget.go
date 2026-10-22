package autonomous

import (
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/inference"
)

// Budget verdict kinds (BudgetVerdict.Kind). An empty kind means the verdict
// does not gate.
const (
	BudgetKindCeiling  = "ceiling"
	BudgetKindPacing   = "pacing"
	BudgetKindEstimate = "estimate"
)

// Sources of the cost estimate for a member about to start
// (BudgetVerdict.EstimateSource).
const (
	BudgetEstimateDeclared   = "declared"
	BudgetEstimatePriorSpend = "prior_spend"
)

// BudgetWindowSpan is the window a budget policy is evaluated over. HasEnd is
// false for the lifetime window, which never ends.
type BudgetWindowSpan struct {
	Window string
	Start  time.Time
	End    time.Time
	HasEnd bool
}

// ResetInfo is what is known about when a breached window frees up again.
// Known is true only when At is a time the daemon can act on by itself.
type ResetInfo struct {
	At     *time.Time
	Known  bool
	Source string // BudgetResetWindow | BudgetResetRateLimit | ""
}

// BudgetVerdict is the outcome of evaluating the budget policies that apply
// to an Automation, optionally for one member about to start.
type BudgetVerdict struct {
	// Gate is true when the Automation or member must not start or continue.
	Gate bool `json:"gate"`
	// Kind is why it gates: ceiling, pacing or estimate; empty when it does not.
	Kind   string `json:"kind,omitempty"`
	Reason string `json:"reason,omitempty"`
	// Scope is "automation" or "member:<id>": the policy that decided, the
	// gating one when any gates, otherwise the one with the least remaining.
	Scope  string        `json:"scope,omitempty"`
	Policy *BudgetPolicy `json:"policy,omitempty"`

	SpentUSD     float64   `json:"spent_usd"`
	CeilingUSD   float64   `json:"ceiling_usd"`
	RemainingUSD float64   `json:"remaining_usd"`
	WindowStart  time.Time `json:"window_start"`
	WindowEnd    time.Time `json:"window_end"` // zero for the lifetime window

	// PacingAllowedUSD is the even-pace spend allowed so far in the window.
	PacingAllowedUSD float64 `json:"pacing_allowed_usd,omitempty"`

	EstimateUSD    float64 `json:"estimate_usd,omitempty"`
	EstimateSource string  `json:"estimate_source,omitempty"`

	// Allocation is the optional estimated share-of-allocation figure. It is
	// always an estimate and never affects Gate.
	Allocation *BudgetAllocation `json:"allocation,omitempty"`

	ResetAt     *time.Time `json:"reset_at,omitempty"`
	ResetKnown  bool       `json:"reset_known"`
	ResetSource string     `json:"reset_source,omitempty"`

	// Peers and PeerSpendUSD describe the opted-in peer spend counted toward
	// the ceiling; empty when no peer is opted in.
	Peers        []PeerBudgetStatus `json:"peers,omitempty"`
	PeerSpendUSD float64            `json:"peer_spend_usd,omitempty"`
}

// AutomationBudget is the pure policy evaluator behind the advance gate and
// the admit hook. It never mutates an Automation. Configure the fields
// before first use.
type AutomationBudget struct {
	// Clock supplies the current time; nil means time.Now.
	Clock func() time.Time
	// Spend returns the dollars spent since since, limited to llm when
	// non-empty: by the whole Automation when member is nil, otherwise by that
	// member. Nil means the shared cost rollup on the manager.
	Spend func(a *Automation, member *AutomationMember, since time.Time, llm string) float64
	// Allocation builds the estimated allocation figure for spend on llm from
	// the plan-tier inputs. Nil means the ledger's AllocationFor.
	Allocation func(llm string, spend float64) *BudgetAllocation
	// Resets reports what is known about when a breached window frees up.
	// Nil means no reset knowledge.
	Resets func(p *BudgetPolicy, win BudgetWindowSpan, now time.Time) ResetInfo
	// PeerSpend returns the spend of the peers the policy opted in, and one
	// line per peer. Nil means no peer spend.
	PeerSpend func(p *BudgetPolicy, from, to time.Time) (usd float64, lines []PeerBudgetStatus)

	mgr    *Manager
	ledger *BudgetLedger
}

// NewAutomationBudget returns an evaluator over m's cost rollup and ledger's
// plan-tier allocations. Either may be nil.
func NewAutomationBudget(m *Manager, ledger *BudgetLedger) *AutomationBudget {
	return &AutomationBudget{mgr: m, ledger: ledger}
}

// SetAutomationBudget wires the budget evaluator; nil clears it.
func (m *Manager) SetAutomationBudget(b *AutomationBudget) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.autoBudget = b
}

// AutomationBudget returns the wired budget evaluator, or nil.
func (m *Manager) AutomationBudget() *AutomationBudget {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.autoBudget
}

func (b *AutomationBudget) now() time.Time {
	if b.Clock == nil {
		return time.Now()
	}
	return b.Clock()
}

func (b *AutomationBudget) spend(a *Automation, member *AutomationMember, since time.Time, llm string) float64 {
	if b.Spend != nil {
		return b.Spend(a, member, since, llm)
	}
	if b.mgr == nil {
		return 0
	}
	if member == nil {
		return b.mgr.AutomationSpendUSD(a, since, llm)
	}
	// A one-member scratch Automation reuses the shared rollup for either
	// member kind instead of joining tasks to cost again.
	return b.mgr.AutomationSpendUSD(&Automation{Members: []AutomationMember{*member}}, since, llm)
}

func (b *AutomationBudget) allocation(llm string, spend float64) *BudgetAllocation {
	if b.Allocation != nil {
		return b.Allocation(llm, spend)
	}
	return b.ledger.AllocationFor(llm, spend)
}

// Evaluate checks the budget policies that apply to a: the Automation's own
// policy (all members' spend) and member's own policy (that member's spend).
// Each is evaluated independently and the tightest wins: any policy that
// gates makes the verdict gate, and the reported scope is the gating one with
// the least remaining. These policies only narrow the global per-LLM
// ceilings, which the capacity gate enforces separately.
//
// member is the member about to start or continue, or nil to check the
// Automation alone. hard is the admit hook's mode: it evaluates only the
// dollar ceiling, never pacing, a start estimate or the allocation figure.
// With no applicable policy the zero verdict is returned without a spend
// lookup.
func (b *AutomationBudget) Evaluate(a *Automation, member *AutomationMember, hard bool) BudgetVerdict {
	if b == nil || a == nil {
		return BudgetVerdict{}
	}
	var memberPolicy *BudgetPolicy
	if member != nil {
		memberPolicy = member.BudgetPolicy
	}
	if !policyApplies(memberPolicy, hard) && !policyApplies(a.BudgetPolicy, hard) {
		return BudgetVerdict{}
	}
	now := b.now()

	var verdicts []BudgetVerdict
	if policyApplies(memberPolicy, hard) {
		verdicts = append(verdicts, b.evalPolicy(a, member, member, memberPolicy, "member:"+member.ID, hard, now))
	}
	if policyApplies(a.BudgetPolicy, hard) {
		verdicts = append(verdicts, b.evalPolicy(a, nil, member, a.BudgetPolicy, "automation", hard, now))
	}

	// The tightest wins: among gating verdicts when any gates, otherwise
	// among all of them; the member policy is listed first and wins a tie.
	pick := -1
	anyGate := false
	for _, v := range verdicts {
		anyGate = anyGate || v.Gate
	}
	for i, v := range verdicts {
		if anyGate && !v.Gate {
			continue
		}
		if pick < 0 || tighter(v, verdicts[pick]) {
			pick = i
		}
	}
	out := verdicts[pick]
	if out.Allocation == nil {
		for _, v := range verdicts {
			if v.Allocation != nil {
				out.Allocation = v.Allocation
				break
			}
		}
	}
	return out
}

// tighter reports whether v leaves less room than w. An allocation-only
// verdict has no ceiling and so never beats a dollar one.
func tighter(v, w BudgetVerdict) bool {
	if v.CeilingUSD <= 0 {
		return false
	}
	if w.CeilingUSD <= 0 {
		return true
	}
	return v.CeilingUSD-v.SpentUSD < w.CeilingUSD-w.SpentUSD
}

// policyApplies reports whether p takes part in an evaluation. An
// allocation_pct policy never gates and in hard mode has nothing to report.
func policyApplies(p *BudgetPolicy, hard bool) bool {
	if p == nil || p.CeilingValue <= 0 {
		return false
	}
	return p.Gates() || !hard
}

// evalPolicy evaluates one policy. scoped is the member whose spend the policy
// counts, nil for the Automation policy; starting is the member about to
// start, if any, whose cost estimate is checked against the ceiling.
func (b *AutomationBudget) evalPolicy(a *Automation, scoped, starting *AutomationMember, p *BudgetPolicy, scope string, hard bool, now time.Time) BudgetVerdict {
	start, end, hasEnd := policyWindow(p, a, now)
	span := BudgetWindowSpan{Window: p.Window, Start: start, End: end, HasEnd: hasEnd}

	spent := b.spend(a, scoped, start, p.Backend)
	var peerUSD float64
	var peers []PeerBudgetStatus
	if b.PeerSpend != nil && len(p.PeerWindow) > 0 {
		peerUSD, peers = b.PeerSpend(p, start, now)
		spent += peerUSD
	}

	v := BudgetVerdict{
		Scope:        scope,
		Policy:       p,
		SpentUSD:     spent,
		WindowStart:  start,
		Peers:        peers,
		PeerSpendUSD: peerUSD,
	}
	if hasEnd {
		v.WindowEnd = end
	}

	// An allocation_pct policy only fills the estimated figure.
	if !p.Gates() {
		v.Allocation = b.allocation(p.Backend, spent)
		return v
	}
	ceiling := p.CeilingValue
	v.CeilingUSD = ceiling
	v.RemainingUSD = max(0, ceiling-spent)
	if !hard && p.AllocationEstimate {
		v.Allocation = b.allocation(p.Backend, spent)
	}

	label := scopeLabel(scope)
	switch {
	case spent >= ceiling:
		v.Gate, v.Kind = true, BudgetKindCeiling
		v.Reason = fmt.Sprintf("%s budget reached: spent $%.2f of the $%.2f %s ceiling", label, spent, ceiling, windowLabel(p))
	case hard:
		return v
	default:
		// Pacing needs an elapsed fraction, which the lifetime window does
		// not have, so it is ignored there.
		if hasEnd && p.EffectivePacing() == "even" {
			if total := end.Sub(start); total > 0 {
				frac := min(1, max(0, float64(now.Sub(start))/float64(total)))
				v.PacingAllowedUSD = ceiling * frac
				if spent > v.PacingAllowedUSD {
					v.Gate, v.Kind = true, BudgetKindPacing
					v.Reason = fmt.Sprintf("%s budget is ahead of an even pace: spent $%.2f, $%.2f allowed so far of the $%.2f %s ceiling",
						label, spent, v.PacingAllowedUSD, ceiling, windowLabel(p))
				}
			}
		}
		if !v.Gate && starting != nil {
			b.applyEstimate(a, starting, p, &v)
		}
	}
	if v.Gate {
		b.fillReset(p, span, now, &v)
	}
	return v
}

// applyEstimate gates v when the member about to start is expected to take
// spend past the ceiling. Without an estimate the member starts below the
// ceiling. For the lifetime window waiting can never help, so the crossing is
// reported as the ceiling itself.
func (b *AutomationBudget) applyEstimate(a *Automation, member *AutomationMember, p *BudgetPolicy, v *BudgetVerdict) {
	est, src := member.EstimatedCostUSD, BudgetEstimateDeclared
	if est <= 0 {
		// A PRD being resumed has spent before: that is the best estimate.
		est, src = b.spend(a, member, time.Time{}, p.Backend), BudgetEstimatePriorSpend
	}
	if est <= 0 {
		return
	}
	v.EstimateUSD, v.EstimateSource = est, src
	if v.SpentUSD+est <= v.CeilingUSD {
		return
	}
	v.Gate, v.Kind = true, BudgetKindEstimate
	if p.Window == "automation" {
		v.Kind = BudgetKindCeiling
	}
	what := "declared estimate"
	if src == BudgetEstimatePriorSpend {
		what = "spend so far on this Automaton"
	}
	v.Reason = fmt.Sprintf("%s budget: starting this member is expected to cost $%.2f (%s), taking spend from $%.2f to $%.2f, past the $%.2f %s ceiling",
		scopeLabel(v.Scope), est, what, v.SpentUSD, v.SpentUSD+est, v.CeilingUSD, windowLabel(p))
}

// fillReset records what the Resets seam knows about the breach. A nil seam
// means no reset knowledge.
func (b *AutomationBudget) fillReset(p *BudgetPolicy, span BudgetWindowSpan, now time.Time, v *BudgetVerdict) {
	if b.Resets == nil {
		return
	}
	info := b.Resets(p, span, now)
	v.ResetAt, v.ResetKnown, v.ResetSource = info.At, info.Known, info.Source
}

func scopeLabel(scope string) string {
	if scope == "automation" {
		return "Automation"
	}
	return "member"
}

func windowLabel(p *BudgetPolicy) string {
	if p.Window == "automation" {
		return "lifetime"
	}
	return p.Window
}

// policyWindow returns the window of p that contains now. The lifetime window
// (automation) starts when the Automation started and has no end. The day,
// week and month windows start at the most recent anchor at or before now and
// end at the next one, read in ResetZone or the local zone; unset anchors
// default to midnight, Monday and the first of the month (see BudgetPolicy).
// The bool is false when the window has no end.
func policyWindow(p *BudgetPolicy, a *Automation, now time.Time) (start, end time.Time, hasEnd bool) {
	if p.Window == "automation" {
		if a != nil && a.StartedAt != nil {
			start = *a.StartedAt
		}
		return start, time.Time{}, false
	}
	loc := time.Local
	if p.ResetZone != "" {
		if l, err := time.LoadLocation(p.ResetZone); err == nil {
			loc = l
		}
	}
	hour, minute := 0, 0
	if p.ResetTime != "" {
		if h, m, err := inference.ParseBudgetClock(p.ResetTime); err == nil {
			hour, minute = h, m
		}
	}
	local := now.In(loc)
	y, mo, d := local.Date()
	switch p.Window {
	case "day":
		start = time.Date(y, mo, d, hour, minute, 0, 0, loc)
		if start.After(local) {
			start = time.Date(y, mo, d-1, hour, minute, 0, 0, loc)
		}
		sy, sm, sd := start.Date()
		end = time.Date(sy, sm, sd+1, hour, minute, 0, 0, loc)
	case "week":
		target := time.Monday
		if wd, ok := inference.ParseBudgetWeekday(strings.TrimSpace(p.ResetWeekday)); ok {
			target = wd
		}
		back := (int(local.Weekday()) - int(target) + 7) % 7
		start = time.Date(y, mo, d-back, hour, minute, 0, 0, loc)
		if start.After(local) {
			start = time.Date(y, mo, d-back-7, hour, minute, 0, 0, loc)
		}
		sy, sm, sd := start.Date()
		end = time.Date(sy, sm, sd+7, hour, minute, 0, 0, loc)
	default: // month
		day := p.ResetDay
		if day < 1 {
			day = 1
		}
		start = time.Date(y, mo, day, hour, minute, 0, 0, loc)
		if start.After(local) {
			start = time.Date(y, mo-1, day, hour, minute, 0, 0, loc)
		}
		sy, sm, _ := start.Date()
		end = time.Date(sy, sm+1, day, hour, minute, 0, 0, loc)
	}
	return start, end, true
}
