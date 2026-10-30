// pushValidatedWork is the harness push engine: after a task has been
// validated (or a PRD has finally gated and completed, per the
// finalGatesPassed flag), push the local work to the remote according
// to the effective push-on-complete policy.
//
// Discipline:
//
//   - never returns an error and never alters local commits — a failed
//     push (no remote, auth, non-fast-forward, anything) is recorded on
//     the PRD as PushState=not_pushed plus a visible PushWarning and an
//     operator alert (see recordPushOutcome);
//   - never force-pushes: internal/git performs plain fast-forward-only
//     pushes and has no force option;
//   - serialised per PRD so two concurrent tasks cannot interleave a
//     pair of push attempts;
//   - safe to call repeatedly (after each validated task, then again at
//     completion) — an already-up-to-date remote yields NothingToPush.
package autonomous

import (
	"context"
	"log"
	"strings"
	"sync"

	"github.com/dmz006/datawatch/internal/git"
)

// pushLockMu guards pushPerPRD. Package-level (not on Manager) so the
// engine only needs its own two files; PRD ids are unique per store,
// so per-PRD serialisation of push attempts holds regardless.
var (
	pushLockMu sync.Mutex
	pushPerPRD = map[string]*sync.Mutex{}
)

// pushLockFor returns the per-PRD mutex that serialises push attempts
// for one PRD, creating it on first use.
func pushLockFor(prdID string) *sync.Mutex {
	pushLockMu.Lock()
	defer pushLockMu.Unlock()
	ln, ok := pushPerPRD[prdID]
	if !ok {
		ln = &sync.Mutex{}
		pushPerPRD[prdID] = ln
	}
	return ln
}

// pushValidatedWork pushes a PRD's local work per its effective
// push-on-complete policy (PRD override > daemon setting > branch).
//
// Behaviour by policy:
//
//   - never (git.PushNever): does nothing, records nothing.
//   - branch (git.PushBranch): pushes the work branch (prd.Git.Branch,
//     else the current HEAD branch) to remote/<branch> with upstream
//     tracking set. A detached HEAD (no work branch recorded and no
//     symbolic HEAD) is skipped without any state change.
//   - default_branch (git.PushDefaultBranch): no-op unless
//     finalGatesPassed; then a plain fast-forward push of HEAD onto
//     the remote's default branch.
//
// Gated off for cluster-dispatched PRDs (their workers push their own
// work), for PRDs with no project directory, and for directories that
// are not git repositories.
func (m *Manager) pushValidatedWork(prd *PRD, finalGatesPassed bool) {
	if prd.ClusterProfile != "" {
		return
	}
	dir := prd.ProjectDir
	if dir == "" {
		return
	}
	policy, remote := m.effectivePushPolicy(prd)
	if policy == git.PushNever {
		return
	}
	if _, err := runGitIn(dir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return // not a git repo — nothing to push
	}

	lock := pushLockFor(prd.ID)
	lock.Lock()
	defer lock.Unlock()

	// Neighbouring completion code (PR open, worktree cleanup, ...)
	// saves the PRD between our read and now; reload the stored copy
	// before mutating so we neither clobber nor double-record state.
	saved, ok := m.store.GetPRD(prd.ID)
	if !ok {
		saved = prd
	}

	ctx := context.Background()
	switch policy {
	case git.PushBranch:
		branch := saved.Git.Branch
		if branch == "" {
			if out, rerr := runGitIn(dir, "rev-parse", "--abbrev-ref", "HEAD"); rerr == nil {
				branch = strings.TrimSpace(out)
			}
		}
		if branch == "" || branch == "HEAD" {
			return // detached HEAD with no recorded work branch — no state change
		}
		log.Printf("autonomous: prd %s push-on-complete (branch): pushing %s to remote %s", saved.ID, branch, remote)
		m.recordPushOutcome(saved, git.PushToBranch(ctx, dir, remote, branch), policy)
	case git.PushDefaultBranch:
		if !finalGatesPassed {
			return
		}
		log.Printf("autonomous: prd %s push-on-complete (default_branch): pushing HEAD to remote %s", saved.ID, remote)
		m.recordPushOutcome(saved, git.PushToDefaultBranch(ctx, dir, remote), policy)
	}
}
