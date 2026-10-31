// Uncommitted-work check at Automaton finish.
//
// The per-task check (checkUncommittedWork) looks at one task's files. This
// one runs once, in the roll-up, before an Automaton is reported finished: it
// looks at the whole worktree, so files that no single task owns (or that a
// cancel or a backend move left behind) stop the Automaton from reaching
// completed, and so from being listed as merge ready. It never stages or
// commits anything; the operator decides what happens to the files.
package autonomous

import (
	"context"
	"fmt"
	"log"

	"github.com/dmz006/datawatch/internal/config"
)

// uncommittedRollupReason is the status-change reason of a roll-up that
// blocked on uncommitted files.
const uncommittedRollupReason = "uncommitted_work"

// UncommittedCauseAutomatonFinished is the cause recorded when the whole
// worktree still holds uncommitted files as the Automaton finishes.
const UncommittedCauseAutomatonFinished = "files were still uncommitted when the Automaton finished"

// checkWorktreeUncommittedAtFinish runs the uncommitted-work check over the
// whole worktree of an Automaton whose tasks are all terminal. It returns
// blocked=true when the Automaton must not complete: the policy is hold, the
// worktree is dirty, and the hold has been recorded (the caller commits the
// blocked status). It returns blocked=false when the Automaton may complete:
// the policy is off (no git call is made), the check does not apply, git
// cannot read the status, the worktree is clean, or the policy is warn and a
// warning was recorded.
//
// The check applies only to an Automaton with a local worktree: it needs a
// work branch, no cluster profile and a project directory. Scope filtering
// does not apply here (the scope setting governs only the per-task check), and
// nothing is committed.
func (m *Manager) checkWorktreeUncommittedAtFinish(ctx context.Context, prd *PRD) (blocked bool, err error) {
	policy := m.Config().Automation.EffectiveUncommittedWorkPolicy()
	if policy == config.UncommittedWorkPolicyOff {
		return false, nil
	}
	if prd.Git.Branch == "" || prd.ClusterProfile != "" || prd.ProjectDir == "" {
		log.Printf("[autonomous] %s uncommitted-work: roll-up check skipped, the Automaton has no local worktree", prd.ID)
		return false, nil
	}

	files, derr := DetectUncommittedWork(ctx, prd.ProjectDir, m.driftGit)
	if derr != nil {
		log.Printf("[autonomous] %s uncommitted-work: roll-up check skipped: %v", prd.ID, derr)
		return false, nil
	}
	if len(files) == 0 {
		return false, nil
	}

	if policy == config.UncommittedWorkPolicyWarn {
		m.warnUncommittedWork(prd, nil, files, UncommittedCauseAutomatonFinished)
		return false, nil
	}

	m.holdAutomatonForUncommittedWork(prd, files, UncommittedCauseAutomatonFinished)
	// A hold that was already open is left as it was; show the files as they
	// are now.
	all, outside := uncommittedFilePaths(files)
	m.store.WithLock(func() {
		if prd.UncommittedWork != nil {
			prd.UncommittedWork.Files = all
			prd.UncommittedWork.OutsideScope = outside
		}
	})
	return true, nil
}

// uncommittedRollupBlockedReason is the plain sentence stored as the
// Automaton's blocked reason after the roll-up check held it: the cause, the
// files, and what to do.
func uncommittedRollupBlockedReason(prd *PRD) string {
	var hold *UncommittedWorkHold
	if prd != nil {
		hold = prd.UncommittedWork
	}
	if hold == nil || len(hold.Files) == 0 {
		return "Files are still uncommitted in the worktree. Commit or discard them, then run the Automaton again."
	}
	return fmt.Sprintf("%s: %s. Commit or discard them, then run the Automaton again.",
		hold.Reason, boundedFileList(hold.Files, uncommittedQuestionFiles))
}
