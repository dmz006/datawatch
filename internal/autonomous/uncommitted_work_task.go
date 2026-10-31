// Uncommitted-work check at task completion.
//
// In an Automaton that commits its own work (prdAutoCommits) a task is only
// complete when its work is committed. After verification and the quality
// gate, checkUncommittedWork looks at the worktree: files left behind that
// belong to the task are committed here, and anything that stays uncommitted
// (or whose commit fails) is held or warned about according to
// autonomous.automation.uncommitted_work_policy. Files outside the task's
// declared files are reported and never committed automatically.
package autonomous

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// uncommittedCauseMaxRunes caps the commit error carried in a hold reason.
const uncommittedCauseMaxRunes = 300

// SetTaskCommit replaces the function the executor uses to commit a task's
// leftover files. Nil restores DefaultTaskCommit. Tests inject a fake.
func (m *Manager) SetTaskCommit(fn TaskCommitFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.taskCommit = fn
}

// taskCommitFn returns the commit function in effect.
func (m *Manager) taskCommitFn() TaskCommitFn {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.taskCommit == nil {
		return DefaultTaskCommit
	}
	return m.taskCommit
}

// uncommittedTaskCommitMessage is the subject of the rescue commit, in the
// style of the session's own task commits: task(<short Automaton id>): <title>.
func uncommittedTaskCommitMessage(prd *PRD, t *Task) string {
	title := strings.Join(strings.Fields(t.Title), " ")
	if title == "" {
		title = "session work"
	}
	return "task(" + shortID(prd.ID) + "): " + title
}

// uncommittedCommitCause is the hold reason for a failed commit.
func uncommittedCommitCause(err error) string {
	msg := strings.Join(strings.Fields(err.Error()), " ")
	if r := []rune(msg); len(r) > uncommittedCauseMaxRunes {
		msg = string(r[:uncommittedCauseMaxRunes]) + "…"
	}
	return UncommittedCauseCommitFailed + ": " + msg
}

// checkUncommittedWork refuses to let a task complete while its work is
// uncommitted. It returns held=true when the task was parked in TaskBlocked
// (the caller persists nothing further and returns nil, as for the setup-fault
// hold); a held task consumes no auto_fix_retries budget and records no failed
// attempt. It returns held=false when the task may complete: the policy is off,
// the check does not apply, the worktree is clean, the leftover files were
// committed here, or the policy is warn and a warning was recorded.
//
// The check applies only to an Automaton that commits (prdAutoCommits), and is
// skipped quietly when the task directory is empty or git cannot read its
// status (not a git repository).
func (m *Manager) checkUncommittedWork(ctx context.Context, prd *PRD, t *Task) (held bool, err error) {
	auto := m.Config().Automation
	policy := auto.EffectiveUncommittedWorkPolicy()
	if policy == config.UncommittedWorkPolicyOff {
		return false, nil
	}
	if !prdAutoCommits(prd) {
		log.Printf("[autonomous] %s/%s uncommitted-work: skipped, the Automaton does not commit", prd.ID, t.ID)
		return false, nil
	}
	dir := m.resolveTaskProjectDir(prd, t)
	if dir == "" {
		log.Printf("[autonomous] %s/%s uncommitted-work: skipped, no task directory", prd.ID, t.ID)
		return false, nil
	}

	scope := auto.EffectiveUncommittedWorkScope()
	writeDirs := EffectiveTaskWriteDirs(prd, t)
	detect := func() ([]UncommittedFile, error) {
		files, derr := DetectUncommittedWork(ctx, dir, m.driftGit)
		if derr != nil {
			return nil, derr
		}
		return ScopeUncommittedFiles(files, t, writeDirs, dir, scope), nil
	}

	remaining, derr := detect()
	if derr != nil {
		log.Printf("[autonomous] %s/%s uncommitted-work: skipped: %v", prd.ID, t.ID, derr)
		return false, nil
	}
	if len(remaining) == 0 {
		return false, nil
	}

	var cause string
	if paths := InScopeUncommitted(remaining); len(paths) > 0 {
		sha, cerr := m.taskCommitFn()(ctx, dir, paths, uncommittedTaskCommitMessage(prd, t))
		if cerr != nil {
			cause = uncommittedCommitCause(cerr)
			log.Printf("[autonomous] WARNING %s/%s uncommitted-work: commit failed: %v", prd.ID, t.ID, cerr)
		} else {
			log.Printf("[autonomous] %s/%s uncommitted-work: committed %d file(s) as %s", prd.ID, t.ID, len(paths), shortID(sha))
		}
	}

	after, derr := detect()
	switch {
	case derr != nil && cause == "":
		// The commit went through but the tree cannot be read again, so
		// there is nothing to hold on.
		log.Printf("[autonomous] %s/%s uncommitted-work: re-check skipped: %v", prd.ID, t.ID, derr)
		return false, nil
	case derr != nil:
		after = remaining
	case len(after) == 0 && cause == "":
		return false, nil
	case len(after) == 0:
		after = remaining
	}
	if cause == "" {
		cause = UncommittedCauseStillDirty
	}

	if policy == config.UncommittedWorkPolicyWarn {
		m.warnUncommittedWork(prd, t, after, cause)
		return false, nil
	}
	if herr := m.holdForUncommittedWork(prd, t, after, cause); herr != nil {
		return true, fmt.Errorf("hold task %s for uncommitted work: %w", t.ID, herr)
	}
	return true, nil
}
