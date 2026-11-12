// Merge block bookkeeping: the record on Task.MergeBlock that says why a
// verified task's branch has not been folded into the Automaton branch.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"log"
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
	if err := m.store.SaveTask(t); err != nil {
		return err
	}
	m.refreshMergeBlockTriageFor(t)
	return nil
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
	if err := m.store.SaveTask(t); err != nil {
		return err
	}
	m.refreshMergeBlockTriageFor(t)
	return nil
}

// refreshMergeBlockTriageFor refreshes the triage record of the task's
// Automaton; a task whose Automaton is not in the store changes nothing.
func (m *Manager) refreshMergeBlockTriageFor(t *Task) {
	if prd, ok := m.store.GetPRD(t.PRDID); ok {
		m.refreshMergeBlockTriage(prd)
	}
}

// refreshMergeBlockTriage writes or refreshes the Automaton's triage record
// while at least one task is merge blocked, even though the Automaton keeps
// running, and removes the merge-blocked part when none is. A failed or
// blocked record keeps its own fields: only its MergeBlocks list changes.
func (m *Manager) refreshMergeBlockTriage(prd *PRD) {
	if m == nil || m.store == nil || prd == nil {
		return
	}
	snap, ok := m.store.SnapshotPRD(prd.ID)
	if !ok {
		return
	}
	var blocks []TriageMergeBlock
	for i := range snap.Story {
		for j := range snap.Story[i].Tasks {
			t := &snap.Story[i].Tasks[j]
			if t.MergeBlock == nil {
				continue
			}
			blocks = append(blocks, TriageMergeBlock{
				TaskID: t.ID, TaskTitle: t.Title, Reason: t.MergeBlock.Reason,
				Action: t.MergeBlock.Action, Since: t.MergeBlock.Since, Attempts: t.MergeBlock.Attempts,
			})
		}
	}
	now := time.Now()
	for attempt := 0; attempt < 3; attempt++ {
		rev, ok := m.store.PRDRev(prd.ID)
		if !ok {
			return
		}
		err := m.store.CommitPRDIfRev(prd.ID, rev, func(p *PRD) bool {
			switch tr := p.Triage; {
			case tr == nil && len(blocks) == 0:
				return false
			case tr == nil:
				name, member := m.mergeBlockOwner(p)
				p.Triage = &TriageRecord{
					AutomationID: p.AutomationID, AutomationName: name, MemberID: member,
					PRDID: p.ID, PRDTitle: p.Title, Status: TriageStatusMergeBlocked,
					RetryBudget: m.ResolvedTriageBudget(p), FirstSeenAt: now, UpdatedAt: now,
					MergeBlocks: blocks,
				}
			case tr.Status == TriageStatusMergeBlocked && len(blocks) == 0:
				p.Triage = nil
			case tr.Status == TriageStatusMergeBlocked ||
				tr.Status == string(PRDFailed) || tr.Status == string(PRDBlocked):
				if len(tr.MergeBlocks) == 0 && len(blocks) == 0 {
					return false
				}
				tr.MergeBlocks = blocks
				tr.UpdatedAt = now
			default:
				return false // merge_pending and other records are left alone
			}
			return true
		})
		if err == nil || !errors.Is(err, ErrPRDStale) {
			if err != nil {
				log.Printf("[autonomous] merge block triage for %s: %v", prd.ID, err)
			}
			return
		}
	}
}

// mergeBlockOwner resolves the owning Automation's name and member ID.
func (m *Manager) mergeBlockOwner(p *PRD) (name, member string) {
	if m.automations == nil || p.AutomationID == "" {
		return "", ""
	}
	a, ok := m.automations.Get(p.AutomationID)
	if !ok {
		return "", ""
	}
	for i := range a.Members {
		if a.Members[i].PRDID == p.ID {
			return a.Name, a.Members[i].ID
		}
	}
	return a.Name, ""
}

// dirtyWorktreeFileCap is the most file names a dirty-worktree block lists.
const dirtyWorktreeFileCap = 20

// dirtyWorktreeBlock reports the uncommitted tracked edits in the worktree at
// dir as a MergeBlock, or nil when the tree is clean. It only reads; the
// edits are never discarded, stashed or committed.
func dirtyWorktreeBlock(ctx context.Context, dir string) *MergeBlock {
	return dirtyWorktreeBlockWith(ctx, dir, defaultDriftGitRunner)
}

// dirtyWorktreeBlockWith is dirtyWorktreeBlock with an explicit git runner.
// Tracked files whose only change is the daemon's own marked sections are not
// real edits and are left out of the listing.
func dirtyWorktreeBlockWith(ctx context.Context, dir string, run driftGitRunner) *MergeBlock {
	out := dropDaemonOnlyStatusEntries(ctx, dir, gitText(ctx, dir, "status", "--porcelain", "--untracked-files=no"), run)
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
