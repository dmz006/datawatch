// B90 — boot-time stuck-task reconciliation.
//
// TC-1: stuck task (TaskVerifying, dead session) → TaskPending on reconcile
// TC-2: stuck task with live session → left alone when sessionAliveFn set
// TC-3: no sessionAliveFn → all stuck tasks reset to pending (conservative: assume dead)
// TC-4: TaskFailed tasks not touched; already-terminal tasks untouched
// TC-5: PRDs not in Running/Active state are skipped
// TC-6: boot_reconcile Decision appended exactly once per PRD with changes

package autonomous

import (
	"testing"
)

func b90Setup(t *testing.T) (*Manager, *PRD) {
	t.Helper()
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "/projb90", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{
		{
			Title: "story",
			Tasks: []Task{
				{Title: "task", Status: TaskVerifying, SessionID: "sess-dead"},
				{Title: "task2", Status: TaskCompleted},
			},
		},
	})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDRunning
	_ = m.Store().SavePRD(prd)
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, prd
}

func TestB90_StuckTaskResetOnReconcile(t *testing.T) {
	m, prd := b90Setup(t)
	// no sessionAliveFn → conservative: dead session → TaskPending so executor can retry
	m.reconcileStuckTasks()

	got, _ := m.Store().GetPRD(prd.ID)
	task := got.Story[0].Tasks[0]
	if task.Status != TaskPending {
		t.Fatalf("want TaskPending, got %q", task.Status)
	}
	if task.Error != "" {
		t.Fatalf("want empty error on reset, got %q", task.Error)
	}
	if task.SessionID != "" {
		t.Fatalf("want empty SessionID on reset, got %q", task.SessionID)
	}
}

func TestB90_LiveSessionSkipped(t *testing.T) {
	m, prd := b90Setup(t)
	m.SetSessionAliveFn(func(sessionID string) bool {
		return sessionID == "sess-dead" // pretend session is alive
	})
	m.reconcileStuckTasks()

	got, _ := m.Store().GetPRD(prd.ID)
	task := got.Story[0].Tasks[0]
	if task.Status != TaskVerifying {
		t.Fatalf("want TaskVerifying (alive), got %q", task.Status)
	}
}

func TestB90_NoAliveFnResetsTopending(t *testing.T) {
	m, prd := b90Setup(t)
	// sessionAliveFn = nil: no way to check liveness → treat all as dead → TaskPending
	m.reconcileStuckTasks()

	got, _ := m.Store().GetPRD(prd.ID)
	if got.Story[0].Tasks[0].Status != TaskPending {
		t.Fatalf("want TaskPending (no alive fn), got %q", got.Story[0].Tasks[0].Status)
	}
}

func TestB90_TerminalTasksUntouched(t *testing.T) {
	m, prd := b90Setup(t)
	m.reconcileStuckTasks()

	got, _ := m.Store().GetPRD(prd.ID)
	completed := got.Story[0].Tasks[1]
	if completed.Status != TaskCompleted {
		t.Fatalf("completed task should be untouched, got %q", completed.Status)
	}
}

func TestB90_NonRunningPRDSkipped(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", "/proj", "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{
		{ID: "s1", PRDID: prd.ID, Tasks: []Task{
			{ID: "t1", StoryID: "s1", PRDID: prd.ID, Status: TaskVerifying, SessionID: "sess"},
		}},
	})
	// PRD is still Draft — not Running/Active — should be skipped
	m.reconcileStuckTasks()

	got, _ := m.Store().GetPRD(prd.ID)
	if got.Story[0].Tasks[0].Status != TaskVerifying {
		t.Fatalf("draft PRD task should be untouched, got %q", got.Story[0].Tasks[0].Status)
	}
}

func TestB90_DecisionAppended(t *testing.T) {
	m, prd := b90Setup(t)
	initialDecisions := len(prd.Decisions)
	m.reconcileStuckTasks()

	got, _ := m.Store().GetPRD(prd.ID)
	added := len(got.Decisions) - initialDecisions
	if added != 1 {
		t.Fatalf("want 1 boot_reconcile decision appended, got %d added (total %d)", added, len(got.Decisions))
	}
	last := got.Decisions[len(got.Decisions)-1]
	if last.Kind != "boot_reconcile" {
		t.Fatalf("want kind=boot_reconcile, got %q", last.Kind)
	}
}
