package autonomous

import "testing"

// A verifying task whose worker already finished must stay verifying across
// the daemon-start pass: the worker is not run again and no retry is counted.
func TestReconcileStuckKeepsFinishedWorkerVerifying(t *testing.T) {
	t.Run("recorded worker result", func(t *testing.T) {
		m, prd := verifyingBootManager(t, []Story{
			{Title: "story", Tasks: []Task{
				{Title: "task", Status: TaskVerifying, SessionID: "sess-worker-ended",
					AttemptCommitSHAs: []string{"abc123"}},
			}},
		})
		m.SetSessionAliveFn(func(string) bool { return false })
		m.reconcileStuckTasks()

		got, _ := m.Store().GetPRD(prd.ID)
		task := got.Story[0].Tasks[0]
		if task.Status != TaskVerifying {
			t.Fatalf("want TaskVerifying kept, got %q", task.Status)
		}
		if task.SessionID != "sess-worker-ended" {
			t.Fatalf("want SessionID kept, got %q", task.SessionID)
		}
		if len(task.AttemptCommitSHAs) != 1 {
			t.Fatalf("want worker result kept, got %v", task.AttemptCommitSHAs)
		}
	})

	t.Run("completion marker in recorded output", func(t *testing.T) {
		m, prd := verifyingBootManager(t, []Story{
			{Title: "story", Tasks: []Task{
				{Title: "task", Status: TaskVerifying, SessionID: "sess-worker-ended"},
			}},
		})
		m.SetSessionAliveFn(func(string) bool { return false })
		m.SetSessionOutputFn(func(string) (string, bool) { return "done\nDATAWATCH_COMPLETE: all good\n", true })
		killed := 0
		m.SetSessionKillerFn(func(string, string, string) error { killed++; return nil })
		m.reconcileStuckTasks()

		got, _ := m.Store().GetPRD(prd.ID)
		task := got.Story[0].Tasks[0]
		if task.Status != TaskVerifying {
			t.Fatalf("want TaskVerifying kept, got %q", task.Status)
		}
		if task.SessionID != "sess-worker-ended" {
			t.Fatalf("want SessionID kept, got %q", task.SessionID)
		}
		if killed != 0 {
			t.Fatalf("a finished worker's session must not be killed, killed=%d", killed)
		}
	})

	t.Run("unfinished worker still resets", func(t *testing.T) {
		m, prd := verifyingBootManager(t, []Story{
			{Title: "story", Tasks: []Task{
				{Title: "task", Status: TaskVerifying, SessionID: "sess-dead"},
			}},
		})
		m.SetSessionAliveFn(func(string) bool { return false })
		m.SetSessionOutputFn(func(string) (string, bool) { return "still working", true })
		m.reconcileStuckTasks()

		got, _ := m.Store().GetPRD(prd.ID)
		if got.Story[0].Tasks[0].Status != TaskPending {
			t.Fatalf("want TaskPending, got %q", got.Story[0].Tasks[0].Status)
		}
	})
}
