package autonomous

import "testing"

// TestVerifierResumedAtBoot — a task in verifying whose verifier session
// survived the daemon restart is not reset at boot: the manager resumes that
// same verifier session (one fresh waiter attached — no second verifier
// spawn), the worker is not re-run and no retry is counted, and it is the
// resumed verifier's verdict that completes the task when the verifier ends.
//
// Both boot paths are exercised: resetInProgressTasksForResume (resume) and
// reconcileStuckTasks (daemon start). The fake verifier is the surviving
// session itself: when it ends, its verdict arrives through the same public
// consume entry (ConsumeVerifierResult) the daemon waiter wires through
// SetVerifierReattachFn.

func TestVerifierResumedAtBoot(t *testing.T) {
	paths := []struct {
		name string
		boot func(*Manager, *PRD)
	}{
		{"resume", func(m *Manager, prd *PRD) { m.resetInProgressTasksForResume(prd) }},
		{"daemon-start", func(m *Manager, prd *PRD) { m.reconcileStuckTasks() }},
	}
	for _, p := range paths {
		t.Run(p.name, func(t *testing.T) {
			m, prd := verifyingBootManager(t, []Story{
				{Title: "story", Tasks: []Task{
					{Title: "task", Status: TaskVerifying,
						SessionID:         "worker-dead",
						VerifierSessionID: "verifier-resumed",
						AttemptCommitSHAs: []string{"abc123"},
						Attempts:          []TaskAttempt{{Number: 1, Outcome: "failed"}},
					},
				}},
			})
			tid := prd.Story[0].Tasks[0].ID

			// The fake waiter boot attaches to the surviving verifier session:
			// it records the attach and, when the fake verifier ends, delivers
			// its verdict through the daemon waiter's public entry.
			var attached []verifierResumeCall
			var verifierEnds func(VerificationResult) bool
			m.SetVerifierReattachFn(func(prdID, taskID, sessionID string) {
				attached = append(attached, verifierResumeCall{prdID, taskID, sessionID})
				verifierEnds = func(v VerificationResult) bool {
					return m.ConsumeVerifierResult(prdID, taskID, v)
				}
			})
			// No verdict file survived: the live session is what survived.
			m.SetVerifierResultReader(func(string, string) (*VerificationResult, bool) { return nil, false })
			m.SetSessionAliveFn(func(id string) bool { return id == "verifier-resumed" })
			killed := []string{}
			m.SetSessionKillerFn(func(sessionID, caller, reason string) error {
				killed = append(killed, sessionID)
				return nil
			})

			p.boot(m, prd)

			got, _ := m.Store().GetPRD(prd.ID)
			task := got.Story[0].Tasks[0]
			// Boot resumed the surviving verifier instead of resetting the
			// worker: exactly one waiter attach — no second verifier was
			// spawned around it.
			if verifierEnds == nil {
				t.Fatal("want boot to attach a waiter to the surviving verifier session")
			}
			if len(attached) != 1 {
				t.Fatalf("want exactly one waiter attach (no second verifier), got %d: %+v", len(attached), attached)
			}
			c := attached[0]
			if c.prdID != prd.ID || c.taskID != tid || c.sessionID != "verifier-resumed" {
				t.Fatalf("waiter attached to the wrong target: %+v (want prd=%s task=%s session=verifier-resumed)", c, prd.ID, tid)
			}
			if task.Status != TaskVerifying {
				t.Fatalf("task must not be reset while its verifier is live, got %q", task.Status)
			}
			if task.SessionID != "worker-dead" {
				t.Fatalf("worker result must stay recorded, got %q", task.SessionID)
			}
			if task.VerifierSessionID != "verifier-resumed" {
				t.Fatalf("verifier session link must stay, got %q", task.VerifierSessionID)
			}
			if len(task.Attempts) != 1 {
				t.Fatalf("worker not re-run, no retry counted: attempts must be untouched, got %d", len(task.Attempts))
			}
			if task.Error != "" || task.CompletedAt != nil {
				t.Fatalf("no outcome may be recorded while the verdict is pending: %+v", task)
			}
			if len(killed) != 0 {
				t.Fatalf("no session may be killed while its verifier is live, killed=%v", killed)
			}

			// The fake verifier ends: its verdict completes the task.
			if !verifierEnds(VerificationResult{OK: true, Summary: "checks pass", VerifierID: "verifier-resumed"}) {
				t.Fatal("want the resumed verifier's verdict to apply")
			}
			got, _ = m.Store().GetPRD(prd.ID)
			task = got.Story[0].Tasks[0]
			if task.Status != TaskCompleted {
				t.Fatalf("the resumed verifier's verdict must complete the task, got %q", task.Status)
			}
			if task.CompletedAt == nil {
				t.Fatal("want CompletedAt set")
			}
			if task.Verification == nil || !task.Verification.OK ||
				task.Verification.Summary != "checks pass" || task.Verification.VerifierID != "verifier-resumed" {
				t.Fatalf("the verdict must be the surviving verifier's, not a second verifier's, got %+v", task.Verification)
			}
			// After the verdict lands, only the worker-session cleanup is allowed
			// — the surviving verifier session is never killed.
			for _, id := range killed {
				if id == "verifier-resumed" {
					t.Fatalf("the surviving verifier session must never be killed, killed=%v", killed)
				}
			}
			if len(attached) != 1 || len(task.Attempts) != 1 {
				t.Fatalf("nothing extra may happen once the verdict lands: attached=%d attempts=%d",
					len(attached), len(task.Attempts))
			}
		})
	}
}
