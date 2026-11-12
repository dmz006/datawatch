// Automation budget admission: a new task start of a member PRD waits while
// the Automation it belongs to is at a dollar ceiling. Only the hard ceiling
// holds here (never pacing, a start estimate or the allocation figure), and
// only new worker starts: a task already holding a lease, its verification and
// planner work are never interrupted, so work in progress finishes.

package autonomous

import (
	"context"
	"fmt"
	"time"
)

// defaultAutomationBudgetPoll is how often a held task re-checks its budget.
const defaultAutomationBudgetPoll = 5 * time.Second

// SetAutomationBudgetPoll sets how often a task held by an Automation budget
// re-checks the hold; zero or negative restores the default.
func (m *Manager) SetAutomationBudgetPoll(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.autoBudgetPoll = d
}

func (m *Manager) automationBudgetPoll() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.autoBudgetPoll > 0 {
		return m.autoBudgetPoll
	}
	return defaultAutomationBudgetPoll
}

// automationBudgetHold reports whether a new task of prd must wait because its
// Automation (or the member's own policy) is at a dollar ceiling, and why. It
// holds nothing for a PRD outside an Automation, an Automation that is missing
// or finished, a PRD that is not a member, or when no evaluator is wired.
func (m *Manager) automationBudgetHold(prd *PRD) (hold bool, reason string) {
	if prd == nil || prd.AutomationID == "" {
		return false, ""
	}
	b := m.AutomationBudget()
	if b == nil || m.automations == nil {
		return false, ""
	}
	a, ok := m.GetAutomation(prd.AutomationID)
	if !ok || a == nil || a.Status == AutomationCompleted || a.Status == AutomationCancelled {
		return false, ""
	}
	var member *AutomationMember
	for i := range a.Members {
		if a.Members[i].kind() == MemberKindPRD && a.Members[i].PRDID == prd.ID {
			member = &a.Members[i]
			break
		}
	}
	if member == nil {
		return false, ""
	}
	v := b.Evaluate(a, member, true)
	if !v.Gate || v.Kind != BudgetKindCeiling {
		return false, ""
	}
	return true, automationBudgetHoldReason(v)
}

// automationBudgetHoldReason words the wait reason for a ceiling verdict.
func automationBudgetHoldReason(v BudgetVerdict) string {
	reset := "reset time unknown"
	if v.ResetKnown && v.ResetAt != nil {
		reset = "resets " + v.ResetAt.Format("2006-01-02 15:04 MST")
	}
	scope := v.Scope
	if scope == "" {
		scope = "automation"
	}
	return fmt.Sprintf("automation budget reached (%s): spent $%.2f of the $%.2f ceiling; %s", scope, v.SpentUSD, v.CeilingUSD, reset)
}

// waitAutomationBudget blocks a new task start while its Automation is at a
// dollar ceiling. The task shows as waiting with the reason, the wait has no
// timeout, and it never counts as a retry or a failure, exactly like the
// capacity wait. It returns errCancelledWhileWaiting when the task is
// cancelled and the context error when ctx ends.
func (m *Manager) waitAutomationBudget(ctx context.Context, prd *PRD, t *Task) error {
	hold, reason := m.automationBudgetHold(prd)
	if !hold {
		return nil
	}
	poll := m.automationBudgetPoll()
	clock := m.spawnWaitClock()
	shown := ""
	for hold {
		if m.taskCancelled(t.ID) {
			return errCancelledWhileWaiting
		}
		if reason != shown {
			m.setWaiting(t, reason)
			shown = reason
		}
		select {
		case <-ctx.Done():
			if m.taskCancelled(t.ID) {
				return errCancelledWhileWaiting
			}
			return ctx.Err()
		case <-clock.After(poll):
		}
		hold, reason = m.automationBudgetHold(prd)
	}
	m.clearWaiting(t)
	return nil
}
