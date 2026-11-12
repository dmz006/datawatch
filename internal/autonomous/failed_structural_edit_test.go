package autonomous

import (
	"strings"
	"testing"
)

// failedPRDFixture returns a manager holding an Automaton in the given status
// with a completed task, a pending task and a failed (non scope-lint) task.
func failedPRDFixture(t *testing.T, status PRDStatus) (*Manager, *PRD) {
	t.Helper()
	m, prd := bl382RunningPRD(t)
	prd.Status = status
	prd.Story[0].Tasks[0].Status = TaskCompleted
	prd.Story[0].Tasks[0].SessionID = ""
	_ = m.Store().SavePRD(prd)
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, prd
}

func TestFailedAcceptsStructuralEdits(t *testing.T) {
	m, prd := failedPRDFixture(t, PRDFailed)

	if _, err := m.AddTask(prd.ID, "story-1", "Added", "do the added work", "op", nil); err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if _, err := m.RemoveTask(prd.ID, "story-1", "task-1b", "op"); err != nil {
		t.Fatalf("RemoveTask: %v", err)
	}
	if _, err := m.EditStory(prd.ID, "story-2", "Renamed", "", "op"); err != nil {
		t.Fatalf("EditStory: %v", err)
	}
	if _, err := m.RepairDependsOn(prd.ID, "op"); err != nil {
		t.Fatalf("RepairDependsOn: %v", err)
	}
	got, err := m.RequestRevision(prd.ID, "op", "fix it")
	if err != nil {
		t.Fatalf("RequestRevision: %v", err)
	}
	if got.Status != PRDRevisionsAsked {
		t.Errorf("status = %q, want revisions_asked", got.Status)
	}
}

func TestFailedCompletedTaskSpecStaysImmutable(t *testing.T) {
	m, prd := failedPRDFixture(t, PRDFailed)
	_, err := m.EditTaskSpec(prd.ID, "task-1a", "new spec", "op")
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("err = %v, want immutable refusal", err)
	}
}

func TestFailedNonScopeLintTaskEditKeepsBehaviour(t *testing.T) {
	m, prd := failedPRDFixture(t, PRDFailed)
	// task-2b failed with an ordinary error, not a scope-lint one.
	_, err := m.EditTaskSpec(prd.ID, "task-2b", "new spec", "op")
	if err == nil || !strings.Contains(err.Error(), "scope lint") {
		t.Fatalf("err = %v, want the failed-prd scope-lint refusal", err)
	}
}

func TestFailedResetReopensAndAddedTaskRunnable(t *testing.T) {
	m, prd := failedPRDFixture(t, PRDFailed)
	out, err := m.AddTask(prd.ID, "story-2", "Added", "do the added work", "op", nil)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	var addedID string
	for _, tk := range out.Story[1].Tasks {
		if tk.Title == "Added" {
			addedID = tk.ID
		}
	}
	if addedID == "" {
		t.Fatal("added task not found")
	}
	if _, err := m.ResetTask(prd.ID, "task-2b", "op", false); err != nil {
		t.Fatalf("ResetTask: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status != PRDRunning {
		t.Errorf("status = %q, want running", got.Status)
	}
	for _, tk := range got.Story[1].Tasks {
		if tk.ID == addedID && normTaskStatus(tk.Status) != TaskPending {
			t.Errorf("added task status = %q, want pending", tk.Status)
		}
		if tk.ID == "task-2b" && normTaskStatus(tk.Status) != TaskPending {
			t.Errorf("reset task status = %q, want pending", tk.Status)
		}
	}
}

func TestStructuralEditsRefusedOutsideSet(t *testing.T) {
	for _, st := range []PRDStatus{PRDRunning, PRDCompleted} {
		m, prd := failedPRDFixture(t, st)
		if _, err := m.AddTask(prd.ID, "story-1", "x", "y", "op", nil); err == nil {
			t.Errorf("%s: AddTask accepted", st)
		}
		// A running Automaton accepts removal of a task that is not running
		// and not queued; see remove_cancelled_task_running_test.go.
		if _, err := m.RemoveTask(prd.ID, "story-1", "task-1b", "op"); err == nil && st != PRDRunning {
			t.Errorf("%s: RemoveTask accepted", st)
		}
		if _, err := m.EditStory(prd.ID, "story-1", "t", "", "op"); err == nil {
			t.Errorf("%s: EditStory accepted", st)
		}
		if _, err := m.RequestRevision(prd.ID, "op", "n"); err == nil {
			t.Errorf("%s: RequestRevision accepted", st)
		}
	}
}
