package autonomous

import (
	"testing"
)

// Boot-resume of a verifying task when the daemon restarts: instead of
// resetting the worker, verification resumes from whatever survived:
//
//   - live verifier session (VerifierSessionID) still judging the task ->
//     attach a fresh waiter to that session (no second verifier spawn, the
//     worker is not reset, no retry is counted) — VerifierResume case 1
//   - verifier ended, but a parseable verdict result file remains ->
//     consume that verdict as the outcome (pass completes, fail is recorded
//     and requeued) — VerifierResume case 2
//   - neither -> the task is kept verifying for the executor, which starts
//     the verification once without re-running the finished worker and
//     without counting a retry — VerifierResume case 3
//
// Both boot paths are covered: resetInProgressTasksForResume (resume) and
// reconcileStuckTasks (daemon start), plus the daemon waiter's public
// consume entry ConsumeVerifierResult.

type verifierResumeCall struct {
	prdID     string
	taskID    string
	sessionID string
}

func TestVerifierResumeReattach(t *testing.T) {
	t.Run("reset-resume attaches waiter to live verifier session", func(t *testing.T) {
		m, prd := verifyingBootManager(t, []Story{
			{Title: "story", Tasks: []Task{
				{Title: "task", Status: TaskVerifying,
					SessionID: "worker-dead", VerifierSessionID: "verifier-live",
					AttemptCommitSHAs: []string{"abc123"},
					Attempts:          []TaskAttempt{{Number: 1, Outcome: "failed"}},
				},
			}},
		})
		tid := prd.Story[0].Tasks[0].ID
		var reattachCalls []verifierResumeCall
		m.SetVerifierReattachFn(func(prdID, taskID, sessionID string) {
			reattachCalls = append(reattachCalls, verifierResumeCall{prdID, taskID, sessionID})
		})
		m.SetVerifierResultReader(func(prdID, taskID string) (*VerificationResult, bool) { return nil, false })
		m.SetSessionAliveFn(func(id string) bool { return id == "verifier-live" })

		m.resetInProgressTasksForResume(prd)

		got, _ := m.Store().GetPRD(prd.ID)
		task := got.Story[0].Tasks[0]
		if len(reattachCalls) != 1 {
			t.Fatalf("want exactly one waiter attach, got %d: %+v", len(reattachCalls), reattachCalls)
		}
		c := reattachCalls[0]
		if c.prdID != prd.ID || c.taskID != tid || c.sessionID != "verifier-live" {
			t.Fatalf("waiter attached to wrong target: %+v (want prd=%s task=%s session=verifier-live)", c, prd.ID, tid)
		}
		// Worker is not reset and no retry is counted around the live verifier.
		if task.Status != TaskVerifying {
			t.Fatalf("want task kept TaskVerifying with its live verifier, got %q", task.Status)
		}
		if task.SessionID != "worker-dead" {
			t.Fatalf("worker session must stay recorded, got %q", task.SessionID)
		}
		if task.VerifierSessionID != "verifier-live" {
			t.Fatalf("verifier session link must stay, got %q", task.VerifierSessionID)
		}
		if task.CompletedAt != nil {
			t.Fatalf("pending verdict: task must not complete, CompletedAt=%v", *task.CompletedAt)
		}
		if task.Error != "" {
			t.Fatalf("no failure may be recorded, got %q", task.Error)
		}
		if len(task.Attempts) != 1 {
			t.Fatalf("no retry counted: attempt history must be untouched, got %d", len(task.Attempts))
		}
	})

	t.Run("daemon-start attaches waiter to live verifier session", func(t *testing.T) {
		m, prd := verifyingBootManager(t, []Story{
			{Title: "story", Tasks: []Task{
				{Title: "task", Status: TaskVerifying,
					SessionID: "worker-dead", VerifierSessionID: "verifier-live",
					AttemptCommitSHAs: []string{"abc123"},
				},
			}},
		})
		var reattachCalls []verifierResumeCall
		m.SetVerifierReattachFn(func(prdID, taskID, sessionID string) {
			reattachCalls = append(reattachCalls, verifierResumeCall{prdID, taskID, sessionID})
		})
		m.SetVerifierResultReader(func(prdID, taskID string) (*VerificationResult, bool) { return nil, false })
		m.SetSessionAliveFn(func(id string) bool { return id == "verifier-live" })

		m.reconcileStuckTasks()

		got, _ := m.Store().GetPRD(prd.ID)
		task := got.Story[0].Tasks[0]
		if len(reattachCalls) != 1 || reattachCalls[0].sessionID != "verifier-live" {
			t.Fatalf("want one waiter attach on verifier-live, got %+v", reattachCalls)
		}
		if task.Status != TaskVerifying || task.SessionID != "worker-dead" || task.VerifierSessionID != "verifier-live" {
			t.Fatalf("worker must not be reset on daemon start: %+v", task)
		}
	})

	t.Run("no waiter wired leaves task verifying without resetting", func(t *testing.T) {
		m, prd := verifyingBootManager(t, []Story{
			{Title: "story", Tasks: []Task{
				{Title: "task", Status: TaskVerifying,
					SessionID: "worker-dead", VerifierSessionID: "verifier-live",
					AttemptCommitSHAs: []string{"abc123"},
				},
			}},
		})
		m.SetVerifierResultReader(func(prdID, taskID string) (*VerificationResult, bool) { return nil, false })
		m.SetSessionAliveFn(func(id string) bool { return id == "verifier-live" })

		// No SetVerifierReattachFn: the waiter is not wired.
		m.resetInProgressTasksForResume(prd)

		got, _ := m.Store().GetPRD(prd.ID)
		task := got.Story[0].Tasks[0]
		if task.Status != TaskVerifying || task.SessionID != "worker-dead" {
			t.Fatalf("without a wired waiter the task still stays verifying, not reset: %+v", task)
		}
	})
}

func TestVerifierResumeConsume(t *testing.T) {
	t.Run("ok verdict file completes the task", func(t *testing.T) {
		m, prd := verifyingBootManager(t, []Story{
			{Title: "story", Tasks: []Task{
				{Title: "task", Status: TaskVerifying,
					SessionID: "worker-dead", VerifierSessionID: "verifier-dead",
					AttemptCommitSHAs: []string{"abc123"},
				},
			}},
		})
		var reattachCalls []verifierResumeCall
		m.SetVerifierReattachFn(func(prdID, taskID, sessionID string) {
			reattachCalls = append(reattachCalls, verifierResumeCall{prdID, taskID, sessionID})
		})
		m.SetVerifierResultReader(func(prdID, taskID string) (*VerificationResult, bool) {
			return &VerificationResult{OK: true, Summary: "checks pass", VerifierID: "ver-2"}, true
		})
		m.SetSessionAliveFn(func(string) bool { return false })

		m.resetInProgressTasksForResume(prd)

		got, _ := m.Store().GetPRD(prd.ID)
		task := got.Story[0].Tasks[0]
		if len(reattachCalls) != 0 {
			t.Fatalf("verifier session is dead: no waiter attach expected, got %+v", reattachCalls)
		}
		if task.Status != TaskCompleted {
			t.Fatalf("want TaskCompleted from the consumed ok verdict, got %q", task.Status)
		}
		if task.CompletedAt == nil {
			t.Fatal("want CompletedAt set")
		}
		if task.Error != "" {
			t.Fatalf("no error expected, got %q", task.Error)
		}
		if task.Verification == nil || !task.Verification.OK || task.Verification.Summary != "checks pass" || task.Verification.VerifierID != "ver-2" {
			t.Fatalf("consumed verdict must be recorded intact, got %+v", task.Verification)
		}
	})

	t.Run("failed verdict file is recorded and requeued for retry", func(t *testing.T) {
		m, prd := verifyingBootManager(t, []Story{
			{Title: "story", Tasks: []Task{
				{Title: "task", Status: TaskVerifying,
					SessionID: "worker-dead", VerifierSessionID: "verifier-dead",
					AttemptCommitSHAs: []string{"abc123"},
				},
			}},
		})
		var reattachCalls []verifierResumeCall
		m.SetVerifierReattachFn(func(prdID, taskID, sessionID string) {
			reattachCalls = append(reattachCalls, verifierResumeCall{prdID, taskID, sessionID})
		})
		m.SetVerifierResultReader(func(prdID, taskID string) (*VerificationResult, bool) {
			return &VerificationResult{OK: false, Summary: "broken", Issues: []string{"boom"}}, true
		})
		m.SetSessionAliveFn(func(string) bool { return false })

		m.resetInProgressTasksForResume(prd)

		got, _ := m.Store().GetPRD(prd.ID)
		task := got.Story[0].Tasks[0]
		if len(reattachCalls) != 0 {
			t.Fatalf("verifier session is dead: no waiter attach expected, got %+v", reattachCalls)
		}
		if task.Status != TaskPending {
			t.Fatalf("want TaskPending for the normal retry path, got %q", task.Status)
		}
		if task.SessionID != "" {
			t.Fatalf("orphaned session cleared, got %q", task.SessionID)
		}
		if task.Verification == nil || task.Verification.OK ||
			task.Verification.Summary != "broken" || len(task.Verification.Issues) != 1 || task.Verification.Issues[0] != "boom" {
			t.Fatalf("consumed failed verdict must be recorded for the retry path, got %+v", task.Verification)
		}
	})

	t.Run("daemon-start consumes ok verdict file", func(t *testing.T) {
		m, prd := verifyingBootManager(t, []Story{
			{Title: "story", Tasks: []Task{
				{Title: "task", Status: TaskVerifying,
					SessionID: "worker-dead", VerifierSessionID: "verifier-dead",
					AttemptCommitSHAs: []string{"abc123"},
				},
			}},
		})
		m.SetVerifierResultReader(func(prdID, taskID string) (*VerificationResult, bool) {
			return &VerificationResult{OK: true, Summary: "checks pass"}, true
		})
		m.SetSessionAliveFn(func(string) bool { return false })

		m.reconcileStuckTasks()

		got, _ := m.Store().GetPRD(prd.ID)
		if got.Story[0].Tasks[0].Status != TaskCompleted {
			t.Fatalf("want TaskCompleted from the consumed ok verdict, got %q", got.Story[0].Tasks[0].Status)
		}
	})
}

func TestVerifierResumeStartsVerificationOnce(t *testing.T) {
	// Neither a live verifier session nor a verdict file survived the restart:
	// the finished worker's result stands, so boot keeps the task verifying
	// for the executor, which starts exactly one verification — no second
	// worker run, no waiter attached, no retry counted.
	t.Run("reset-resume keeps finished worker for one verification", func(t *testing.T) {
		m, prd := verifyingBootManager(t, []Story{
			{Title: "story", Tasks: []Task{
				{Title: "task", Status: TaskVerifying,
					SessionID:         "worker-dead",
					AttemptCommitSHAs: []string{"abc123"},
					Attempts:          []TaskAttempt{{Number: 1, Outcome: "failed"}},
				},
			}},
		})
		var reattachCalls []verifierResumeCall
		m.SetVerifierReattachFn(func(prdID, taskID, sessionID string) {
			reattachCalls = append(reattachCalls, verifierResumeCall{prdID, taskID, sessionID})
		})
		m.SetVerifierResultReader(func(prdID, taskID string) (*VerificationResult, bool) { return nil, false })
		m.SetSessionAliveFn(func(string) bool { return false })

		m.resetInProgressTasksForResume(prd)

		got, _ := m.Store().GetPRD(prd.ID)
		task := got.Story[0].Tasks[0]
		if len(reattachCalls) != 0 {
			t.Fatalf("no live verifier session: no waiter attach expected, got %+v", reattachCalls)
		}
		if task.Status != TaskVerifying {
			t.Fatalf("worker result stands: task must stay TaskVerifying for one verification, got %q", task.Status)
		}
		if task.SessionID != "worker-dead" {
			t.Fatalf("worker result must stay recorded, got %q", task.SessionID)
		}
		if len(task.Attempts) != 1 {
			t.Fatalf("no retry counted: attempt history must be untouched, got %d", len(task.Attempts))
		}
		if task.Error != "" {
			t.Fatalf("no failure may be recorded, got %q", task.Error)
		}
	})

	t.Run("daemon-start keeps finished worker for one verification", func(t *testing.T) {
		m, prd := verifyingBootManager(t, []Story{
			{Title: "story", Tasks: []Task{
				{Title: "task", Status: TaskVerifying,
					SessionID:         "worker-dead",
					AttemptCommitSHAs: []string{"abc123"},
				},
			}},
		})
		m.SetVerifierResultReader(func(prdID, taskID string) (*VerificationResult, bool) { return nil, false })
		m.SetSessionAliveFn(func(string) bool { return false })

		m.reconcileStuckTasks()

		got, _ := m.Store().GetPRD(prd.ID)
		if got.Story[0].Tasks[0].Status != TaskVerifying {
			t.Fatalf("want TaskVerifying for one verification, got %q", got.Story[0].Tasks[0].Status)
		}
	})
}

func TestVerifierResumeConsumeVerifierResult(t *testing.T) {
	// The daemon waiter's public consume entry (wired through
	// SetVerifierReattachFn): it applies the verdict of a surviving verifier
	// session and is idempotent.
	t.Run("pass completes the task", func(t *testing.T) {
		m, prd := verifyingBootManager(t, []Story{
			{Title: "story", Tasks: []Task{
				{Title: "task", Status: TaskVerifying,
					SessionID: "worker-dead", VerifierSessionID: "verifier-live",
					AttemptCommitSHAs: []string{"abc123"},
				},
			}},
		})
		applied := m.ConsumeVerifierResult(prd.ID, prd.Story[0].Tasks[0].ID, VerificationResult{OK: true, Summary: "pass", VerifierID: "ver-9"})
		if !applied {
			t.Fatal("want the verdict applied to a verifying task")
		}
		got, _ := m.Store().GetPRD(prd.ID)
		task := got.Story[0].Tasks[0]
		if task.Status != TaskCompleted || task.Verification == nil || !task.Verification.OK || task.Verification.VerifierID != "ver-9" {
			t.Fatalf("want completed with the consumed verdict intact, got %+v", task)
		}
		// Second consume after completion is a no-op.
		if m.ConsumeVerifierResult(prd.ID, task.ID, VerificationResult{OK: false, Summary: "late fail"}) {
			t.Fatal("second consume must be a no-op")
		}
	})

	t.Run("fail is recorded and requeued exactly once", func(t *testing.T) {
		m, prd := verifyingBootManager(t, []Story{
			{Title: "story", Tasks: []Task{
				{Title: "task", Status: TaskVerifying,
					SessionID: "worker-dead", VerifierSessionID: "verifier-live",
					AttemptCommitSHAs: []string{"abc123"},
				},
			}},
		})
		tid := prd.Story[0].Tasks[0].ID
		applied := m.ConsumeVerifierResult(prd.ID, tid, VerificationResult{OK: false, Summary: "broken", Issues: []string{"boom"}})
		if !applied {
			t.Fatal("want the verdict applied to a verifying task")
		}
		got, _ := m.Store().GetPRD(prd.ID)
		task := got.Story[0].Tasks[0]
		if task.Status != TaskPending {
			t.Fatalf("want TaskPending for the normal retry path, got %q", task.Status)
		}
		if task.Verification == nil || task.Verification.OK {
			t.Fatalf("failed verdict must be recorded, got %+v", task.Verification)
		}
		if m.ConsumeVerifierResult(prd.ID, tid, VerificationResult{OK: true, Summary: "late pass"}) {
			t.Fatal("second consume must be a no-op once the task left verifying")
		}
		got, _ = m.Store().GetPRD(prd.ID)
		if got.Story[0].Tasks[0].Status != TaskPending {
			t.Fatalf("late verdict must not flip a requeued task, got %q", got.Story[0].Tasks[0].Status)
		}
	})
}
