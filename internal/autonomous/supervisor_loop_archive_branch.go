// Branch guard for the supervisor loop's close step. A finished Automaton
// whose work branch still carries commits that are not on the base branch is
// kept, so that work is not orphaned by archiving. The guard is read-only: it
// runs for-each-ref and the CompareBranchToMain queries, nothing else.
//
// It applies only inside closeFinishedRecords, to every terminal state. Manager.Archive and the
// operator Archive path are unchanged and still archive.
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
func (s *SupervisorLoopSteps) archiveKeepsBranch(p *PRD) (bool, int) {
	if p == nil || p.Git.Branch == "" {
		return false, 0 // no branch work to protect
	}
	m := s.c.m
	m.mu.Lock()
	repo := m.cfg.WorktreeBaseRepo
	m.mu.Unlock()
	if repo == "" {
		log.Printf("[autonomous] supervisor-loop: keeping %s: no base repository to check branch %s", p.ID, p.Git.Branch)
		return true, 0
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
		return true, 0
	}
	if strings.TrimSpace(out) == "" {
		return false, 0 // the branch was dropped
	}
	base := p.Git.BaseBranch
	if base == "" {
		base = resolveMergeBase(ctx, git, repo)
	}
	cmp := CompareBranchToMain(ctx, git, repo, p.Git.Branch, base)
	if cmp.State == MergeStateUnknown {
		log.Printf("[autonomous] supervisor-loop: keeping %s: branch %s state unknown: %s", p.ID, p.Git.Branch, cmp.Error)
		return true, 0
	}
	return cmp.Ahead > 0, cmp.Ahead
}
