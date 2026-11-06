// Merge block bookkeeping: the record on Task.MergeBlock that says why a
// verified task's branch has not been folded into the Automaton branch.
package autonomous

import (
	"context"
	"fmt"
	"strings"
	"time"
)

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

// dirtyWorktreeFileCap is the most file names a dirty-worktree block lists.
const dirtyWorktreeFileCap = 20

// dirtyWorktreeBlock reports the uncommitted tracked edits in the worktree at
// dir as a MergeBlock, or nil when the tree is clean. It only reads; the
// edits are never discarded, stashed or committed.
func dirtyWorktreeBlock(ctx context.Context, dir string) *MergeBlock {
	out := gitText(ctx, dir, "status", "--porcelain", "--untracked-files=no")
	var files []string
	total := 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 4 {
			continue
		}
		name := strings.TrimSpace(line[3:])
		if i := strings.LastIndex(name, " -> "); i >= 0 {
			name = name[i+4:]
		}
		total++
		if len(files) < dirtyWorktreeFileCap {
			files = append(files, name)
		}
	}
	if total == 0 {
		return nil
	}
	list := strings.Join(files, ", ")
	if total > len(files) {
		list += fmt.Sprintf(" and %d more", total-len(files))
	}
	return &MergeBlock{
		Kind: MergeBlockKindDirtyWorktree,
		Reason: fmt.Sprintf("The Automaton worktree %s has uncommitted edits to %s, so finished task branches cannot be merged into it",
			dir, list),
		Action: "Commit or discard those edits in the worktree, then the merge is retried automatically",
		Files:  files,
	}
}

// alertMergeBlocked raises the one operator alert for a newly blocked task.
func (m *Manager) alertMergeBlocked(t *Task, b *MergeBlock) {
	if m == nil || b == nil {
		return
	}
	if fn := m.AlertSink(); fn != nil {
		fn("warn", "Automaton merge blocked",
			fmt.Sprintf("Task %q is finished but not merged. %s. %s.", t.Title, b.Reason, b.Action))
	}
}
