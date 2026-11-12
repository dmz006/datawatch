// Blocked-PRD task approval (ApproveTask) — a PRD in PRDBlocked with a
// task stuck in TaskBlocked must accept operator approval, which releases
// the task back to TaskPending (clearing the stale error) for the existing
// run path to pick up. Draft/cancelled PRDs still refuse approval.
package autonomous

import (
	"testing"
	"time"
)

// seededBlockedPRD sets up the same single-story/two-task PRD as
// guidedModePRD, but places the PRD in PRDBlocked with task-1a
// TaskBlocked and a recorded Error, saved to the store.
func seededBlockedPRD(t *testing.T, prdStatus PRDStatus, taskStatus TaskStatus, taskErr string) (*Manager, *PRD) {
	t.Helper()
	m, prd := guidedModePRD(t)
	prd.Status = prdStatus
	prd.Story[0].Tasks[0].Status = taskStatus
	prd.Story[0].Tasks[0].Error = taskErr
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, prd
}

func TestApproveTask_BlockedPRD_BlockedTaskBecomesPending(t *testing.T) {
	m, prd := seededBlockedPRD(t, PRDBlocked, TaskBlocked, "foreign file drift detected")
	if m == nil {
		t.Fatal("expected manager")
	}

	updated, err := m.ApproveTask(prd.ID, "task-1a", "operator")
	if err != nil {
		t.Fatalf("ApproveTask on PRDBlocked rejected: %v", err)
	}
	task := updated.Story[0].Tasks[0]
	if task.Status != TaskPending {
		t.Errorf("task-1a status = %q; want %q after approving a blocked task", task.Status, TaskPending)
	}
	if task.Error != "" {
		t.Errorf("task-1a Error = %q; want cleared after approval", task.Error)
	}
	if !task.Approved {
		t.Error("task-1a Approved = false; want true")
	}
	if task.ApprovedBy != "operator" {
		t.Errorf("task-1a ApprovedBy = %q; want %q", task.ApprovedBy, "operator")
	}
	if task.ApprovedAt == nil {
		t.Error("task-1a ApprovedAt = nil; want set")
	} else if time.Since(*task.ApprovedAt).Abs() > time.Hour {
		t.Errorf("task-1a ApprovedAt = %s; want roughly now", task.ApprovedAt)
	}
	// PRD status is left for the run path — do NOT auto-transit.
	if updated.Status != PRDBlocked {
		t.Errorf("PRD status = %q; want %q left unchanged (run path owns the transition)", updated.Status, PRDBlocked)
	}
	found := false
	for _, d := range updated.Decisions {
		if d.Kind == "approve_task" {
			found = true
		}
	}
	if !found {
		t.Error("expected approve_task Decision to be recorded")
	}
}

func TestApproveTask_DraftPRD_StillRefused(t *testing.T) {
	m, prd := seededBlockedPRD(t, PRDDraft, TaskBlocked, "boom")
	if _, err := m.ApproveTask(prd.ID, "task-1a", "operator"); err == nil {
		t.Error("expected error approving a task on a PRD in draft status")
	}
}

func TestApproveTask_CancelledPRD_StillRefused(t *testing.T) {
	m, prd := seededBlockedPRD(t, PRDCancelled, TaskBlocked, "boom")
	if _, err := m.ApproveTask(prd.ID, "task-1a", "operator"); err == nil {
		t.Error("expected error approving a task on a PRD in cancelled status")
	}
}
