// protected_paths_false_hold_release.go — releasing false-positive
// protected-paths holds.
//
// A task whose commit touched protected paths where the whole diff against
// the base commit is the daemon's own injected sections (platform context,
// RTK, cross-session, Memory & Knowledge, Session Guardrails) did not edit
// the operator's rules: the hold is a false positive, and waiting on the
// operator to approve the daemon's own injection parks the task for nothing.
//
// ReleaseFalseProtectedPathHolds re-checks every held task once at daemon
// start (the previous instance's question goroutine is gone, so the open
// question will not be re-answered) and releases the ones whose held paths
// are all daemon-only diffs. A release needs positive proof: an unanswered
// hold, the tree the task worked in, and for every held path a diff that is
// shown to hold daemon sections and nothing else (pathsNotProvenDaemonOnly).
// Whatever cannot be shown keeps the hold.
package autonomous

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// protectedPathsFalsePositiveDecisionKind is the Decision kind that records
// a false-positive protected-paths hold release.
const protectedPathsFalsePositiveDecisionKind = "protected_paths_false_positive_released"

// protectedPathBlockedErrPrefix is the Task.Error prefix a protected-paths
// block (blockProtectedPaths) writes.
const protectedPathBlockedErrPrefix = "protected_path_blocked: "

// ReleaseFalseProtectedPathHolds re-checks every task held on a protected
// path and releases the ones held on false positives, returning their task
// ids (in Automaton order).
//
// A task is a candidate when its status is TaskBlocked and it carries a
// ProtectedPathHold, or a Task.Error starting with "protected_path_blocked:
// ". It stays held when the operator already answered its question, when the
// question is no longer open for any other reason, and when the Automaton
// works on its own branch and the task records no directory of its own (its
// task worktree may have been removed, and the Automaton worktree does not
// hold the task's unmerged change). Otherwise its held paths are re-checked
// with pathsNotProvenDaemonOnly against the task's directory and the stored
// base commit — the commit gate's fails-closed check, which here also keeps
// an unchanged path. When every held path is a daemon-section-only diff, the hold is a false positive and
// the task is released: the open protected-paths question is withdrawn
// without raising a new one, the hold
// and the error are cleared, a protected_paths_false_positive_released
// decision naming the task and the paths is recorded, the task is returned
// to the queue the way reset_task does without force (nothing is wiped), and
// the Automaton is saved. When at least one path has any other diff, no diff,
// or a diff that cannot be read, or the question cannot be withdrawn, the
// task is left alone, still held.
//
// It is called once from the daemon startup path, after a restart: the
// previous instance's question goroutine is gone, so no question is
// re-answered, and the sweep is the only path that releases such a hold
// without operator action.
func (m *Manager) ReleaseFalseProtectedPathHolds(ctx context.Context) []string {
	if m == nil || m.store == nil {
		return nil
	}
	released := []string{}
	for _, listed := range m.store.ListPRDs() {
		if ctx.Err() != nil {
			return released
		}
		prd, ok := m.store.SnapshotPRD(listed.ID)
		if !ok || prd.Status == PRDHeld || closedRecordReason(prd) != "" {
			continue // a held or closed Automaton's tasks are never released
		}
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				if ctx.Err() != nil {
					return released
				}
				t := &prd.Story[si].Tasks[ti]
				if m.releaseFalseProtectedPathHold(ctx, prd, t) {
					released = append(released, t.ID)
				}
			}
		}
	}
	return released
}

// releaseFalseProtectedPathHold inspects one candidate task; prd and t are
// the snapshot row that identifies it. It returns true only when it actually
// released the task (question withdrawn, hold and error cleared, decision
// recorded, task back in the queue, Automaton saved).
func (m *Manager) releaseFalseProtectedPathHold(ctx context.Context, prd *PRD, t *Task) bool {
	if t.Status != TaskBlocked {
		return false
	}
	hold := t.ProtectedPathHold
	if hold == nil {
		if strings.HasPrefix(t.Error, protectedPathBlockedErrPrefix) {
			// A block-action hold stores no base commit and no question: the
			// re-check cannot run against a stored base commit, and an
			// unreadable base leaves the task alone.
			log.Printf("[autonomous] prd=%s task=%s: protected-paths hold stores no base commit; leaving it held", prd.ID, t.ID)
		}
		return false
	}
	if len(hold.Paths) == 0 {
		log.Printf("[autonomous] prd=%s task=%s: protected-paths hold records no paths; leaving it held", prd.ID, t.ID)
		return false
	}
	// An answer is the operator's decision, whatever it was: only a hold that
	// still waits for one can turn out to be a false positive.
	if hold.AwaitingSpecChange || (hold.QuestionID != "" && !m.openUncommittedQuestion(hold.QuestionID)) {
		log.Printf("[autonomous] prd=%s task=%s: protected-paths hold question %s was answered or is no longer open; leaving it held", prd.ID, t.ID, hold.QuestionID)
		return false
	}
	// On its own branch, a task with no directory of its own may have worked
	// in a task worktree that is gone. The directory it would fall back to
	// never received the held change, so reading it proves nothing.
	if prd.Git.Branch != "" && t.ProjectDir == "" && t.WorktreePath == "" {
		log.Printf("[autonomous] prd=%s task=%s: protected-paths hold: the tree the task worked in is not recorded; leaving it held", prd.ID, t.ID)
		return false
	}
	dir := m.resolveTaskProjectDir(prd, t)
	if dir == "" {
		log.Printf("[autonomous] prd=%s task=%s: protected-paths hold has no readable task directory; leaving it held", prd.ID, t.ID)
		return false
	}
	base := hold.BaseCommit
	if remaining := m.pathsNotProvenDaemonOnly(ctx, dir, base, hold.Paths); len(remaining) > 0 {
		log.Printf("[autonomous] prd=%s task=%s: protected-paths re-check keeps %s against base %s; staying held", prd.ID, t.ID, strings.Join(remaining, ", "), base)
		return false
	}

	// Every held path is a proven daemon-section-only diff: the task did not edit
	// the operator's rules, so the hold is a false positive. Release it.

	// The Automaton may have closed since the snapshot: check the live record
	// before anything is withdrawn.
	if m.closedToRecoveryID(prd.ID) {
		return false
	}

	// 1. Withdraw the open hold question; no new question is raised. A
	//    question that cannot be withdrawn (an answer arrived meanwhile, or the
	//    store failed) keeps the hold.
	if hold.QuestionID != "" {
		if err := m.dispatchWithdraw(hold.QuestionID, "autonomous", "protected-paths hold released as false positive: all held paths are daemon-section-only against the base commit"); err != nil {
			log.Printf("[autonomous] prd=%s task=%s: withdrawing hold question %s: %v; leaving it held", prd.ID, t.ID, hold.QuestionID, err)
			return false
		}
	}

	// 2. Clear the hold and the error and record the decision on the stored
	//    row, not the snapshot, only if the hold still stands.
	live, ok := m.store.GetPRD(prd.ID)
	if !ok {
		return false
	}
	applied := false
	m.store.WithLock(func() {
		liveTask, _ := findTaskInPRD(live, t.ID)
		if liveTask == nil || liveTask.Status != TaskBlocked {
			return
		}
		if lh := liveTask.ProtectedPathHold; lh == nil || lh.QuestionID != hold.QuestionID || len(lh.Paths) == 0 {
			return
		}
		liveTask.ProtectedPathHold = nil
		liveTask.Error = ""
		live.Decisions = append(live.Decisions, Decision{
			At:    time.Now(),
			Kind:  protectedPathsFalsePositiveDecisionKind,
			Actor: "autonomous",
			Note: fmt.Sprintf("task %s: protected-paths hold released as false positive; all held paths are daemon-section-only against base %s: %s",
				t.ID, base, boundedFileList(sortedProtectedPaths(hold.Paths), protectedPathsQuestionFiles)),
			QuestionID: hold.QuestionID,
		})
		applied = true
	})
	if !applied {
		return false
	}
	if err := m.store.SavePRD(live); err != nil {
		log.Printf("[autonomous] prd=%s task=%s: saving the released protected-paths hold: %v", prd.ID, t.ID, err)
		return false
	}

	// 3. Return the task to the queue the way reset_task does without force:
	//    it saves the Automaton, records its own decision and unblocks the
	//    dependents.
	if _, err := m.resetTask(prd.ID, t.ID, "autonomous", false, false, nil); err != nil {
		log.Printf("[autonomous] prd=%s task=%s: re-queueing after the protected-paths hold release: %v", prd.ID, t.ID, err)
		return false
	}

	log.Printf("[autonomous] prd=%s task=%s: released false-positive protected-paths hold (daemon-section-only diff against base %s); task back in the queue", prd.ID, t.ID, base)
	return true
}
