// Return a blocked Automation to its draft state, the operator's way of
// undoing a block that happened before any member started. An Automation
// whose members have already run has real progress to preserve and cannot be
// un-run, so only a blocked Automation with no started member and no cursor is
// returned; everything that would let the operator tell the two apart is left
// in place. Written through the same compare-and-set the other control verbs
// use (automation_control.go).

package autonomous

import (
	"fmt"
)

// returnToDraftKind is the Decision kind for the return; the audit action is
// controlReturnToDraft (both carry the same meaning, like the other control
// verbs).
const returnToDraftKind = "return_to_draft"

// controlReturnToDraft is the audit action for returning a blocked
// Automation to draft.
const controlReturnToDraft = "automation_return_to_draft"

// checkReturnable reports why a cannot be returned to draft, or nil when it
// is safe to: it is blocked before anything in it started.
func checkReturnable(a *Automation) error {
	for _, mem := range a.Members {
		if mem.StartedAt != nil {
			return fmt.Errorf("409: automation %s has a member that already started; only a blocked automation that never started a member can be returned to draft", a.ID)
		}
	}
	if a.CurrentMemberID != "" {
		return fmt.Errorf("409: automation %s has a cursor on member %s, so it was started; only a blocked automation that never started a member can be returned to draft", a.ID, a.CurrentMemberID)
	}
	if len(a.ActiveCursors) != 0 {
		return fmt.Errorf("409: automation %s has active cursors %v, so it was started; only a blocked automation that never started a member can be returned to draft", a.ID, a.ActiveCursors)
	}
	return nil
}

// ReturnAutomationToDraft returns a blocked Automation to the draft state the
// operator can still edit: the status goes back to draft, the block's runtime
// state is cleared (IdleSince, the cursors, StartedAt and the budget episode)
// and every member is back to pending with no timestamps, so the Automation
// can be re-activated as if it had never run. It is refused with a 409 when
// the Automation is not blocked (naming its status) or when any member has
// started or any cursor was ever started. expectedVersion is the
// compare-and-set version; zero skips the check, like the other control verbs.
func (m *Manager) ReturnAutomationToDraft(id, actor string, expectedVersion int64) (*Automation, error) {
	if actor == "" {
		actor = "operator"
	}
	// Precheck without a version requirement so the operator always hears the
	// real reason (status or started member) before any CAS applies.
	a, ok := m.automations.Get(id)
	if !ok {
		return nil, fmt.Errorf("404: automation %s not found", id)
	}
	if err := checkControlStatus(a, []AutomationStatus{AutomationBlocked}); err != nil {
		return nil, err
	}
	if err := checkReturnable(a); err != nil {
		return nil, err
	}
	updated, err := m.automations.Update(id, expectedVersion, func(a *Automation) error {
		if a.Status != AutomationBlocked {
			return fmt.Errorf("409: automation %s is %s; only a blocked automation can be returned to draft", a.ID, a.Status)
		}
		if err := checkReturnable(a); err != nil {
			return err
		}
		a.Status = AutomationDraft
		a.IdleSince = nil
		a.ActiveCursors = nil
		a.CurrentMemberID = ""
		a.StartedAt = nil
		a.BudgetState = nil
		for i := range a.Members {
			a.Members[i].Status = MemberPending
			a.Members[i].StartedAt = nil
			a.Members[i].FinishedAt = nil
		}
		controlDecision(a, returnToDraftKind, actor, "returned to draft by "+actor+"; no member had started")
		return nil
	})
	if err != nil {
		return nil, err
	}
	m.controlAudit(controlReturnToDraft, map[string]any{
		"automation_id": id, "actor": actor, "version": updated.Version,
	})
	return updated, nil
}
