// Operator controls for the two Automation run-shape settings: max_parallel
// (how many members run at once) and stage_gate (whether a member's stage holds
// it back). Each is a narrow control, like set_budget and set_decision_assistant:
// it changes only its own field, so a bare toggle never rewrites the
// description, budget or defaults the way a whole-record update does.
//
// Both are allowed while the Automation is draft, active, paused or blocked.
// On an active Automation the change takes effect at once: the coordinator
// re-checks the held members through the same admission path boot reconcile
// uses, so raising or clearing a limit, or turning the gate off, starts the
// members that were waiting, in member order, and lowering a limit or turning
// the gate on holds the members that have not started yet. A member already
// running is never stopped.

package autonomous

import (
	"fmt"
)

// Decision kinds and audit actions of the two controls.
const (
	controlSetMaxParallel = "automation_set_max_parallel"
	controlSetStageGate   = "automation_set_stage_gate"
)

// parallelSettingStatuses are the statuses the two controls accept.
var parallelSettingStatuses = []AutomationStatus{AutomationDraft, AutomationActive, AutomationPaused, AutomationBlocked}

// SetMaxParallel sets the most members that run at once. memberID "" sets the
// Automation's own limit; a fork's ID sets that fork's limit on its branches
// (the smaller of the two applies). limit 0 clears the limit; a negative limit
// is refused, and so is a member that is not a fork. It records one Decision
// and one audit record. expectedVersion 0 skips the version check.
func (m *Manager) SetMaxParallel(id, memberID string, limit int, actor string, expectedVersion int64) (*Automation, error) {
	if actor == "" {
		actor = operatorActor
	}
	if limit < 0 {
		return nil, fmt.Errorf("max_parallel %d must not be negative (0 means no limit)", limit)
	}
	scope := "automation"
	if memberID != "" {
		scope = "fork " + memberID
	}
	note := fmt.Sprintf("%s max_parallel set to %d", scope, limit)
	if limit == 0 {
		note = scope + " max_parallel cleared (no limit)"
	}
	updated, err := m.automations.Update(id, expectedVersion, func(a *Automation) error {
		if err := checkControlStatus(a, parallelSettingStatuses); err != nil {
			return err
		}
		if memberID == "" {
			a.MaxParallel = limit
		} else {
			mem := automationMember(a, memberID)
			if mem == nil {
				return fmt.Errorf("404: automation %s has no member %s", id, memberID)
			}
			if mem.kind() != MemberKindFork {
				return fmt.Errorf("member %s is a %s member; max_parallel on a member is only valid on a fork", memberID, mem.kind())
			}
			mem.MaxParallel = limit
		}
		controlDecision(a, controlSetMaxParallel, actor, note)
		return nil
	})
	if err != nil {
		return nil, err
	}
	m.controlAudit(controlSetMaxParallel, map[string]any{
		"automation_id": id, "actor": actor, "version": updated.Version,
		"member_id": memberID, "max_parallel": limit,
	})
	return m.releaseHeldMembers(updated), nil
}

// SetStageGate turns the stage gate on or off. On, a staged member is not
// started while an earlier stage has unfinished members; off, a stage is only a
// label again. It records one Decision and one audit record. expectedVersion 0
// skips the version check.
func (m *Manager) SetStageGate(id string, on bool, actor string, expectedVersion int64) (*Automation, error) {
	if actor == "" {
		actor = operatorActor
	}
	note := "stage gate turned off"
	if on {
		note = "stage gate turned on"
	}
	updated, err := m.automations.Update(id, expectedVersion, func(a *Automation) error {
		if err := checkControlStatus(a, parallelSettingStatuses); err != nil {
			return err
		}
		a.StageGate = on
		controlDecision(a, controlSetStageGate, actor, note)
		return nil
	})
	if err != nil {
		return nil, err
	}
	m.controlAudit(controlSetStageGate, map[string]any{
		"automation_id": id, "actor": actor, "version": updated.Version, "stage_gate": on,
	})
	return m.releaseHeldMembers(updated), nil
}

// releaseHeldMembers lets the coordinator re-check an active Automation after a
// run-shape setting changed, so members the old setting held start under the
// new one without waiting for another member to finish. It returns the
// Automation as stored afterwards. A paused or blocked Automation starts
// nothing, and without a coordinator there is nothing to re-check.
func (m *Manager) releaseHeldMembers(updated *Automation) *Automation {
	if updated.Status != AutomationActive {
		return updated
	}
	m.mu.Lock()
	c := m.coordinator
	m.mu.Unlock()
	if c == nil {
		return updated
	}
	c.reconcileOne(updated)
	if final, ok := m.automations.Get(updated.ID); ok {
		return final
	}
	return updated
}
