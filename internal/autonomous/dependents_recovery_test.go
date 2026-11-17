// Behavior tests for the unconditional dependent recovery: when the
// dependency is no longer failing, the dependents that failed behind it —
// for any reason, not only infrastructure — return to pending, and a
// dependent that failed on its own work stays failed.
//
// They drive the real API through the same dependent-rig harness as the
// infrastructure path tests (newDepRig in infra_retry_dependents_test.go):
// a root seeded failed, dependents behind it, one task per story so no
// story can finish.

package autonomous

import (
	"strings"
	"testing"
)

// TestDependentsReturnToPendingWhenDependencyCompletes — the dependent
// requeue is unconditional: the root failed for a non-infrastructure
// (content) reason only, so no retry policy applies at all; the operator
// resets it, its dependents return to pending in the same step, and the
// reopened run completes.
func TestDependentsReturnToPendingWhenDependencyCompletes(t *testing.T) {
	for _, mode := range depPathModes {
		t.Run(mode.name, func(t *testing.T) {
			// The root fails for a content reason — its dependents are marked
			// failed behind it with the "dependency <id> failed" text and no
			// failure reason of their own.
			r := newDepRig(t, mode.concurrency, nil, failedWith(contentRootReason()))
			assertNotMarked(t, r.task(t, depChildID), depRootID, "failed")
			assertNotMarked(t, r.task(t, depGrandID), depChildID, "failed")

			// Give the child an attempt history and a verification record the
			// recovery must not clear (attempts) / must clear (verification).
			r.edit(t, depChildID, func(tk *Task) {
				tk.Attempts = []TaskAttempt{{}}
				tk.Verification = &VerificationResult{Summary: "an earlier run"}
			})

			// The operator retries the root. The recovery runs inside the reset,
			// in the same step — no manual second run is involved.
			if _, err := r.api.ResetTask(r.prdID, depRootID, "operator", false); err != nil {
				t.Fatalf("ResetTask: %v", err)
			}

			// Both dependents are back at pending with their dependency error
			// cleared, their reason nil, and the child's verification gone —
			// but the child's attempt history kept.
			for _, id := range []string{depChildID, depGrandID} {
				tk := r.task(t, id)
				if normTaskStatus(tk.Status) != TaskPending {
					t.Errorf("task %s status = %q, want pending after the reset (error %q)", id, tk.Status, tk.Error)
				}
				if tk.Error != "" {
					t.Errorf("task %s error = %q, want it cleared", id, tk.Error)
				}
				if tk.FailureReason != nil {
					t.Errorf("task %s failure reason = %+v, want none", id, tk.FailureReason)
				}
				if tk.Verification != nil {
					t.Errorf("task %s verification = %+v, want it cleared", id, tk.Verification)
				}
			}
			if child := r.task(t, depChildID); len(child.Attempts) != 1 {
				t.Errorf("child attempts = %d, want the previous attempt history kept", len(child.Attempts))
			}

			// The Automaton was failed; the reset reopened it, so it is running
			// now (or the reopened run has already completed) — never failed
			// again.
			if st := r.prd(t).Status; st != PRDRunning && st != PRDCompleted {
				t.Fatalf("automaton status = %q, want running — the reset reopens the failed run", st)
			}

			// One dependent decision for the content root: it names the root
			// and every released task. The infrastructure kind stays at zero.
			got := drDecisions(t, r.m, r.prdID, dependentsRecoveredKind)
			if len(got) != 1 {
				t.Fatalf("%s decisions = %d (of %d total), want 1", dependentsRecoveredKind, len(got), len(drDecisions(t, r.m, r.prdID, "")))
			} else if !strings.Contains(got[0].Note, "root="+depRootID) ||
				!strings.Contains(got[0].Note, depChildID) || !strings.Contains(got[0].Note, depGrandID) {
				t.Errorf("decision note = %q, want the root and both released tasks", got[0].Note)
			}
			if got := drDecisions(t, r.m, r.prdID, infraDependentsRecoveredKind); len(got) != 0 {
				t.Errorf("infra decisions = %d, want none for a content root", len(got))
			}

			// The reopened run recovers the whole chain to completed.
			waitForPRDTerminal(t, r.m, r.prdID)
			r.api.execWG.Wait()
			for _, id := range []string{depRootID, depChildID, depGrandID} {
				if tk := r.task(t, id); tk.Status != TaskCompleted {
					t.Errorf("task %s status = %q, want completed (error %q)", id, tk.Status, tk.Error)
				}
			}
			if st := r.prd(t).Status; st != PRDCompleted {
				t.Errorf("automaton status = %q, want completed", st)
			}
		})
	}
}

// TestDependentWithOwnFailureStaysFailed — the recovery never re-queues a
// dependent that carried its own failure: the chain root stays failed, the
// one behind it is left untouched, and no decision names them.
func TestDependentWithOwnFailureStaysFailed(t *testing.T) {
	for _, mode := range depPathModes {
		t.Run(mode.name, func(t *testing.T) {
			r := newDepRig(t, mode.concurrency, nil, failedWith(contentRootReason()))
			assertNotMarked(t, r.task(t, depChildID), depRootID, "failed")
			assertNotMarked(t, r.task(t, depGrandID), depChildID, "failed")

			// The child fails on its own work behind the root — its content
			// failure, not the root's.
			own := ownFailureReason()
			r.edit(t, depChildID, func(tk *Task) {
				tk.Error = "the work is incomplete"
				tk.FailureReason = &own
			})

			// The operator retries the root. Nothing is released: the child
			// carries a failure of its own, and the grandchild is still behind
			// a failed chain root.
			if _, err := r.api.ResetTask(r.prdID, depRootID, "operator", false); err != nil {
				t.Fatalf("ResetTask: %v", err)
			}

			child := r.task(t, depChildID)
			if child.Status != TaskFailed {
				t.Fatalf("child status = %q, want failed — the recovery leaves its own failure alone", child.Status)
			}
			if child.Error != "the work is incomplete" {
				t.Errorf("child error = %q, want its own failure text kept", child.Error)
			}
			if fr := child.FailureReason; fr == nil || fr.Category != "content" || fr.Detail != "the verification failed on the task's own work" {
				t.Errorf("child failure reason = %+v, want its own content reason kept", fr)
			}
			grand := r.task(t, depGrandID)
			if grand.Status != TaskFailed {
				t.Fatalf("grandchild status = %q, want failed — its chain root is still failing", grand.Status)
			}
			if want := "dependency " + depChildID + " failed"; grand.Error != want {
				t.Errorf("grandchild error = %q, want %q — left untouched", grand.Error, want)
			}
			if grand.FailureReason != nil {
				t.Errorf("grandchild failure reason = %+v, want none — left untouched", grand.FailureReason)
			}
			if got := drDecisions(t, r.m, r.prdID, dependentsRecoveredKind); len(got) != 0 {
				t.Errorf("dependents decisions = %d, want none — the recovery touched nothing", len(got))
			}
			if got := drDecisions(t, r.m, r.prdID, infraDependentsRecoveredKind); len(got) != 0 {
				t.Errorf("infra decisions = %d, want none", len(got))
			}

			// The root re-runs to completion on the reopened run; its dependents'
			// own failures stand.
			if err := r.api.Run(r.prdID); err != nil {
				t.Fatalf("Run: %v", err)
			}
			waitForPRDTerminal(t, r.m, r.prdID)
			r.api.execWG.Wait()

			if root := r.task(t, depRootID); root.Status != TaskCompleted {
				t.Errorf("root status = %q, want completed (error %q)", root.Status, root.Error)
			}
			child = r.task(t, depChildID)
			if child.Status != TaskFailed || child.Error != "the work is incomplete" {
				t.Errorf("child = status %q error %q, want failed on its own failure", child.Status, child.Error)
			}
			if fr := child.FailureReason; fr == nil || fr.Category != "content" {
				t.Errorf("child failure reason = %+v, want its own content reason", fr)
			}
			if grand := r.task(t, depGrandID); grand.Status != TaskFailed {
				t.Errorf("grandchild status = %q, want still failed behind the child", grand.Status)
			}
			if st := r.prd(t).Status; st == PRDCompleted {
				t.Errorf("automaton status = %q, want not completed — the child's own failure stands", st)
			}
		})
	}
}
