// BL406 Phase 5 unit tests — GuidedModeSource migration/resolution,
// task-level approval gating (Approve, ApproveTask, flattenTasks).
package autonomous

import (
	"testing"
)

func guidedModePRD(t *testing.T) (*Manager, *PRD) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "/p", "claude", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{
		{
			ID: "story-1", Title: "Story One",
			Tasks: []Task{
				{ID: "task-1a", Title: "Task 1A"},
				{ID: "task-1b", Title: "Task 1B"},
			},
		},
	})
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, prd
}

func TestMigrateGuidedMode_LegacyBoolMigratesToOperator(t *testing.T) {
	prd := &PRD{GuidedMode: true}
	migrateGuidedMode(prd)
	if prd.GuidedModeSource != GuidedModeOperator {
		t.Errorf("GuidedModeSource = %q; want %q", prd.GuidedModeSource, GuidedModeOperator)
	}
}

func TestMigrateGuidedMode_NeverOverwritesExplicitSource(t *testing.T) {
	prd := &PRD{GuidedMode: true, GuidedModeSource: GuidedModeGuardrailAuto}
	migrateGuidedMode(prd)
	if prd.GuidedModeSource != GuidedModeGuardrailAuto {
		t.Errorf("GuidedModeSource = %q; want unchanged %q", prd.GuidedModeSource, GuidedModeGuardrailAuto)
	}
}

func TestMigrateGuidedMode_NoBoolNoSource_StaysEmpty(t *testing.T) {
	prd := &PRD{}
	migrateGuidedMode(prd)
	if prd.GuidedModeSource != "" {
		t.Errorf("GuidedModeSource = %q; want empty", prd.GuidedModeSource)
	}
}

func TestResolveGuidedModeSource_CouncilFallsBackToOperatorWhenUnwired(t *testing.T) {
	m, prd := guidedModePRD(t)
	prd.GuidedModeSource = GuidedModeCouncil
	got := m.resolveGuidedModeSource(prd)
	if got != GuidedModeOperator {
		t.Errorf("resolveGuidedModeSource = %q; want fallback %q", got, GuidedModeOperator)
	}
}

func TestResolveGuidedModeSource_OperatorPassesThrough(t *testing.T) {
	m, prd := guidedModePRD(t)
	prd.GuidedModeSource = GuidedModeOperator
	if got := m.resolveGuidedModeSource(prd); got != GuidedModeOperator {
		t.Errorf("resolveGuidedModeSource = %q; want %q", got, GuidedModeOperator)
	}
}

func TestSetPRDGuidedMode_LiveAppliesOperatorSource(t *testing.T) {
	m, prd := guidedModePRD(t)
	if err := m.SetPRDGuidedMode(prd.ID, true); err != nil {
		t.Fatalf("SetPRDGuidedMode: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.GuidedModeSource != GuidedModeOperator {
		t.Errorf("GuidedModeSource = %q; want %q (applied live, not only at next load)", got.GuidedModeSource, GuidedModeOperator)
	}
	if err := m.SetPRDGuidedMode(prd.ID, false); err != nil {
		t.Fatalf("SetPRDGuidedMode(false): %v", err)
	}
	got, _ = m.Store().GetPRD(prd.ID)
	if got.GuidedModeSource != "" {
		t.Errorf("GuidedModeSource = %q; want cleared after disabling", got.GuidedModeSource)
	}
}

func TestApprove_OperatorSource_GatesPendingTasksToAwaitingApproval(t *testing.T) {
	m, prd := guidedModePRD(t)
	prd.GuidedModeSource = GuidedModeOperator
	prd.Status = PRDNeedsReview
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	updated, err := m.Approve(prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	for _, task := range updated.Story[0].Tasks {
		if task.Status != TaskAwaitingApproval {
			t.Errorf("task %s status = %q; want %q", task.ID, task.Status, TaskAwaitingApproval)
		}
	}
}

func TestApprove_NoGuidedMode_TasksStayPending(t *testing.T) {
	m, prd := guidedModePRD(t)
	prd.Status = PRDNeedsReview
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	updated, err := m.Approve(prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	for _, task := range updated.Story[0].Tasks {
		if task.Status == TaskAwaitingApproval {
			t.Errorf("task %s unexpectedly gated without guided_mode_source set", task.ID)
		}
	}
}

func TestFlattenTasks_SkipsAwaitingApprovalTasks(t *testing.T) {
	_, prd := guidedModePRD(t)
	prd.Story[0].Tasks[0].Status = TaskAwaitingApproval
	prd.Story[0].Tasks[1].Status = TaskPending

	out := flattenTasks(prd)
	if len(out) != 1 || out[0].ID != "task-1b" {
		t.Errorf("flattenTasks returned %d tasks; want only task-1b (task-1a gated)", len(out))
	}
}

func TestApproveTask_ReEntersFlattenTasksAfterApproval(t *testing.T) {
	m, prd := guidedModePRD(t)
	prd.Status = PRDRunning
	prd.Story[0].Tasks[0].Status = TaskAwaitingApproval
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	updated, err := m.ApproveTask(prd.ID, "task-1a", "operator")
	if err != nil {
		t.Fatalf("ApproveTask: %v", err)
	}
	if updated.Story[0].Tasks[0].Status != TaskPending {
		t.Errorf("task-1a status = %q; want %q after approval", updated.Story[0].Tasks[0].Status, TaskPending)
	}
	if !updated.Story[0].Tasks[0].Approved || updated.Story[0].Tasks[0].ApprovedBy != "operator" {
		t.Errorf("task-1a Approved/ApprovedBy not set: %+v", updated.Story[0].Tasks[0])
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

	out := flattenTasks(updated)
	if len(out) != 2 {
		t.Errorf("flattenTasks returned %d tasks after approval; want 2 (both now eligible)", len(out))
	}
}

func TestApproveTask_NotFound(t *testing.T) {
	m, prd := guidedModePRD(t)
	prd.Status = PRDRunning
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	if _, err := m.ApproveTask(prd.ID, "no-such-task", "operator"); err == nil {
		t.Error("expected error for unknown task ID")
	}
}

func TestApproveTask_RejectsPRDInWrongStatus(t *testing.T) {
	m, prd := guidedModePRD(t)
	prd.Status = PRDNeedsReview
	prd.Story[0].Tasks[0].Status = TaskAwaitingApproval
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	if _, err := m.ApproveTask(prd.ID, "task-1a", "operator"); err == nil {
		t.Error("expected error approving a task on a PRD still in needs_review")
	}
}
