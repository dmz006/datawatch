package autonomous

// Boot-time reconciliation of orphaned verifying tasks: a TaskVerifying /
// TaskRunningTests task whose session is dead/no longer exists must resolve
// from its recorded verification instead of being blindly reset to
// TaskPending. Covers both boot paths:
//   - resetInProgressTasksForResume (resume path)
//   - reconcileStuckTasks (daemon-start path)
//
// Cases per path:
//   - recorded ok verdict + dead session → TaskCompleted, Verification intact
//   - failed verdict (OK=false) + dead session → TaskPending (executor retry)
//   - nil Verification + dead/absent session → TaskPending
//   - live session → stays TaskVerifying, not reset

import (
	"testing"
)

func verifyingBootManager(t *testing.T, stories []Story) (*Manager, *PRD) {
	t.Helper()
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "/projverifying", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, stories); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDRunning
	_ = m.Store().SavePRD(prd)
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, prd
}

func TestResetInProgressVerifying(t *testing.T) {
	t.Run("ok verdict dead session completes", func(t *testing.T) {
		m, prd := verifyingBootManager(t, []Story{
			{Title: "story", Tasks: []Task{
				{Title: "task", Status: TaskVerifying, SessionID: "sess-dead",
					Verification: &VerificationResult{OK: true, Summary: "looks good", VerifierID: "ver-1"}},
			}},
		})
		m.SetSessionAliveFn(func(string) bool { return false })
		m.resetInProgressTasksForResume(prd)

		got, _ := m.Store().GetPRD(prd.ID)
		task := got.Story[0].Tasks[0]
		if task.Status != TaskCompleted {
			t.Fatalf("want TaskCompleted, got %q", task.Status)
		}
		if task.Status == TaskVerifying {
			t.Fatal("task must never stay TaskVerifying with a dead session")
		}
		if task.CompletedAt == nil {
			t.Fatal("want CompletedAt set")
		}
		if task.SessionID != "" {
			t.Fatalf("want empty SessionID, got %q", task.SessionID)
		}
		if task.Error != "" {
			t.Fatalf("want empty Error, got %q", task.Error)
		}
		if task.Verification == nil || !task.Verification.OK || task.Verification.Summary != "looks good" || task.Verification.VerifierID != "ver-1" {
			t.Fatalf("Verification must be kept intact, got %+v", task.Verification)
		}
	})

	t.Run("failed verdict dead session goes pending", func(t *testing.T) {
		m, prd := verifyingBootManager(t, []Story{
			{Title: "story", Tasks: []Task{
				{Title: "task", Status: TaskVerifying, SessionID: "sess-dead",
					Verification: &VerificationResult{OK: false, Summary: "broken", Issues: []string{"boom"}}},
			}},
		})
		m.SetSessionAliveFn(func(string) bool { return false })
		m.resetInProgressTasksForResume(prd)

		got, _ := m.Store().GetPRD(prd.ID)
		task := got.Story[0].Tasks[0]
		if task.Status != TaskPending {
			t.Fatalf("want TaskPending (retry path), got %q", task.Status)
		}
		if task.SessionID != "" {
			t.Fatalf("want empty SessionID, got %q", task.SessionID)
		}
	})

	t.Run("nil verification dead session goes pending", func(t *testing.T) {
		m, prd := verifyingBootManager(t, []Story{
			{Title: "story", Tasks: []Task{
				{Title: "task", Status: TaskVerifying, SessionID: "sess-dead"},
			}},
		})
		m.SetSessionAliveFn(func(string) bool { return false })
		m.resetInProgressTasksForResume(prd)

		got, _ := m.Store().GetPRD(prd.ID)
		if got.Story[0].Tasks[0].Status != TaskPending {
			t.Fatalf("want TaskPending, got %q", got.Story[0].Tasks[0].Status)
		}
	})

	t.Run("missing session goes pending", func(t *testing.T) {
		m, prd := verifyingBootManager(t, []Story{
			{Title: "story", Tasks: []Task{
				{Title: "task", Status: TaskVerifying},
			}},
		})
		m.SetSessionAliveFn(func(string) bool { return false })
		m.resetInProgressTasksForResume(prd)

		got, _ := m.Store().GetPRD(prd.ID)
		if got.Story[0].Tasks[0].Status != TaskPending {
			t.Fatalf("want TaskPending, got %q", got.Story[0].Tasks[0].Status)
		}
	})

	t.Run("live session stays verifying", func(t *testing.T) {
		m, prd := verifyingBootManager(t, []Story{
			{Title: "story", Tasks: []Task{
				{Title: "task", Status: TaskVerifying, SessionID: "sess-alive",
					Verification: &VerificationResult{OK: true, Summary: "looks good"}},
			}},
		})
		m.SetSessionAliveFn(func(id string) bool { return id == "sess-alive" })
		m.resetInProgressTasksForResume(prd)

		got, _ := m.Store().GetPRD(prd.ID)
		task := got.Story[0].Tasks[0]
		if task.Status != TaskVerifying {
			t.Fatalf("want TaskVerifying (live session), got %q", task.Status)
		}
		if task.SessionID != "sess-alive" {
			t.Fatalf("want SessionID sess-alive kept, got %q", task.SessionID)
		}
		if task.CompletedAt != nil {
			t.Fatalf("live session task must not be reset/completed, CompletedAt=%v", *task.CompletedAt)
		}
	})
}

func TestReconcileStuckVerifying(t *testing.T) {
	t.Run("ok verdict dead session completes", func(t *testing.T) {
		m, prd := verifyingBootManager(t, []Story{
			{Title: "story", Tasks: []Task{
				{Title: "task", Status: TaskVerifying, SessionID: "sess-dead",
					Verification: &VerificationResult{OK: true, Summary: "looks good", VerifierID: "ver-1"}},
			}},
		})
		m.SetSessionAliveFn(func(string) bool { return false })
		m.reconcileStuckTasks()

		got, _ := m.Store().GetPRD(prd.ID)
		task := got.Story[0].Tasks[0]
		if task.Status != TaskCompleted {
			t.Fatalf("want TaskCompleted, got %q", task.Status)
		}
		if task.Status == TaskVerifying {
			t.Fatal("task must never stay TaskVerifying with a dead session")
		}
		if task.CompletedAt == nil {
			t.Fatal("want CompletedAt set")
		}
		if task.SessionID != "" {
			t.Fatalf("want empty SessionID, got %q", task.SessionID)
		}
		if task.Error != "" {
			t.Fatalf("want empty Error, got %q", task.Error)
		}
		if task.Verification == nil || !task.Verification.OK || task.Verification.Summary != "looks good" || task.Verification.VerifierID != "ver-1" {
			t.Fatalf("Verification must be kept intact, got %+v", task.Verification)
		}
	})

	t.Run("failed verdict dead session goes pending", func(t *testing.T) {
		m, prd := verifyingBootManager(t, []Story{
			{Title: "story", Tasks: []Task{
				{Title: "task", Status: TaskVerifying, SessionID: "sess-dead",
					Verification: &VerificationResult{OK: false, Summary: "broken", Issues: []string{"boom"}}},
			}},
		})
		m.SetSessionAliveFn(func(string) bool { return false })
		m.reconcileStuckTasks()

		got, _ := m.Store().GetPRD(prd.ID)
		task := got.Story[0].Tasks[0]
		if task.Status != TaskPending {
			t.Fatalf("want TaskPending (retry path), got %q", task.Status)
		}
		if task.SessionID != "" {
			t.Fatalf("want empty SessionID, got %q", task.SessionID)
		}
	})

	t.Run("nil verification dead session goes pending", func(t *testing.T) {
		m, prd := verifyingBootManager(t, []Story{
			{Title: "story", Tasks: []Task{
				{Title: "task", Status: TaskVerifying, SessionID: "sess-dead"},
			}},
		})
		m.SetSessionAliveFn(func(string) bool { return false })
		m.reconcileStuckTasks()

		got, _ := m.Store().GetPRD(prd.ID)
		if got.Story[0].Tasks[0].Status != TaskPending {
			t.Fatalf("want TaskPending, got %q", got.Story[0].Tasks[0].Status)
		}
	})

	t.Run("missing session goes pending", func(t *testing.T) {
		m, prd := verifyingBootManager(t, []Story{
			{Title: "story", Tasks: []Task{
				{Title: "task", Status: TaskVerifying},
			}},
		})
		m.SetSessionAliveFn(func(string) bool { return false })
		m.reconcileStuckTasks()

		got, _ := m.Store().GetPRD(prd.ID)
		if got.Story[0].Tasks[0].Status != TaskPending {
			t.Fatalf("want TaskPending, got %q", got.Story[0].Tasks[0].Status)
		}
	})

	t.Run("live session stays verifying", func(t *testing.T) {
		m, prd := verifyingBootManager(t, []Story{
			{Title: "story", Tasks: []Task{
				{Title: "task", Status: TaskVerifying, SessionID: "sess-alive",
					Verification: &VerificationResult{OK: true, Summary: "looks good"}},
			}},
		})
		m.SetSessionAliveFn(func(id string) bool { return id == "sess-alive" })
		m.reconcileStuckTasks()

		got, _ := m.Store().GetPRD(prd.ID)
		task := got.Story[0].Tasks[0]
		if task.Status != TaskVerifying {
			t.Fatalf("want TaskVerifying (live session), got %q", task.Status)
		}
		if task.SessionID != "sess-alive" {
			t.Fatalf("want SessionID sess-alive kept, got %q", task.SessionID)
		}
		if task.CompletedAt != nil {
			t.Fatalf("live session task must not be reset/completed, CompletedAt=%v", *task.CompletedAt)
		}
	})
}
