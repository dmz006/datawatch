// Boot-resume of a task found in TaskVerifying after a daemon restart:
//
//   - a finished worker (dead session with a real completion line and/or a
//     recorded worker result) and no recorded verdict stays verifying, keeps its
//     session, SHAs and worker result, counts no retry, ends no session, and is
//     verified exactly once without a new worker being spawned;
//   - a verifying task whose worker did not finish is reset to pending.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous -run TestVerifyingFinishedWorker
package autonomous

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
)

// setupVerifyingBooted is setupBootedAdoptState with the task moved to
// TaskVerifying, carrying base and pre-task SHAs and no recorded verification.
func setupVerifyingBooted(t *testing.T, m *Manager, sessionID string, workerResult []string) *PRD {
	t.Helper()
	cur := setupBootedAdoptState(t, m, sessionID)
	tk := &cur.Story[0].Tasks[0]
	tk.Status = TaskVerifying
	tk.BaseSHA = "1111111111111111111111111111111111111111"
	tk.PreTaskSHA = "2222222222222222222222222222222222222222"
	tk.RetryCount = 0
	tk.Verification = nil
	tk.AttemptCommitSHAs = workerResult
	if err := m.Store().SaveTask(tk); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	cur, _ = m.Store().GetPRD(cur.ID)
	return cur
}

func TestVerifyingFinishedWorker_KeptAndVerifiedOnce(t *testing.T) {
	cases := []struct {
		name         string
		output       string
		workerResult []string
	}{
		{"completion line", claudeCodeEchoedTask, nil},
		{"worker result", "", []string{"3333333333333333333333333333333333333333"}},
		{"both", claudeCodeEchoedTask, []string{"3333333333333333333333333333333333333333"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, api, _, verify := apiFixture(t)
			fs := newFakeSessions() // worker session is gone
			if tc.output != "" {
				fs.recordOutput("vf-done", tc.output)
			}
			fs.wire(m)

			cur := setupVerifyingBooted(t, m, "vf-done", tc.workerResult)
			before := cur.Story[0].Tasks[0]

			m.resetInProgressTasksForResume(cur)

			got, _ := m.Store().GetPRD(cur.ID)
			tk := got.Story[0].Tasks[0]
			if tk.Status != TaskVerifying {
				t.Fatalf("finished worker must stay verifying, got %s", tk.Status)
			}
			if tk.SessionID != before.SessionID || tk.BaseSHA != before.BaseSHA || tk.PreTaskSHA != before.PreTaskSHA {
				t.Fatalf("session/SHAs changed: session=%q base=%q pre=%q", tk.SessionID, tk.BaseSHA, tk.PreTaskSHA)
			}
			if !reflect.DeepEqual(tk.AttemptCommitSHAs, before.AttemptCommitSHAs) {
				t.Fatalf("worker result changed: %v -> %v", before.AttemptCommitSHAs, tk.AttemptCommitSHAs)
			}
			if tk.RetryCount != before.RetryCount || tk.PriorAttemptCount != before.PriorAttemptCount {
				t.Fatalf("retry counted: RetryCount %d->%d PriorAttemptCount %d->%d",
					before.RetryCount, tk.RetryCount, before.PriorAttemptCount, tk.PriorAttemptCount)
			}
			if n := fs.killCount(); n != 0 {
				t.Fatalf("session ended on boot-resume: %v", fs.killed())
			}

			var spawnN, verifyN int32
			noSpawn := func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
				atomic.AddInt32(&spawnN, 1)
				return SpawnResult{}, fmt.Errorf("must not spawn: the worker already finished")
			}
			counting := func(ctx context.Context, p *PRD, k *Task) (VerificationResult, error) {
				atomic.AddInt32(&verifyN, 1)
				return verify(ctx, p, k)
			}
			out := runToTerminal(t, m, api, cur.ID, noSpawn, counting)
			if out.Status != PRDCompleted {
				t.Fatalf("want PRDCompleted, got %s (task error: %q)", out.Status, out.Story[0].Tasks[0].Error)
			}
			if n := atomic.LoadInt32(&spawnN); n != 0 {
				t.Fatalf("spawned %d worker(s), want 0", n)
			}
			if n := atomic.LoadInt32(&verifyN); n != 1 {
				t.Fatalf("verification ran %d times, want 1", n)
			}
		})
	}
}

func TestVerifyingFinishedWorker_UnfinishedWorkerIsReset(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	fs := newFakeSessions() // dead session
	fs.recordOutput("vf-unfinished", "> Implement the widget.\n● Working on it...\n")
	fs.wire(m)

	cur := setupVerifyingBooted(t, m, "vf-unfinished", nil)
	m.resetInProgressTasksForResume(cur)

	got, _ := m.Store().GetPRD(cur.ID)
	if st := got.Story[0].Tasks[0].Status; st != TaskPending {
		t.Fatalf("unfinished verifying task must be reset; status=%s, want pending", st)
	}
}
