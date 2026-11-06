// Timed merge retry: one pass that retries every verified task branch still
// waiting to be folded into its Automaton branch.
package autonomous

import (
	"context"
	"log"
	"time"
)

// mergeRetryEligible reports whether prd is worth a retry: it is neither
// cancelled nor archived and has a completed task with a branch whose merge
// state is empty or pending.
func mergeRetryEligible(prd *PRD) bool {
	if prd == nil || prd.ProjectDir == "" ||
		prd.Status == PRDCancelled || prd.Status == PRDArchived {
		return false
	}
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			if t.WorktreeBranch != "" && t.Status == TaskCompleted &&
				(t.MergeState == "" || t.MergeState == MergeStatePending) {
				return true
			}
		}
	}
	return false
}

// MergeRetryPass walks every Automaton in the store that has a completed task
// waiting to merge (running, blocked for merge, or any other state except
// cancelled and archived) and runs MergeCompletedTasks for it, which takes the
// per-Automaton merge lock. A block that persists has its LastRetryAt recorded
// through setMergeBlock. A failure on one Automaton is logged and never stops
// the pass. Returns the number of tasks merged. It starts no timer.
func (m *Manager) MergeRetryPass(ctx context.Context) int {
	if m == nil || m.store == nil {
		return 0
	}
	total := 0
	for _, listed := range m.store.ListPRDs() {
		if ctx.Err() != nil {
			return total
		}
		snap, ok := m.store.SnapshotPRD(listed.ID)
		if !ok || !mergeRetryEligible(snap) {
			continue
		}
		prd, ok := m.store.GetPRD(listed.ID)
		if !ok {
			continue
		}
		started := time.Now()
		n, err := m.MergeCompletedTasks(ctx, prd)
		total += n
		if err != nil {
			log.Printf("[autonomous] %s merge retry: %v", prd.ID, err)
		}
		m.stampMergeRetry(prd, started)
	}
	return total
}

// stampMergeRetry records this retry on every task of prd that still carries a
// merge block the retry did not already refresh.
func (m *Manager) stampMergeRetry(prd *PRD, since time.Time) {
	var stale []*Task
	m.store.WithLock(func() {
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				t := &prd.Story[si].Tasks[ti]
				if t.MergeBlock != nil && t.MergeState != MergeStateMerged &&
					t.MergeBlock.LastRetryAt.Before(since) {
					stale = append(stale, t)
				}
			}
		}
	})
	for _, t := range stale {
		var b MergeBlock
		m.store.WithLock(func() { b = *t.MergeBlock })
		b.LastRetryAt = time.Now()
		if err := m.setMergeBlock(t, b); err != nil {
			log.Printf("[autonomous] %s/%s merge retry stamp: %v", prd.ID, t.ID, err)
		}
	}
}
