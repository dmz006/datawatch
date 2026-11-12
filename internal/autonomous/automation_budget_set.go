// Setting an Automation's budget policy (or one member's override) as an
// operator control operation. Like the other control operations it takes the
// expected Version, writes through the store's compare-and-set, and records one
// Decision and one audit record. It never changes the Automation's status: a
// paused Automation resumes through the coordinator tick and a blocked one
// through the operator's resume.

package autonomous

import (
	"errors"
	"fmt"
	"strings"
)

// controlSetBudget is the audit action of the set-budget operation.
const controlSetBudget = "automation_set_budget"

// decisionBudgetPolicy is the Decision kind recorded when a policy is set or
// cleared.
const decisionBudgetPolicy = "budget_policy"

// BudgetSetRequest is the argument set of the set-budget operation. The JSON
// names are the contract of every surface. MemberID empty targets the
// Automation's own policy; set, that member's override. The policy fields
// replace the whole policy; Clear removes it instead and may not be combined
// with them. EstimatedCostUSD (members only) declares the expected cost of the
// member's Automaton: alone it changes only the estimate, and zero removes it.
type BudgetSetRequest struct {
	MemberID string `json:"member_id,omitempty"`

	CeilingType        string   `json:"ceiling_type,omitempty"`
	CeilingValue       float64  `json:"ceiling_value,omitempty"`
	Window             string   `json:"window,omitempty"`
	Pacing             string   `json:"pacing,omitempty"`
	OnBreach           string   `json:"on_breach,omitempty"`
	Backend            string   `json:"backend,omitempty"`
	AllocationEstimate bool     `json:"allocation_estimate,omitempty"`
	PeerWindow         []string `json:"peer_window,omitempty"`
	ResetTime          string   `json:"reset_time,omitempty"`
	ResetWeekday       string   `json:"reset_weekday,omitempty"`
	ResetDay           int      `json:"reset_day,omitempty"`
	ResetZone          string   `json:"reset_zone,omitempty"`

	EstimatedCostUSD *float64 `json:"estimated_cost_usd,omitempty"`
	Clear            bool     `json:"clear,omitempty"`

	// Reason is the operator's note for the audit record; optional.
	Reason string `json:"reason,omitempty"`
}

// hasPolicy reports whether any policy field is set.
func (r BudgetSetRequest) hasPolicy() bool {
	return r.CeilingType != "" || r.CeilingValue != 0 || r.Window != "" || r.Pacing != "" ||
		r.OnBreach != "" || r.Backend != "" || r.AllocationEstimate || len(r.PeerWindow) > 0 ||
		r.ResetTime != "" || r.ResetWeekday != "" || r.ResetDay != 0 || r.ResetZone != ""
}

// policy builds the policy the request describes. PeerWindow is copied so
// validation's trimming never touches the caller's slice.
func (r BudgetSetRequest) policy() *BudgetPolicy {
	return &BudgetPolicy{
		CeilingType:        r.CeilingType,
		CeilingValue:       r.CeilingValue,
		Window:             r.Window,
		Pacing:             r.Pacing,
		OnBreach:           r.OnBreach,
		Backend:            r.Backend,
		AllocationEstimate: r.AllocationEstimate,
		PeerWindow:         append([]string(nil), r.PeerWindow...),
		ResetTime:          r.ResetTime,
		ResetWeekday:       r.ResetWeekday,
		ResetDay:           r.ResetDay,
		ResetZone:          r.ResetZone,
	}
}

// validate checks the shape of the request, before the Automation is read.
func (r BudgetSetRequest) validate() error {
	if r.Clear {
		if r.hasPolicy() || r.EstimatedCostUSD != nil {
			return errors.New("budget invalid: clear removes the policy and cannot be combined with other budget fields")
		}
		return nil
	}
	if r.EstimatedCostUSD != nil {
		if r.MemberID == "" {
			return errors.New("budget invalid: estimated_cost_usd applies to a member; name it with member_id")
		}
		if *r.EstimatedCostUSD < 0 {
			return fmt.Errorf("budget invalid: estimated_cost_usd %v must not be negative", *r.EstimatedCostUSD)
		}
	}
	if !r.hasPolicy() {
		if r.EstimatedCostUSD == nil {
			return errors.New("budget invalid: nothing to set; give a ceiling, or clear to remove the policy")
		}
		return nil
	}
	return validateBudgetPolicy(r.policy())
}

// memberFinished reports whether mem has already ended, so a budget set on it
// could never take effect.
func memberFinished(mem *AutomationMember) bool {
	switch mem.Status {
	case MemberCompleted, MemberBlocked, MemberFailed, MemberSkipped, MemberCancelled:
		return true
	}
	return false
}

// checkBudgetTarget returns a 404 or 409 error unless the policy may be set on
// the member req names (nothing to check for the Automation's own policy).
func checkBudgetTarget(a *Automation, req BudgetSetRequest) error {
	if req.MemberID == "" {
		return nil
	}
	mem := automationMember(a, req.MemberID)
	if mem == nil {
		return fmt.Errorf("404: member %s not found in automation %s", req.MemberID, a.ID)
	}
	if memberFinished(mem) {
		return fmt.Errorf("409: member %s is %s; a budget can only be set on a member that has not finished", req.MemberID, mem.Status)
	}
	if k := mem.kind(); k == MemberKindFork || k == MemberKindJoin {
		return fmt.Errorf("budget invalid: member %s is a %s and has no budget of its own; set it on a member or on the Automation", req.MemberID, k)
	}
	return nil
}

// SetAutomationBudgetPolicy sets, replaces or clears the budget policy of
// Automation id (empty member_id) or one member's override. The Automation must
// not be completed or cancelled, and a member must not have finished. A stale
// expectedVersion is a 409 and an unknown Automation or member a 404; an invalid
// policy is a plain error (400). The status is never changed here.
func (m *Manager) SetAutomationBudgetPolicy(id string, req BudgetSetRequest, actor string, expectedVersion int64) (*Automation, error) {
	if actor == "" {
		actor = "operator"
	}
	if err := req.validate(); err != nil {
		return nil, err
	}
	open := []AutomationStatus{AutomationDraft, AutomationActive, AutomationPaused, AutomationBlocked}
	a, err := m.loadForControl(id, expectedVersion, open...)
	if err != nil {
		return nil, err
	}
	if err := checkBudgetTarget(a, req); err != nil {
		return nil, err
	}

	updated, err := m.automations.Update(id, expectedVersion, func(cur *Automation) error {
		if err := checkControlStatus(cur, open); err != nil {
			return err
		}
		if err := checkBudgetTarget(cur, req); err != nil {
			return err
		}
		slot := &cur.BudgetPolicy
		if req.MemberID != "" {
			mem := automationMember(cur, req.MemberID)
			slot = &mem.BudgetPolicy
			if req.EstimatedCostUSD != nil {
				mem.EstimatedCostUSD = *req.EstimatedCostUSD
			}
		}
		switch {
		case req.Clear:
			*slot = nil
		case req.hasPolicy():
			*slot = req.policy()
		}
		controlDecision(cur, decisionBudgetPolicy, actor, budgetSetNote(req, actor))
		return nil
	})
	if err != nil {
		return nil, err
	}
	m.controlAudit(controlSetBudget, budgetSetAuditDetails(updated, req, actor))
	return updated, nil
}

// budgetSetNote is the Decision text of a set-budget operation.
func budgetSetNote(req BudgetSetRequest, actor string) string {
	target := "automation budget policy"
	if req.MemberID != "" {
		target = "budget policy of member " + req.MemberID
	}
	switch {
	case req.Clear:
		return fmt.Sprintf("%s cleared by %s", target, actor)
	case req.hasPolicy():
		p := req.policy()
		return fmt.Sprintf("%s set by %s: %s ceiling %.2f per %s, pacing %s, on breach %s",
			target, actor, p.CeilingType, p.CeilingValue, windowLabel(p), p.EffectivePacing(), p.EffectiveOnBreach())
	default:
		return fmt.Sprintf("expected cost of member %s set to $%.2f by %s", req.MemberID, *req.EstimatedCostUSD, actor)
	}
}

// budgetSetAuditDetails is the payload of the automation_set_budget audit
// record: who, why, what was set and the version it produced.
func budgetSetAuditDetails(a *Automation, req BudgetSetRequest, actor string) map[string]any {
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = budgetSetNote(req, actor)
	}
	d := map[string]any{
		"automation_id": a.ID, "actor": actor, "reason": reason,
		"member_id": req.MemberID, "clear": req.Clear, "version": a.Version,
	}
	if req.hasPolicy() {
		d["ceiling_type"], d["ceiling_value"], d["window"] = req.CeilingType, req.CeilingValue, req.Window
		d["pacing"], d["on_breach"], d["backend"] = req.Pacing, req.OnBreach, req.Backend
		d["allocation_estimate"], d["peer_window"] = req.AllocationEstimate, req.PeerWindow
	}
	if req.EstimatedCostUSD != nil {
		d["estimated_cost_usd"] = *req.EstimatedCostUSD
	}
	return d
}
