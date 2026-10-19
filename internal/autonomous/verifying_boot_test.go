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
	"strings"
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

func TestBootVerifyingDecisionLog(t *testing.T) {
	// Auditable boot-time resolution: each PRD with orphaned verifying tasks
	// resolved must carry exactly one verifying_resolution Decision naming
	// how many completed vs re-queued; a PRD with nothing resolvable must not
	// gain a spurious entry. Covers both boot passes.
	entryCount := func(got *PRD) int {
		n := 0
		for _, d := range got.Decisions {
			if d.Kind == "verifying_resolution" {
				n++
			}
		}
		return n
	}

	okVerdictStory := func() []Story {
		return []Story{
			{Title: "story", Tasks: []Task{
				{Title: "task", Status: TaskVerifying, SessionID: "sess-dead",
					Verification: &VerificationResult{OK: true, Summary: "looks good", VerifierID: "ver-1"}},
			}},
		}
	}

	resolvable := func(t *testing.T, stories []Story, boot func(m *Manager, prd *PRD)) (*Manager, *PRD) {
		t.Helper()
		m, prd := verifyingBootManager(t, stories)
		m.SetSessionAliveFn(func(string) bool { return false })
		boot(m, prd)
		return m, prd
	}

	drive := func(t *testing.T, name string, boot func(m *Manager, prd *PRD)) {
		t.Helper()

		t.Run(name+"_records_exactly_one_verifying_resolution", func(t *testing.T) {
			m, prd := resolvable(t, okVerdictStory(), boot)
			got, _ := m.Store().GetPRD(prd.ID)

			task := got.Story[0].Tasks[0]
			if task.Status != TaskCompleted {
				t.Fatalf("want terminal TaskCompleted, got %q", task.Status)
			}

			n := entryCount(got)
			if n != 1 {
				t.Fatalf("want exactly 1 verifying_resolution decision, got %d: %+v", n, got.Decisions)
			}
			var d *Decision
			for i := range got.Decisions {
				if got.Decisions[i].Kind == "verifying_resolution" {
					d = &got.Decisions[i]
					break
				}
			}
			if d.Actor != "autonomous" {
				t.Fatalf("want Actor autonomous, got %q", d.Actor)
			}
			if d.Note == "" || !strings.Contains(d.Note, "1") {
				t.Fatalf("note must name the resolved counts, got %q", d.Note)
			}
			if d.At.IsZero() {
				t.Fatal("verifying_resolution decision must carry a timestamp")
			}
		})

		t.Run(name+"_nothing_resolvable_no_spurious_entry", func(t *testing.T) {
			// Only a pending task with no session and a live verifying task:
			// nothing is an orphaned-with-dead-session verifying task.
			m, prd := verifyingBootManager(t, []Story{
				{Title: "story", Tasks: []Task{
					{Title: "pending", Status: TaskPending},
					{Title: "livever", Status: TaskVerifying, SessionID: "sess-alive",
						Verification: &VerificationResult{OK: true, Summary: "looks good"}},
				}},
			})
			before := entryCount(prd)
			m.SetSessionAliveFn(func(id string) bool { return id == "sess-alive" })
			boot(m, prd)
			got, _ := m.Store().GetPRD(prd.ID)
			if entryCount(got) != before {
				t.Fatalf("no verifying_resolution entry expected, got %+v", got.Decisions)
			}
		})
	}

	drive(t, "reset-resume", func(m *Manager, prd *PRD) { m.resetInProgressTasksForResume(prd) })
	drive(t, "daemon-start", func(m *Manager, prd *PRD) { m.reconcileStuckTasks() })
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
