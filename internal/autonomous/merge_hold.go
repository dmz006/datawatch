// Holding a task back while an earlier task's branch is still unmerged. A task
// cuts its worktree from the Automaton branch head, so one launched before the
// earlier branch lands would start from a stale head.

package autonomous

import (
	"context"
	"fmt"
	"log"
	"time"
)

// mergeHoldPoll is how often an executor with a task held for a merge looks
// again. The merge retry timer lands the merge; this poll notices it.
var mergeHoldPoll = 5 * time.Second

// mergeHoldReason returns a plain sentence when t must not start because
// another task of the Automaton that has its own branch is completed and not
// yet merged (merge state empty or pending), and "" otherwise. A task whose
// merge is in conflict or held is handled by its own path and does not hold.
// A task that does not get its own worktree, and a read-only task, are never
// held. Only a task that has not started is held.
func (m *Manager) mergeHoldReason(prd *PRD, t *Task) string {
	if prd == nil || t == nil || !m.taskWorktreeEnabled(prd, t) {
		return ""
	}
	reason := ""
	m.store.WithLock(func() {
		switch normTaskStatus(t.Status) {
		case TaskPending, TaskQueued, TaskWaitingCapacity, TaskWaitingDecision:
		default:
			return
		}
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				x := &prd.Story[si].Tasks[ti]
				if x == t || x.ID == t.ID {
					continue
				}
				if x.WorktreeBranch != "" && x.Status == TaskCompleted &&
					(x.MergeState == "" || x.MergeState == MergeStatePending) {
					reason = fmt.Sprintf("waiting for the merge of task %s into the Automaton branch", x.ID)
					return
				}
			}
		}
	})
	return reason
}

// mergeHold runs mergeHoldReason and records the result on the task's wait
// reason without touching its Status. It reports whether t is held.
func (m *Manager) mergeHold(prd *PRD, t *Task) bool {
	reason := m.mergeHoldReason(prd, t)
	var changed bool
	m.store.WithLock(func() {
		switch {
		case reason != "":
			changed = t.WaitReason != reason
			t.WaitReason = reason
		case isMergeHoldNote(t.WaitReason):
			changed = true
			t.WaitReason = ""
		}
	})
	if changed {
		if err := m.store.SaveTask(t); err != nil {
			log.Printf("[autonomous] %s/%s merge hold note: %v", prd.ID, t.ID, err)
		}
	}
	return reason != ""
}

// isMergeHoldNote reports whether s is a note written by mergeHold.
func isMergeHoldNote(s string) bool {
	const p = "waiting for the merge of task "
	return len(s) >= len(p) && s[:len(p)] == p
}

// mergeHoldPending reports whether any task of prd that has not started is
// held for a merge.
func (m *Manager) mergeHoldPending(prd *PRD) bool {
	var tasks []*Task
	m.store.WithLock(func() { tasks = flattenTasks(prd) })
	for _, t := range tasks {
		if m.mergeHoldReason(prd, t) != "" {
			return true
		}
	}
	return false
}

// awaitMergeLanding waits one poll interval (or a wake nudge), then runs the
// ordered merge pass so a merge that can land does. It reports false when ctx
// ended, so the caller stops waiting.
func (m *Manager) awaitMergeLanding(ctx context.Context, prd *PRD, wake <-chan struct{}) bool {
	timer := time.NewTimer(mergeHoldPoll)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-wake:
	case <-timer.C:
	}
	if _, err := m.MergeCompletedTasks(ctx, prd); err != nil {
		log.Printf("[autonomous] %s merge hold pass: %v", prd.ID, err)
	}
	return ctx.Err() == nil
}
