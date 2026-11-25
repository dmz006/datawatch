// Releasing the Automaton worktree: after completion with an automatic pull
// request, and in the cleanup of a rejected or archived Automaton. The same
// rules as for task worktrees (task_worktree_keep.go) apply: cleanup_mode off
// and report remove nothing; a worktree with its own uncommitted or untracked
// files, real files in an ignored folder (parked_work.go), a live session,
// detached commits of its own, or anything that cannot be checked is kept; and
// what was removed or kept is written to the audit log. Neither step deletes a
// branch.
package autonomous

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"

	"github.com/dmz006/datawatch/internal/config"
)

// automatonCheckoutStep is one of the steps that releases an Automaton
// worktree. Its name is part of its audit actions:
// autonomous_<name>_worktree_remove, _kept and _would_remove.
type automatonCheckoutStep struct {
	name string
	// mayForce lets the step pass --force when only the daemon's own files
	// are left. The reject and archive cleanup never forces.
	mayForce bool
}

var (
	completionCheckoutStep = automatonCheckoutStep{name: "completion", mayForce: true}
	orphanCheckoutStep     = automatonCheckoutStep{name: "orphan"}
)

// showUntrackedOverride makes git's own unforced "worktree remove" look for
// untracked files. With status.showUntrackedFiles=no in the repository or user
// settings, git would otherwise delete them without refusing.
const showUntrackedOverride = "status.showUntrackedFiles=normal"

// verifyCopiesFor returns the folders whose identical copy makes a
// verification script of the Automaton worktree dir safe to remove. In on mode
// the scripts are saved first, so only a script that really was saved counts.
// Report mode writes nothing and previews that: the worktree's own folder
// stands for the copies the save would make (the files directly in it).
func (m *Manager) verifyCopiesFor(prdID, wtDir, dir, mode string) []string {
	saved := savedVerifyScriptsDir(wtDir, prdID)
	own := filepath.Join(dir, ".datawatch", "verify")
	if mode != config.CleanupModeOn {
		return []string{saved, own}
	}
	if _, err := saveVerifyScripts(own, saved); err != nil {
		log.Printf("[autonomous] prd=%s worktree release: saving verification scripts: %v", prdID, err)
	}
	return []string{saved}
}

// releaseAutomatonCheckout removes prd's worktree when cleanup_mode is on and
// nothing in it would be lost. The work branch stays in every case.
func (m *Manager) releaseAutomatonCheckout(prd *PRD, step automatonCheckoutStep) {
	dir := prd.ProjectDir
	if dir == "" {
		return
	}
	_, statErr := os.Stat(dir)
	if errors.Is(statErr, os.ErrNotExist) {
		return // already gone: nothing to remove or keep
	}
	action := func(what string) string { return "autonomous_" + step.name + "_worktree_" + what }
	m.mu.Lock()
	mode := m.cfg.Automation.EffectiveCleanupMode()
	repo, wtDir := m.cfg.WorktreeBaseRepo, m.cfg.WorktreeDir
	m.mu.Unlock()
	if wtDir == "" {
		wtDir = defaultWorktreeDir(m.dataDir)
	}

	tip, head := "", ""
	audit := func(name string, extra map[string]any) {
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
		if err := m.worktreeCleanupAudit()(name, details); err != nil {
			log.Printf("[autonomous] prd=%s %s worktree: audit %s: %v", prd.ID, step.name, name, err)
		}
	}
	keep := func(reason, detail string, files ...string) {
		if len(files) > maxKeptFiles {
			files = files[:maxKeptFiles]
		}
		log.Printf("[autonomous] prd=%s %s: worktree %s kept (%s): %s", prd.ID, step.name, dir, reason, detail)
		audit(action("kept"), map[string]any{"reason": reason, "detail": detail, "files": files})
	}

	if mode == config.CleanupModeOff {
		keep(taskKeepCleanupOff, "cleanup_mode is off: nothing is looked at or removed")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), worktreeReleaseGitTimeout)
	defer cancel()

	if statErr != nil {
		keep(taskKeepUnverified, "the worktree could not be read: "+statErr.Error())
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

	own, clean, err := taskWorktreeStatus(ctx, dir, m.verifyCopiesFor(prd.ID, wtDir, dir, mode))
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
		audit(action("would_remove"), map[string]any{"reason": taskKeepReportMode,
			"detail": "cleanup_mode is report: the worktree is clean and would be removed"})
		return
	}
	// Without --force git itself refuses a worktree that changed since the
	// check. It is passed only when what is left is the daemon's own.
	args := []string{"-c", showUntrackedOverride, "worktree", "remove"}
	if !clean && step.mayForce {
		args = append(args, "--force")
	}
	if _, err := taskGitOut(ctx, dir, append(args, dir)...); err != nil {
		keep(taskKeepRemoveFailed, err.Error())
		return
	}
	audit(action("remove"), nil)
}
