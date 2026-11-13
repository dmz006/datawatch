// Branch guard for the supervisor loop's close step. A finished Automaton
// whose work branch still carries commits that are not on the base branch
// is kept, so that work is not orphaned by archiving:
//
//   - A completed record is kept while its branch carries unmerged commits
//     (merged-branch guard, archive_merge_guard.go). "Merged" is judged
//     by cherry-pick equivalence, so a rebased or squashed landing counts;
//     an empty branch (zero commits ahead) or a dropped branch has no work.
//
//   - A cancelled record past its grace window is kept while its branch is
//     ahead of the base branch (archiveKeepsBranch).
//
// Both guards are read-only: they run ref/rev-list/cherry/log queries,
// nothing else. They apply inside closeFinishedRecords. Manager.Archive
// applies the same merged-branch rule to automated actors (operator
// archives and the merge step stay unblocked), and the derived read-only
// merge_state field surfaces the state on record views.
package autonomous

import (
	"context"
	"log"
	"strings"
)

// archiveBranchGitTimeout bounds the git work of one record's check.
const archiveBranchGitTimeout = mergePendingGitTimeout

// archiveKeepsBranch reports whether the close step must keep p because its
// branch holds work that is not on the base branch. Conservative: any git
// error, an empty base repository or an unknown comparison keeps the record.
// Only a missing branch, no recorded branch, or a comparison showing zero
// commits ahead lets the record go.
func (s *SupervisorLoopSteps) archiveKeepsBranch(p *PRD) bool {
	if p == nil || p.Git.Branch == "" {
		return false // no branch work to protect
	}
	m := s.c.m
	m.mu.Lock()
	repo := m.cfg.WorktreeBaseRepo
	m.mu.Unlock()
	if repo == "" {
		log.Printf("[autonomous] supervisor-loop: keeping %s: no base repository to check branch %s", p.ID, p.Git.Branch)
		return true
	}
	git := s.mergeGit
	if git == nil {
		git = NewMergeGitRunner()
	}
	ctx, cancel := context.WithTimeout(context.Background(), archiveBranchGitTimeout)
	defer cancel()

	out, err := git.Run(ctx, repo, "for-each-ref", "--format=%(refname)", "refs/heads/"+p.Git.Branch)
	if err != nil {
		log.Printf("[autonomous] supervisor-loop: keeping %s: checking branch %s: %v", p.ID, p.Git.Branch, err)
		return true
	}
	if strings.TrimSpace(out) == "" {
		return false // the branch was dropped
	}
	base := p.Git.BaseBranch
	if base == "" {
		base = resolveMergeBase(ctx, git, repo)
	}
	cmp := CompareBranchToMain(ctx, git, repo, p.Git.Branch, base)
	if cmp.State == MergeStateUnknown {
		log.Printf("[autonomous] supervisor-loop: keeping %s: branch %s state unknown: %s", p.ID, p.Git.Branch, cmp.Error)
		return true
	}
	return cmp.Ahead > 0
}

// archiveCompletedBranchGuard reports whether the close step must keep p
// because p finished while its work branch still carries commits that are
// not on the base branch (merged-branch guard). It shares branchUnmerged
// with the Manager.Archive guard, so both paths judge "merged" the same
// way — by cherry-pick equivalence, so a rebased or squashed landing
// counts as merged. Same conservative failure mode: any git error, an
// empty base repository or an unknown comparison keeps the record.
func (s *SupervisorLoopSteps) archiveCompletedBranchGuard(p *PRD) bool {
	if p == nil {
		return false
	}
	m := s.c.m
	ctx, cancel := context.WithTimeout(context.Background(), archiveBranchGitTimeout*2)
	defer cancel()
	block, _, known := m.archiveBranchMergeBlock(ctx, p)
	if !known {
		log.Printf("[autonomous] supervisor-loop: keeping %s: branch merge state undetermined", p.ID)
		return true
	}
	return block
}
