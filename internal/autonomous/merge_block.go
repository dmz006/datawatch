// Merge block bookkeeping: the record on Task.MergeBlock that says why a
// verified task's branch has not been folded into the Automaton branch.
package autonomous

import "time"

// Merge block kinds (MergeBlock.Kind).
const (
	// MergeBlockKindDirtyWorktree: the Automaton worktree has uncommitted
	// tracked changes, so MergeTaskBranch refuses to merge.
	MergeBlockKindDirtyWorktree = "dirty_worktree"
)

// setMergeBlock records b on the task and saves it. When the task already
// carries a block of the same Kind, the original Since is kept and Attempts is
// incremented; otherwise the block starts fresh (Since defaults to now,
// Attempts to 1).
func (m *Manager) setMergeBlock(t *Task, b MergeBlock) error {
	m.store.WithLock(func() {
		if prev := t.MergeBlock; prev != nil && prev.Kind == b.Kind {
			if !prev.Since.IsZero() {
				b.Since = prev.Since
			}
			b.Attempts = prev.Attempts + 1
		} else {
			if b.Since.IsZero() {
				b.Since = time.Now()
			}
			if b.Attempts < 1 {
				b.Attempts = 1
			}
		}
		t.MergeBlock = &b
	})
	return m.store.SaveTask(t)
}

// clearMergeBlock removes the task's merge block record, saving only when
// there was one.
func (m *Manager) clearMergeBlock(t *Task) error {
	had := false
	m.store.WithLock(func() {
		if t.MergeBlock != nil {
			had = true
			t.MergeBlock = nil
		}
	})
	if !had {
		return nil
	}
	return m.store.SaveTask(t)
}
