// Per-task git worktrees. With autonomous.parallel.worktree_per_task on, a
// task of a worktree-mode PRD works in its own linked worktree on its own
// branch, cut from the PRD branch HEAD when the task starts (so everything
// its dependencies merged is already in it). Sibling writers then key the
// tree lock on different paths and do not contend.
package autonomous

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// maxTaskSlugLen bounds the title part of a task branch / directory name.
const maxTaskSlugLen = 40

// taskWorktreeRoot is the directory holding one PRD's task worktrees, a
// sibling of the PRD worktree under the worktree base.
func taskWorktreeRoot(worktreeDir string, prd *PRD) string {
	if worktreeDir = strings.TrimSpace(worktreeDir); worktreeDir == "" {
		worktreeDir = defaultWorktreeDir("")
	}
	return filepath.Join(worktreeDir, prd.ID+"-tasks")
}

// taskSlug names a task for use in a branch and a directory: the slugified
// title, shortened, plus the task id so two tasks with one title differ.
func taskSlug(task *Task) string {
	s := slugify(task.Title)
	if len(s) > maxTaskSlugLen {
		s = strings.Trim(s[:maxTaskSlugLen], "-")
	}
	id := slugify(task.ID)
	switch {
	case s == "":
		return id
	case id == "":
		return s
	}
	return s + "-" + id
}

// taskBranchFor derives a task's branch from its PRD's branch. Git cannot
// hold a branch "x" and a branch "x/y" together (the ref "x" is a file where
// "x/y" needs a directory), so the task slug is joined to the PRD branch
// with "--tasks-" rather than a slash.
func taskBranchFor(prdBranch string, task *Task) string {
	return prdBranch + "--tasks-" + taskSlug(task)
}

// EnsureTaskWorktree creates (or reuses, after a restart) the worktree of one
// task of prd under the worktree base, on a task branch cut from the PRD
// branch HEAD. baseRepo is the repository the PRD worktree belongs to.
// Idempotent: a task whose worktree is already there gets it back unchanged.
func EnsureTaskWorktree(prd *PRD, task *Task, baseRepo, worktreeDir string) (path, branch string, err error) {
	if prd == nil || task == nil {
		return "", "", fmt.Errorf("task worktree: prd and task are required")
	}
	baseRepo = strings.TrimSpace(baseRepo)
	if baseRepo == "" {
		return "", "", fmt.Errorf("task worktree: base repo not configured")
	}
	if prd.Git.Branch == "" {
		return "", "", fmt.Errorf("task worktree: prd %s has no branch (not in worktree mode)", prd.ID)
	}
	slug := taskSlug(task)
	if slug == "" {
		return "", "", fmt.Errorf("task worktree: task has no id or title to name it by")
	}
	root := taskWorktreeRoot(worktreeDir, prd)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", "", fmt.Errorf("task worktree: mkdir %s: %w", root, err)
	}
	path = filepath.Join(root, slug)
	// Containment: the path must stay inside the PRD's task root.
	if rel, rerr := filepath.Rel(root, path); rerr != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("task worktree: path %s escapes %s", path, root)
	}
	branch = taskBranchFor(prd.Git.Branch, task)

	if _, serr := os.Stat(filepath.Join(path, ".git")); serr == nil {
		if cerr := CheckProjectWorktree(path, baseRepo); cerr != nil {
			return "", "", fmt.Errorf("task worktree reuse refused: %w", cerr)
		}
		prepareWorktreeBestEffort(path)
		return path, branch, nil
	}

	if out, werr := runGitIn(baseRepo, "worktree", "add", "-b", branch, path, prd.Git.Branch); werr != nil {
		// The branch may be left over from an attempt that crashed before
		// the worktree was recorded: check the existing branch out instead.
		if out2, werr2 := runGitIn(baseRepo, "worktree", "add", path, branch); werr2 != nil {
			return "", "", fmt.Errorf("task worktree add (%s): %s / retry on existing branch: %s: %w", branch, strings.TrimSpace(out), strings.TrimSpace(out2), werr2)
		}
	}
	prepareWorktreeBestEffort(path)
	restoreTaskVerifyScript(prd, task, path, worktreeDir)
	return path, branch, nil
}

// restoreTaskVerifyScript copies the task's verification script into a newly
// created task worktree, from the Automaton worktree first and then the saved
// folder. Best effort: a failure is logged, and a script found in neither
// place is left for the run-time ErrVerificationScriptMissing path.
func restoreTaskVerifyScript(prd *PRD, task *Task, path, worktreeDir string) {
	script := strings.TrimSpace(task.VerificationScript)
	if script == "" {
		return
	}
	base := filepath.Base(script)
	if strings.TrimSpace(worktreeDir) == "" {
		worktreeDir = defaultWorktreeDir("")
	}
	srcDirs := []string{
		filepath.Join(worktreeDir, prd.ID, ".datawatch", "verify"),
		savedVerifyScriptsDir(worktreeDir, prd.ID),
	}
	n, err := restoreVerifyScripts(srcDirs, path, []string{base})
	if err != nil {
		log.Printf("[autonomous] task worktree: restore verification script %s into %s failed (non-fatal): %v", base, path, err)
		return
	}
	if n > 0 {
		log.Printf("[autonomous] task worktree: restored %d verification script(s) into %s", n, path)
	}
}

// taskWorktreeEnabled reports whether t gets its own worktree: the setting is
// on, the PRD runs in worktree mode, and the task has no directory of its own.
// A read-only task keeps the PRD worktree: it writes nothing to isolate.
func (m *Manager) taskWorktreeEnabled(prd *PRD, t *Task) bool {
	if prd == nil || t == nil || prd.Git.Branch == "" || t.ProjectDir != "" || t.SpawnPRD {
		return false
	}
	if m.readOnlyTaskSkipsLock(prd, t) {
		return false
	}
	return m.effectiveWorktreePerTask(prd)
}

// ensureTaskWorktreeFor creates the task's worktree when enabled and records
// it on the task. A no-op (nil) when the feature is off.
func (m *Manager) ensureTaskWorktreeFor(prd *PRD, t *Task) error {
	if !m.taskWorktreeEnabled(prd, t) {
		return nil
	}
	m.mu.Lock()
	repo, dir := m.cfg.WorktreeBaseRepo, m.cfg.WorktreeDir
	m.mu.Unlock()
	if repo == "" {
		return nil
	}
	if strings.TrimSpace(dir) == "" {
		dir = defaultWorktreeDir(m.dataDir)
	}
	// A PRD worktree that is not a valid linked worktree is for the launch
	// guard to refuse; do not mask that refusal with a worktree error here.
	if CheckProjectWorktree(prd.ProjectDir, repo) != nil {
		return nil
	}
	path, branch, err := EnsureTaskWorktree(prd, t, repo, dir)
	if err != nil {
		return err
	}
	m.store.WithLock(func() { t.WorktreePath, t.WorktreeBranch = path, branch })
	_ = m.store.SaveTask(t)
	return nil
}

// taskWorktreeInUse reports whether a live session works in path.
func (m *Manager) taskWorktreeInUse(path string) bool {
	m.mu.Lock()
	listFn := m.sessionListerFn
	m.mu.Unlock()
	if listFn == nil {
		return false
	}
	want := absClean(path)
	for _, s := range listFn() {
		if s.Alive && s.ProjectDir != "" && absClean(s.ProjectDir) == want {
			return true
		}
	}
	return false
}

// removeTaskWorktree removes one task worktree and its branch, unless a live
// session uses it. Returns whether it was removed.
func (m *Manager) removeTaskWorktree(ctx context.Context, repo string, t *Task) bool {
	if t == nil || t.WorktreePath == "" {
		return false
	}
	if m.taskWorktreeInUse(t.WorktreePath) {
		return false
	}
	if _, err := runGitInCtx(ctx, repo, "worktree", "remove", "--force", t.WorktreePath); err != nil {
		if _, serr := os.Stat(t.WorktreePath); serr == nil {
			return false // still there and git would not remove it
		}
	}
	if t.WorktreeBranch != "" {
		_, _ = runGitInCtx(ctx, repo, "branch", "-D", t.WorktreeBranch)
	}
	m.store.WithLock(func() { t.WorktreePath, t.WorktreeBranch = "", "" })
	_ = m.store.SaveTask(t)
	return true
}

// taskBranchMerged reports whether the task branch is already on the PRD branch.
func taskBranchMerged(ctx context.Context, repo, taskBranch, prdBranch string) bool {
	if taskBranch == "" || prdBranch == "" {
		return false
	}
	_, err := runGitInCtx(ctx, repo, "merge-base", "--is-ancestor", taskBranch, prdBranch)
	return err == nil
}

// CleanupTaskWorktrees removes the task worktrees of prd: those whose branch
// is merged into the PRD branch, or all of them when the PRD has ended
// (completed or cancelled). A worktree a live session uses is never removed.
// Returns how many were removed.
func (m *Manager) CleanupTaskWorktrees(ctx context.Context, prd *PRD) int {
	if prd == nil {
		return 0
	}
	m.mu.Lock()
	repo := m.cfg.WorktreeBaseRepo
	m.mu.Unlock()
	if repo == "" {
		return 0
	}
	ended := prd.Status == PRDCompleted || prd.Status == PRDCancelled
	n := 0
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			if t.WorktreePath == "" {
				continue
			}
			if ended || (isTerminalTaskStatus(t.Status) && taskBranchMerged(ctx, repo, t.WorktreeBranch, prd.Git.Branch)) {
				if m.removeTaskWorktree(ctx, repo, t) {
					n++
				}
			}
		}
	}
	return n
}
