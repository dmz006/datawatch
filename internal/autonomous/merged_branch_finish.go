package autonomous

import (
	"context"
	"log"
	"strings"
	"time"
)

// mergedBranchFinishKind is the decision-log kind of the one entry written
// when an Automaton is finished because its merge commit is already in main.
const mergedBranchFinishKind = "merged_branch_finish"

// mergedBranchFinishTimeout bounds the read-only git checks.
const mergedBranchFinishTimeout = 20 * time.Second

// finishIfMerged completes an Automaton whose recorded merge commit is
// already contained in the base branch. It returns true only when the
// Automaton was moved to completed. Any git error, missing record or a commit
// not in main returns false and changes nothing, so the normal path (and its
// judge) still runs. Git is only read; no judge or executor is started.
func (m *Manager) finishIfMerged(prd *PRD) bool {
	if prd == nil || prd.MergedAt == nil || prd.MergedAt.IsZero() {
		return false
	}
	commit := strings.TrimSpace(prd.MergeCommit)
	dir := strings.TrimSpace(prd.ProjectDir)
	if commit == "" || dir == "" {
		return false
	}
	git := m.mergedFinishGit
	if git == nil {
		git = NewMergeGitRunner()
	}
	ctx, cancel := context.WithTimeout(context.Background(), mergedBranchFinishTimeout)
	defer cancel()
	base := resolveMergeBase(ctx, git, dir)
	if base == "" {
		return false
	}
	// A stored HEAD or branch name would always be "in main".
	commit, err := storedCommitArg(ctx, git, dir, commit)
	if err != nil {
		log.Printf("[autonomous] automaton=%s not finished from its shipped record: %v", prd.ID, err)
		return false
	}
	if _, err := git.Run(ctx, dir, "merge-base", "--is-ancestor", "--end-of-options", commit, base); err != nil {
		return false
	}
	rev, ok := m.store.PRDRev(prd.ID)
	if !ok {
		return false
	}
	at := m.holdNow()
	done := false
	err = m.store.CommitPRDIfRev(prd.ID, rev, func(p *PRD) bool {
		if p.Status == PRDCompleted || p.Status == PRDCancelled || p.Status == PRDArchived {
			return false
		}
		p.Status = PRDCompleted
		p.BlockedReason = ""
		p.Decisions = append(p.Decisions, Decision{
			At:    at,
			Kind:  mergedBranchFinishKind,
			Actor: "autonomous",
			Note:  "Automaton finished because its merge commit is already in main; no further work or review was started.",
		})
		done = true
		return true
	})
	if err != nil || !done {
		return false
	}
	log.Printf("[autonomous] automaton=%s finished: merge commit already in main", prd.ID)
	return true
}
