// The merged-branch guard ties archiving of finished Automata to whether
// their work actually landed on the base branch.
//
// A completed record whose work branch still carries commits not on the base
// branch is refused to automatic archiving: Manager.Archive returns
// ErrArchiveUnmerged (the typed ArchiveUnmergedError naming the branch and
// the commit count) when the actor is not the operator (supervisor loop and
// any other automated actor). The operator can still archive a completed or
// cancelled record (explicit decision, e.g. an abandon), and the merge step
// archives after the merge has landed, so both paths stay unblocked.
//
// The check is read-only: it runs the branch-existence query and the
// CompareBranchToMain queries, nothing else. "Merged" is judged by
// cherry-pick equivalence ("git cherry <base> <branch>" minus lines), so a
// rebased or squashed landing counts as merged; an empty branch (zero
// commits ahead) has no work and lets the record go. Any git failure, an
// empty base repository or an unknown comparison keeps the record — the
// failure mode is "never archive", never "archive early".
//
// The same check feeds the derived read-only merge_state field of the
// Automaton view: a completed or archived record whose branch has commits
// not on base shows MergeStateFinishedNotMerged; it is computed on read,
// never persisted on the record, and never hides the record.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

// ErrArchiveUnmerged is the sentinel for Manager.Archive's refusal to
// archive a completed record with unmerged branch work on behalf of an
// automated actor. The concrete error is *ArchiveUnmergedError, which
// names the branch and the commit count.
var ErrArchiveUnmerged = errors.New("automaton branch has commits not merged into the base branch")

// ErrArchiveMergeUnknown is returned by Manager.Archive when an automated
// actor would archive a completed record but the merge state of its branch
// cannot be determined (git failure, unreadable comparison). The record is
// kept: a "merged" verdict it could not verify is a lost-work failure.
var ErrArchiveMergeUnknown = errors.New("archive: branch merge state could not be determined; keeping the record")

// ArchiveUnmergedError is the typed ErrArchiveUnmerged refusal: it names
// the branch that still carries work and the number of its commits that
// are not on the base branch.
type ArchiveUnmergedError struct {
	Branch  string
	Commits int
}

func (e *ArchiveUnmergedError) Error() string {
	return fmt.Sprintf("archive of completed automaton refused: branch %s has %d commit(s) not merged into the base branch; merge the branch or archive as the operator once the work is abandoned", e.Branch, e.Commits)
}

// Is makes errors.Is(refusal, ErrArchiveUnmerged) true.
func (e *ArchiveUnmergedError) Is(target error) bool { return target == ErrArchiveUnmerged }

// MergeStateFinishedNotMerged is the derived read-only merge_state value on
// the Automaton view: the record finished (completed, or archived since)
// while its work branch still carries commits not on the base branch.
// Computed on read; never stored.
const MergeStateFinishedNotMerged = "finished_not_merged"

// mergeStateCacheWindowDefault is how long a derived merge_state value is
// cached per record in the Manager (see automatonMergeState).
const mergeStateCacheWindowDefault = time.Minute

// branchUnmerged reports whether branch still carries commits that are not
// on base, and how many. The third return value, known, is false when the
// comparison could not be determined (a git failure, a bad count, an
// unknown state); callers must then keep the record rather than treat it
// as merged. Only a genuinely absent branch ref (rev-parse error with
// "not a valid object" / empty for-each-ref output and no other failure)
// or a successful comparison showing the work on base (merged) or nothing
// to land (empty) lets the record go. Read-only: ref queries plus the
// CompareBranchToMain queries.
func branchUnmerged(ctx context.Context, git MergeGitRunner, repo, branch, base string) (unmerged bool, commits int, known bool) {
	if branch == "" {
		return false, 0, true
	}
	// Branch existence: an error here is either "branch dropped" (valid,
	// nothing left to wait for) or a git failure (must keep the record).
	out, err := git.Run(ctx, repo, "for-each-ref", "--format=%(refname)", "refs/heads/"+branch)
	if err != nil {
		return true, 0, false
	}
	if strings.TrimSpace(out) == "" {
		return false, 0, true // the branch was dropped: nothing left to wait for
	}
	cmp := CompareBranchToMain(ctx, git, repo, branch, base)
	switch cmp.State {
	case MergeStateMerged, MergeStateEmpty:
		return false, 0, true
	case MergeStateUnknown:
		return true, 0, false
	}
	commits = cmp.CherryNotOnBase
	if commits <= 0 {
		commits = cmp.Ahead
	}
	return true, commits, true
}

// archiveMergeRunner returns the git runner for the branch checks: the
// injected test one when set, the production shell otherwise.
func (m *Manager) archiveMergeRunner() MergeGitRunner {
	m.mu.Lock()
	git := m.archiveGuardGit
	m.mu.Unlock()
	if git == nil {
		return NewMergeGitRunner()
	}
	return git
}

// archiveBranchMergeBlock runs the merge-state check for record p and
// reports (block, commits, known): block=true with known=false means the
// state is undetermined and the record must be kept.
func (m *Manager) archiveBranchMergeBlock(ctx context.Context, p *PRD) (bool, int, bool) {
	if p == nil || p.Git.Branch == "" {
		return false, 0, true // no branch work to protect
	}
	m.mu.Lock()
	repo := m.cfg.WorktreeBaseRepo
	m.mu.Unlock()
	if repo == "" {
		return true, 0, true // no base repository to check against: keep
	}
	git := m.archiveMergeRunner()
	base := p.Git.BaseBranch
	if base == "" {
		base = resolveMergeBase(ctx, git, repo)
	}
	return branchUnmerged(ctx, git, repo, p.Git.Branch, base)
}

// archiveActorIsOperator reports whether actor is the manual operator
// (explicit archive decision). The supervisor loop and any other automated
// actor are not.
func archiveActorIsOperator(actor string) bool {
	return actor == operatorActor
}

// archiveGuard applies the merged-branch guard to Archive for actor. It
// refuses (typed ErrArchiveUnmerged, or ErrArchiveMergeUnknown when the
// state is undetermined) only a completed record whose branch has unmerged
// commits when the actor is automated; cancelled/rejected records, operator
// archives and merged (or empty, or already dropped) branches pass.
func (m *Manager) archiveGuard(p *PRD, actor string) error {
	if p == nil || p.Status != PRDCompleted {
		return nil
	}
	if archiveActorIsOperator(actor) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), mergePendingGitTimeout*2)
	defer cancel()
	block, commits, known := m.archiveBranchMergeBlock(ctx, p)
	switch {
	case !block:
		return nil
	case known:
		return &ArchiveUnmergedError{Branch: p.Git.Branch, Commits: commits}
	default:
		log.Printf("[autonomous] Archive %s kept: branch merge state undetermined", p.ID)
		return ErrArchiveMergeUnknown
	}
}

type mergeStateCacheEntry struct {
	value    string
	computed time.Time
}

// AutomatonMergeState returns the derived read-only merge_state for views
// of p: MergeStateFinishedNotMerged when p is completed (or archived since)
// and its work branch still carries commits not on the base branch —
// including a record the operator archived while unmerged. It is "" for
// active or non-completed records, for records with no branch, for merged
// or empty or dropped branches, and when the state cannot be determined
// (the field never hides work, it only surfaces it). Never persisted.
// Results are cached per record for mergeStateCacheWindow so a read view
// over many completed Automata does not issue the base repository's git
// calls N+1 times; the cache stores values only, the record is never
// mutated, and the branch state is re-verified on every guard write.
func (m *Manager) AutomatonMergeState(p *PRD) string {
	if p == nil || (p.Status != PRDCompleted && p.Status != PRDArchived) {
		return ""
	}
	key := p.ID + "\x00" + string(p.Status)
	m.mu.Lock()
	window := m.mergeStateCacheWindow
	if window <= 0 {
		window = mergeStateCacheWindowDefault
	}
	if e, ok := m.mergeStateCache[key]; ok && time.Since(e.computed) < window {
		m.mu.Unlock()
		return e.value
	}
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), mergePendingGitTimeout)
	defer cancel()
	block, _, known := m.archiveBranchMergeBlock(ctx, p)
	value := ""
	if known && block {
		value = MergeStateFinishedNotMerged
	}

	m.mu.Lock()
	if m.mergeStateCache == nil {
		m.mergeStateCache = map[string]mergeStateCacheEntry{}
	}
	m.mergeStateCache[key] = mergeStateCacheEntry{value: value, computed: time.Now()}
	m.mu.Unlock()
	return value
}

// SetArchiveGuardGit injects the git runner the archive guard and the
// derived merge_state view use; nil restores the production shell. Tests
// use it in place of shelling out; the runner is only for read queries.
func (m *Manager) SetArchiveGuardGit(git MergeGitRunner) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.archiveGuardGit = git
}
