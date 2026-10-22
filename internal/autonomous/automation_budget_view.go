// The read-only budget view of an Automation: the stored record plus what the
// budget evaluator says about it now. It is computed on every read and never
// persisted.

package autonomous

import (
	"fmt"
	"strings"
	"time"
)

// BudgetView is the computed budget state of one Automation, built from the
// policy that is tightest right now (see AutomationBudget.Evaluate). Dollar
// figures are exact spend; the allocation estimate is an estimate and is never
// used to pause or block.
type BudgetView struct {
	// Scope is "automation" or "member:<id>": the policy this view describes.
	Scope        string  `json:"scope"`
	CeilingType  string  `json:"ceiling_type,omitempty"`
	CeilingUSD   float64 `json:"ceiling_usd"`
	SpentUSD     float64 `json:"spent_usd"`
	RemainingUSD float64 `json:"remaining_usd"`

	Window      string     `json:"window,omitempty"`
	WindowStart *time.Time `json:"window_start,omitempty"`
	WindowEnd   *time.Time `json:"window_end,omitempty"` // absent for the lifetime window

	Pacing           string  `json:"pacing,omitempty"`
	PacingAllowedUSD float64 `json:"pacing_allowed_usd,omitempty"`
	OnBreach         string  `json:"on_breach,omitempty"`
	// CeilingReached is true when the dollar ceiling stops starts. Hold is
	// "pacing" or "estimate" while a member is held back without a breach.
	CeilingReached bool   `json:"ceiling_reached"`
	Hold           string `json:"hold,omitempty"`
	HoldReason     string `json:"hold_reason,omitempty"`

	// ResetAt is when the window frees up again; ResetKnown is true when the
	// daemon can act on it by itself and ResetSource says where it came from
	// (window or rate_limit).
	ResetAt     *time.Time `json:"reset_at,omitempty"`
	ResetKnown  bool       `json:"reset_known"`
	ResetSource string     `json:"reset_source,omitempty"`

	// State is the stored breach episode, if any.
	State *BudgetState `json:"state,omitempty"`

	// Allocation is the optional estimated share of the plan allocation.
	Allocation *BudgetAllocation `json:"allocation_estimate,omitempty"`

	// Peers lists the other daemons' usage counted toward the ceiling;
	// PeerBasis says on what basis, PeerNote why named peers were not counted.
	Peers        []PeerBudgetStatus `json:"peers,omitempty"`
	PeerSpendUSD float64            `json:"peer_spend_usd,omitempty"`
	PeerBasis    string             `json:"peer_basis,omitempty"`
	PeerNote     string             `json:"peer_note,omitempty"`

	// Note carries a plain remark about the view, set when the policy is an
	// estimate only.
	Note string `json:"note,omitempty"`
}

// AutomationView is an Automation as the read surfaces return it: the stored
// record with the computed budget view next to it. The view is never saved.
type AutomationView struct {
	*Automation
	Budget *BudgetView `json:"budget,omitempty"`
}

// allocationOnlyNote explains a policy that can never pause or block.
const allocationOnlyNote = "this policy is an estimate only: an allocation_pct ceiling never pauses or blocks the Automation"

// AutomationViewOf returns a with its computed budget view.
func (m *Manager) AutomationViewOf(a *Automation) *AutomationView {
	return &AutomationView{Automation: a, Budget: m.BudgetViewFor(a)}
}

// BudgetViewFor computes the budget view of a, or nil when a has no budget
// policy in play and no recorded breach. The evaluated policy is the
// Automation's own together with the override of the first live member that has
// one; the tightest decides. It takes the Manager lock, so callers must not
// hold it.
func (m *Manager) BudgetViewFor(a *Automation) *BudgetView {
	if a == nil {
		return nil
	}
	var member *AutomationMember
	for _, id := range liveCursorIDs(a) {
		if mem := automationMember(a, id); mem != nil && mem.BudgetPolicy != nil && !memberFinished(mem) {
			member = mem
			break
		}
	}
	v := m.AutomationBudget().Evaluate(a, member, false)
	if v.Policy == nil {
		if a.BudgetState == nil {
			return nil
		}
		return &BudgetView{Scope: a.BudgetState.Scope, State: cloneBudgetState(a.BudgetState)}
	}
	return newBudgetView(a, v)
}

// newBudgetView turns an evaluator verdict into the operator-facing view.
func newBudgetView(a *Automation, v BudgetVerdict) *BudgetView {
	p := v.Policy
	bv := &BudgetView{
		Scope:            v.Scope,
		CeilingType:      p.CeilingType,
		CeilingUSD:       v.CeilingUSD,
		SpentUSD:         v.SpentUSD,
		RemainingUSD:     v.RemainingUSD,
		Window:           p.Window,
		Pacing:           p.EffectivePacing(),
		PacingAllowedUSD: v.PacingAllowedUSD,
		OnBreach:         p.EffectiveOnBreach(),
		CeilingReached:   v.Gate && v.Kind == BudgetKindCeiling,
		ResetAt:          v.ResetAt,
		ResetKnown:       v.ResetKnown,
		ResetSource:      v.ResetSource,
		State:            cloneBudgetState(a.BudgetState),
		Allocation:       v.Allocation,
		Peers:            v.Peers,
		PeerSpendUSD:     v.PeerSpendUSD,
		PeerNote:         v.PeerNote,
	}
	if !v.WindowStart.IsZero() {
		start := v.WindowStart
		bv.WindowStart = &start
	}
	if !v.WindowEnd.IsZero() {
		end := v.WindowEnd
		bv.WindowEnd = &end
		if bv.ResetAt == nil && !v.Gate {
			// Not stopped: the window itself says when spend starts over.
			bv.ResetAt, bv.ResetKnown, bv.ResetSource = &end, true, BudgetResetWindow
		}
	}
	if v.Gate && v.Kind != BudgetKindCeiling {
		bv.Hold, bv.HoldReason = v.Kind, v.Reason
	}
	if len(p.PeerWindow) > 0 {
		bv.PeerBasis = PeerOptInNote
	}
	if !p.Gates() {
		bv.Note = allocationOnlyNote
	}
	return bv
}

func cloneBudgetState(s *BudgetState) *BudgetState {
	if s == nil {
		return nil
	}
	c := *s
	if s.ResumeAt != nil {
		at := *s.ResumeAt
		c.ResumeAt = &at
	}
	return &c
}

// ceilingDisplay renders the dollar position for the status line, for example
// "$12.40 of $40.00 (week)". It is empty for a policy with no dollar ceiling.
func (bv *BudgetView) ceilingDisplay() string {
	if bv == nil || bv.CeilingUSD <= 0 {
		return ""
	}
	window := bv.Window
	if window == "automation" {
		window = "lifetime"
	}
	if member, ok := strings.CutPrefix(bv.Scope, "member:"); ok {
		window += ", member " + member
	}
	return fmt.Sprintf("$%.2f of $%.2f (%s)", bv.SpentUSD, bv.CeilingUSD, window)
}
