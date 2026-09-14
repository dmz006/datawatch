// BL382 unit tests — CancelStory, CancelTask, ResetTask(force=true).
package autonomous

import (
	"strings"
	"testing"
)

// bl382RunningPRD creates a running PRD with two stories + tasks via NewManager.
func bl382RunningPRD(t *testing.T) (*Manager, *PRD) {
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
		{
			ID: "story-2", Title: "Story Two",
			Tasks: []Task{
				{ID: "task-2a", Title: "Task 2A"},
				{ID: "task-2b", Title: "Task 2B"},
			},
		},
	})
	// Force PRD to running state.
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDRunning
	prd.Story[0].Status = StoryInProgress
	prd.Story[0].Tasks[0].Status = TaskInProgress
	prd.Story[0].Tasks[0].SessionID = "sess-1a"
	prd.Story[0].Tasks[1].Status = TaskPending
	prd.Story[1].Status = StoryPending
	prd.Story[1].Tasks[0].Status = TaskCompleted
	prd.Story[1].Tasks[1].Status = TaskFailed
	prd.Story[1].Tasks[1].Error = "boom"
	_ = m.Store().SavePRD(prd)
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, prd
}

// --- CancelStory ---

func TestBL382_CancelStory_PendingStory_MarkedCancelled(t *testing.T) {
	m, prd := bl382RunningPRD(t)
	_, err := m.CancelStory(prd.ID, "story-2", "operator", "no longer needed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Story[1].Status != StoryCancelled {
		t.Errorf("story-2 status = %q; want cancelled", got.Story[1].Status)
	}
	found := false
	for _, d := range got.Decisions {
		if d.Kind == "cancel_story" {
			found = true
		}
	}
	if !found {
		t.Error("expected cancel_story Decision to be recorded")
	}
}

func TestBL382_CancelStory_InProgressStory_SessionKilled(t *testing.T) {
	m, prd := bl382RunningPRD(t)
	killed := []string{}
	m.SetSessionKillerFn(func(id string) error { killed = append(killed, id); return nil })

	_, err := m.CancelStory(prd.ID, "story-1", "operator", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(killed) == 0 || killed[0] != "sess-1a" {
		t.Errorf("expected sess-1a to be killed; got %v", killed)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Story[0].Status != StoryCancelled {
		t.Errorf("story-1 status = %q; want cancelled", got.Story[0].Status)
	}
	// Non-completed tasks should also be marked cancelled.
	for _, tk := range got.Story[0].Tasks {
		if tk.Status != TaskCancelled && tk.Status != TaskCompleted {
			t.Errorf("task %s status = %q; want cancelled", tk.ID, tk.Status)
		}
	}
}

func TestBL382_CancelStory_AlreadyCancelled_Returns409(t *testing.T) {
	m, prd := bl382RunningPRD(t)
	prd.Story[1].Status = StoryCancelled
	_ = m.Store().SavePRD(prd)

	_, err := m.CancelStory(prd.ID, "story-2", "operator", "")
	if err == nil || !strings.HasPrefix(err.Error(), "409:") {
		t.Errorf("expected 409 error; got %v", err)
	}
}

func TestBL382_CancelStory_NotFound_ReturnsError(t *testing.T) {
	m, prd := bl382RunningPRD(t)
	_, err := m.CancelStory(prd.ID, "story-99", "operator", "")
	if err == nil {
		t.Error("expected error for unknown story")
	}
}

// --- CancelTask ---

func TestBL382_CancelTask_PendingTask_MarkedCancelled(t *testing.T) {
	m, prd := bl382RunningPRD(t)
	_, err := m.CancelTask(prd.ID, "task-1b", "operator", "skip it")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Story[0].Tasks[1].Status != TaskCancelled {
		t.Errorf("task-1b status = %q; want cancelled", got.Story[0].Tasks[1].Status)
	}
}

func TestBL382_CancelTask_InProgressTask_SessionKilled(t *testing.T) {
	m, prd := bl382RunningPRD(t)
	killed := []string{}
	m.SetSessionKillerFn(func(id string) error { killed = append(killed, id); return nil })

	_, err := m.CancelTask(prd.ID, "task-1a", "operator", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(killed) == 0 || killed[0] != "sess-1a" {
		t.Errorf("expected sess-1a killed; got %v", killed)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Story[0].Tasks[0].Status != TaskCancelled {
		t.Errorf("task-1a status = %q; want cancelled", got.Story[0].Tasks[0].Status)
	}
}

func TestBL382_CancelTask_AlreadyCancelled_Returns409(t *testing.T) {
	m, prd := bl382RunningPRD(t)
	prd.Story[0].Tasks[1].Status = TaskCancelled
	_ = m.Store().SavePRD(prd)

	_, err := m.CancelTask(prd.ID, "task-1b", "operator", "")
	if err == nil || !strings.HasPrefix(err.Error(), "409:") {
		t.Errorf("expected 409 error; got %v", err)
	}
}

func TestBL382_CancelTask_AlreadyCompleted_Returns409(t *testing.T) {
	m, prd := bl382RunningPRD(t)
	_, err := m.CancelTask(prd.ID, "task-2a", "operator", "")
	if err == nil || !strings.HasPrefix(err.Error(), "409:") {
		t.Errorf("expected 409 error for completed task; got %v", err)
	}
}

// --- ResetTask with force ---

func TestBL382_ResetTask_Force_CompletedTask_ResetsToPending(t *testing.T) {
	m, prd := bl382RunningPRD(t)
	_, err := m.ResetTask(prd.ID, "task-2a", "operator", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	// Status "" means pending in the task state machine.
	if got.Story[1].Tasks[0].Status != "" {
		t.Errorf("task-2a status = %q; want empty (pending)", got.Story[1].Tasks[0].Status)
	}
	found := false
	for _, d := range got.Decisions {
		if d.Kind == "requeue_task" {
			found = true
		}
	}
	if !found {
		t.Error("expected requeue_task Decision to be recorded")
	}
}

func TestBL382_ResetTask_NoForce_CompletedTask_ReturnsError(t *testing.T) {
	m, prd := bl382RunningPRD(t)
	_, err := m.ResetTask(prd.ID, "task-2a", "operator", false)
	if err == nil {
		t.Error("expected error when resetting completed task without force")
	}
}

func TestBL382_ResetTask_Force_CancelledTask_ResetsToPending(t *testing.T) {
	m, prd := bl382RunningPRD(t)
	prd.Story[0].Tasks[1].Status = TaskCancelled
	_ = m.Store().SavePRD(prd)

	_, err := m.ResetTask(prd.ID, "task-1b", "operator", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Story[0].Tasks[1].Status != "" {
		t.Errorf("task-1b status = %q; want empty (pending)", got.Story[0].Tasks[1].Status)
	}
}

func TestBL382_ResetTask_FailedTask_NoForce_StillWorks(t *testing.T) {
	m, prd := bl382RunningPRD(t)
	_, err := m.ResetTask(prd.ID, "task-2b", "operator", false)
	if err != nil {
		t.Fatalf("failed task should still reset without force: %v", err)
	}
}
