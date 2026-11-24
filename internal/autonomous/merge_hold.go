// Holding a task back while an earlier task's branch is still unmerged. A task
// cuts its worktree from the Automaton branch head, so one launched before the
// earlier branch lands would start from a stale head.

package autonomous

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

// mergeHoldPoll is how often an executor with a task held for a merge looks
// again. The merge retry timer lands the merge; this poll notices it.
var mergeHoldPoll = 5 * time.Second

// mergeHoldBackoffMax caps the wait between two failed merge hold passes of one
// Automaton. The wait starts at mergeHoldPoll and doubles with each failure.
var mergeHoldBackoffMax = 5 * time.Minute

// mergeHoldClock is the time source of the merge hold backoff; tests replace it.
var mergeHoldClock = time.Now

// mergeHoldBackoff is one Automaton's failure state: consecutive failed passes,
// the earliest time the next pass may run, and the last error text (so a
// repeated identical failure is logged once, not on every retry).
type mergeHoldBackoff struct {
	fails   int
	next    time.Time
	lastErr string
}

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

// runMergeHoldPass runs one ordered merge pass for an Automaton that has a task
// held for a merge, and keeps the engine quiet while it cannot help:
//   - the operator pause holds it (a merge pass is work the engine starts by
//     itself; the operator resumes it);
//   - after a failed pass the next one waits, doubling from mergeHoldPoll up to
//     mergeHoldBackoffMax, and the same failure is logged once, so a pass that
//     fails on the same cause (uncommitted changes in a task worktree) does not
//     retry every few seconds for ever. A pass that works clears the backoff.
//
// It reports whether the pass ran.
func (m *Manager) runMergeHoldPass(ctx context.Context, prd *PRD) bool {
	var dir string
	m.store.WithLock(func() { dir = prd.ProjectDir })
	if _, blocked := m.pauseBlock(dir); blocked {
		return false
	}
	now := mergeHoldClock()
	m.mu.Lock()
	st := m.mergeHoldBack[prd.ID]
	m.mu.Unlock()
	if st != nil && now.Before(st.next) {
		return false
	}
	n, err := m.MergeCompletedTasks(ctx, prd)
	// A pass that merged nothing while a finished task carries a merge block
	// (uncommitted changes, a conflict) is as unproductive as an error: the
	// block is recorded and alerted once, so the retry only needs to look
	// again later.
	blocked := err == nil && n == 0 && m.mergeBlockedTask(prd)
	m.mu.Lock()
	defer m.mu.Unlock()
	if (err == nil && !blocked) || ctx.Err() != nil {
		delete(m.mergeHoldBack, prd.ID)
		return true
	}
	if blocked {
		err = errMergeStillBlocked
	}
	if m.mergeHoldBack == nil {
		m.mergeHoldBack = map[string]*mergeHoldBackoff{}
	}
	if st == nil {
		st = &mergeHoldBackoff{}
		m.mergeHoldBack[prd.ID] = st
	}
	st.fails++
	wait := mergeHoldPoll
	for i := 1; i < st.fails && wait < mergeHoldBackoffMax; i++ {
		wait *= 2
	}
	if wait > mergeHoldBackoffMax {
		wait = mergeHoldBackoffMax
	}
	st.next = now.Add(wait)
	if msg := err.Error(); msg != st.lastErr {
		st.lastErr = msg
		if blocked {
			return true
		}
		log.Printf("[autonomous] %s merge hold pass: %v (next try in %s)", prd.ID, err, wait)
	}
	return true
}

// errMergeStillBlocked marks a merge hold pass that landed nothing because a
// finished task's merge carries a block (see MergeBlock).
var errMergeStillBlocked = errors.New("merge still blocked")

// mergeBlockedTask reports whether a completed task of prd that waits to merge
// carries a merge block.
func (m *Manager) mergeBlockedTask(prd *PRD) bool {
	found := false
	m.store.WithLock(func() {
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				t := &prd.Story[si].Tasks[ti]
				if t.WorktreeBranch != "" && t.Status == TaskCompleted && t.MergeBlock != nil &&
					(t.MergeState == "" || t.MergeState == MergeStatePending) {
					found = true
				}
			}
		}
	})
	return found
}

// awaitMergeLanding waits one poll interval (or a wake nudge), then runs the
// ordered merge pass (runMergeHoldPass) so a merge that can land does. It
// reports false when ctx ended, so the caller stops waiting.
func (m *Manager) awaitMergeLanding(ctx context.Context, prd *PRD, wake <-chan struct{}) bool {
	timer := time.NewTimer(mergeHoldPoll)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-wake:
	case <-timer.C:
	}
	m.runMergeHoldPass(ctx, prd)
	return ctx.Err() == nil
}
