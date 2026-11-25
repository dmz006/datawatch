// Releasing the Automaton worktree after completion with an automatic pull
// request. The same rules as for task worktrees (task_worktree_keep.go) apply:
// cleanup_mode off and report remove nothing; a worktree with its own
// uncommitted or untracked files, a live session, detached commits of its own,
// or anything that cannot be checked is kept; and what was removed or kept is
// written to the audit log. This step deletes no branch.
package autonomous

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"

	"github.com/dmz006/datawatch/internal/config"
)

// Audit actions for the Automaton worktree released at completion.
const (
	auditCompletionWorktreeRemove      = "autonomous_completion_worktree_remove"
	auditCompletionWorktreeKept        = "autonomous_completion_worktree_kept"
	auditCompletionWorktreeWouldRemove = "autonomous_completion_worktree_would_remove"
)

// showUntrackedOverride makes git's own unforced "worktree remove" look for
// untracked files. With status.showUntrackedFiles=no in the repository or user
// settings, git would otherwise delete them without refusing.
const showUntrackedOverride = "status.showUntrackedFiles=normal"

// releaseCompletionWorktree removes prd's worktree when cleanup_mode is on and
// nothing in it would be lost. The work branch stays in every case.
func (m *Manager) releaseCompletionWorktree(prd *PRD) {
	dir := prd.ProjectDir
	if dir == "" {
		return
	}
	m.mu.Lock()
	mode := m.cfg.Automation.EffectiveCleanupMode()
	repo, wtDir := m.cfg.WorktreeBaseRepo, m.cfg.WorktreeDir
	m.mu.Unlock()
	if wtDir == "" {
		wtDir = defaultWorktreeDir(m.dataDir)
	}

	tip, head := "", ""
	audit := func(action string, extra map[string]any) {
		details := map[string]any{
			"automaton_id": prd.ID,
			"worktree":     dir,
			"branch":       prd.Git.Branch,
			"branch_tip":   tip,
			"head":         head,
			"mode":         mode,
		}
		for k, v := range extra {
			details[k] = v
		}
		if err := m.worktreeCleanupAudit()(action, details); err != nil {
			log.Printf("[autonomous] prd=%s completion worktree: audit %s: %v", prd.ID, action, err)
		}
	}
	keep := func(reason, detail string, files ...string) {
		if len(files) > maxKeptFiles {
			files = files[:maxKeptFiles]
		}
		log.Printf("[autonomous] prd=%s completion: worktree %s kept (%s): %s", prd.ID, dir, reason, detail)
		audit(auditCompletionWorktreeKept, map[string]any{"reason": reason, "detail": detail, "files": files})
	}

	if mode == config.CleanupModeOff {
		keep(taskKeepCleanupOff, "cleanup_mode is off: nothing is looked at or removed")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), worktreeReleaseGitTimeout)
	defer cancel()

	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return // already gone
		}
		keep(taskKeepUnverified, "the worktree could not be read: "+err.Error())
		return
	}
	// Without its .git entry, git would answer for a parent repository.
	if _, err := os.Lstat(filepath.Join(dir, ".git")); err != nil {
		keep(taskKeepUnverified, "the folder has no .git entry, so its files cannot be checked")
		return
	}
	if repo == "" {
		repo = dir // the worktree names its own repository
	}
	got, _, err := branchTip(ctx, dir, prd.Git.Branch)
	if err != nil {
		keep(taskKeepUnverified, "the work branch could not be read: "+err.Error())
		return
	}
	tip = got
	if m.taskWorktreeInUse(dir) {
		keep(taskKeepLiveSession, "a live session works in the worktree")
		return
	}
	wt, listed, err := findLinkedWorktree(ctx, dir, dir)
	if err != nil {
		keep(taskKeepUnverified, "the worktree list could not be read: "+err.Error())
		return
	}
	if !listed || wt.primary {
		keep(taskKeepUnverified, "the folder is not a linked worktree")
		return
	}
	head = wt.head

	// A verification script counts as safe only once an identical copy is
	// saved. Report mode writes nothing.
	saved := savedVerifyScriptsDir(wtDir, prd.ID)
	if mode == config.CleanupModeOn {
		if _, err := saveVerifyScripts(filepath.Join(dir, ".datawatch", "verify"), saved); err != nil {
			log.Printf("[autonomous] prd=%s completion: saving verification scripts: %v", prd.ID, err)
		}
	}
	own, clean, err := taskWorktreeStatus(ctx, dir, []string{saved})
	if err != nil {
		keep(taskKeepUnverified, "git status failed: "+err.Error())
		return
	}
	if len(own) > 0 {
		keep(taskKeepUncommitted, "the worktree holds uncommitted or untracked files", own...)
		return
	}
	if wt.detached {
		onBranch := false
		if tip != "" {
			_, aerr := taskGitOut(ctx, dir, "merge-base", "--is-ancestor", wt.head, tip)
			onBranch = aerr == nil
		}
		if !onBranch && commitsLanded(ctx, repo, taskDefaultBranch(ctx, repo, prd), prd, wt.head) == "" {
			keep(taskKeepDetached, "the worktree's detached HEAD holds commits that are on neither the work branch nor the default branch")
			return
		}
	}
	if mode != config.CleanupModeOn {
		audit(auditCompletionWorktreeWouldRemove, map[string]any{"reason": taskKeepReportMode,
			"detail": "cleanup_mode is report: the worktree is clean and would be removed"})
		return
	}
	// Without --force git itself refuses a worktree that changed since the
	// check. It is passed only when what is left is the daemon's own.
	args := []string{"-c", showUntrackedOverride, "worktree", "remove"}
	if !clean {
		args = append(args, "--force")
	}
	if _, err := taskGitOut(ctx, dir, append(args, dir)...); err != nil {
		keep(taskKeepRemoveFailed, err.Error())
		return
	}
	audit(auditCompletionWorktreeRemove, nil)
}
