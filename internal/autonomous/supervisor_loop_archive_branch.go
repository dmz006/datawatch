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

	state, ahead, detail := checkBranchAhead(ctx, git, repo, p.Git.Branch, p.Git.BaseBranch)
	switch state {
	case branchCheckUnknown:
		log.Printf("[autonomous] supervisor-loop: keeping %s: branch %s state unknown: %s", p.ID, p.Git.Branch, detail)
		return true, 0
	case branchCheckDropped:
		return false, 0 // the branch was dropped
	}
	return ahead > 0, ahead
}

// Outcomes of checkBranchAhead.
const (
	branchCheckUnknown = "unknown" // git failed or the comparison could not be made
	branchCheckDropped = "dropped" // the branch no longer exists
	branchCheckAhead   = "ahead"   // the branch holds commits not on the base
	branchCheckMerged  = "merged"  // the branch holds nothing the base lacks
)

// checkBranchAhead is the read-only branch comparison shared by the close
// step guard and the archived-unmerged finder: for-each-ref, then
// CompareBranchToMain. An empty base is resolved from the repository. detail
// carries the reason when the state is unknown.
func checkBranchAhead(ctx context.Context, git MergeGitRunner, repo, branch, base string) (state string, ahead int, detail string) {
	out, err := git.Run(ctx, repo, "for-each-ref", "--format=%(refname)", "refs/heads/"+branch)
	if err != nil {
		return branchCheckUnknown, 0, "checking branch " + branch + ": " + err.Error()
	}
	if strings.TrimSpace(out) == "" {
		return branchCheckDropped, 0, ""
	}
	if base == "" {
		base = resolveMergeBase(ctx, git, repo)
	}
	cmp := CompareBranchToMain(ctx, git, repo, branch, base)
	if cmp.State == MergeStateUnknown {
		return branchCheckUnknown, 0, cmp.Error
	}
	if cmp.Ahead > 0 {
		return branchCheckAhead, cmp.Ahead, ""
	}
	return branchCheckMerged, 0, ""
}
