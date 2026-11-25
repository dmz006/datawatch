// Recovery steps that run just before a verified task branch is merged into
// the PRD branch (task_merge.go). Two states used to leave a completed task
// retrying its merge forever, every pass, with nothing for the operator to do
// but repair the tree by hand:
//
//   - the task branch is missing from the base repository although the task's
//     own worktree still holds the finished commit (a detached HEAD, or a
//     worktree that is a clone of its own, so the commit is not in the base
//     repository at all). The branch is recreated at that commit, bringing the
//     commit over first when it is only in the task worktree.
//   - an untracked file in the PRD worktree has the path of a file the task
//     branch adds, so git refuses the merge ("untracked working tree files
//     would be overwritten"). A copy that is identical to the branch's file is
//     dropped; a different copy is moved aside, never deleted, under
//     .datawatch/merge-aside/<task>/ in the PRD worktree.
//
// Neither step discards work: the commit is kept under the branch name, and a
// differing file is preserved in the aside folder.
package autonomous

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// taskBranchExists reports whether the base repository has a local branch of
// that name.
func taskBranchExists(ctx context.Context, dir, branch string) bool {
	_, err := runGitInCtx(ctx, dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// syncTaskBranchToWorktree makes the base repository's branch for t point at
// the commit the task worktree finished on. A missing branch is created. An
// existing branch is only moved forward, and only when the task worktree has no
// branch of its own checked out (a detached HEAD), because a task worktree on
// the branch moves the shared ref by itself. The commit is fetched from the
// task worktree first when the base repository does not have it.
func (m *Manager) syncTaskBranchToWorktree(ctx context.Context, prd *PRD, t *Task) error {
	dir := prd.ProjectDir
	branch := strings.TrimSpace(t.WorktreeBranch)
	exists := taskBranchExists(ctx, dir, branch)
	tw := strings.TrimSpace(t.WorktreePath)
	if tw == "" || !pathExists(filepath.Join(tw, ".git")) {
		if exists {
			return nil
		}
		return fmt.Errorf("task branch %s does not exist and the task worktree %q is gone, so the finished work cannot be recovered", branch, tw)
	}
	sha := strings.TrimSpace(gitText(ctx, tw, "rev-parse", "HEAD"))
	if sha == "" {
		if exists {
			return nil
		}
		return fmt.Errorf("task branch %s does not exist and the task worktree %s has no commit", branch, tw)
	}
	if exists {
		tip := strings.TrimSpace(gitText(ctx, dir, "rev-parse", "refs/heads/"+branch))
		if tip == sha {
			return nil
		}
		// A task worktree on a branch of its own moves the shared ref itself.
		if _, err := runGitInCtx(ctx, tw, "symbolic-ref", "-q", "HEAD"); err == nil {
			return nil
		}
		if !m.commitInRepo(ctx, dir, sha) {
			if out, err := runGitInCtx(ctx, dir, "fetch", "--quiet", "--no-tags", tw, "HEAD"); err != nil {
				return fmt.Errorf("fetch task commit %s from %s: %s: %w", sha, tw, strings.TrimSpace(out), err)
			}
		}
		if _, err := runGitInCtx(ctx, dir, "merge-base", "--is-ancestor", tip, sha); err != nil {
			return nil // diverged: leave the branch alone, the merge reports it
		}
		if out, err := runGitInCtx(ctx, dir, "branch", "-f", branch, sha); err != nil {
			return fmt.Errorf("move task branch %s to %s: %s: %w", branch, sha, strings.TrimSpace(out), err)
		}
		log.Printf("[autonomous] %s task %s: moved branch %s forward to the task worktree commit %s", prd.ID, t.ID, branch, sha)
		return nil
	}
	if !m.commitInRepo(ctx, dir, sha) {
		if out, err := runGitInCtx(ctx, dir, "fetch", "--quiet", "--no-tags", tw, "HEAD"); err != nil {
			return fmt.Errorf("fetch task commit %s from %s: %s: %w", sha, tw, strings.TrimSpace(out), err)
		}
	}
	if out, err := runGitInCtx(ctx, dir, "branch", branch, sha); err != nil {
		return fmt.Errorf("recreate task branch %s at %s: %s: %w", branch, sha, strings.TrimSpace(out), err)
	}
	log.Printf("[autonomous] %s task %s: recreated the missing branch %s at the task worktree commit %s", prd.ID, t.ID, branch, sha)
	return nil
}

// commitInRepo reports whether dir's repository has the commit.
func (m *Manager) commitInRepo(ctx context.Context, dir, sha string) bool {
	_, err := runGitInCtx(ctx, dir, "cat-file", "-e", sha+"^{commit}")
	return err == nil
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// clearOverwritableUntracked removes or moves aside every untracked file of dir
// that the merge of branch would overwrite. It returns the paths dropped
// because they were identical to the branch's file, and for each path moved
// aside where its content is now. A file moved aside earlier is never
// replaced (moveKeepingBoth).
func clearOverwritableUntracked(ctx context.Context, dir, branch, taskID string) (dropped []string, movedAside []movedFile, err error) {
	untracked := splitNul(gitText(ctx, dir, "ls-files", "--others", "--exclude-standard", "-z"))
	if len(untracked) == 0 {
		return nil, nil, nil
	}
	adds := map[string]bool{}
	for _, p := range splitNul(gitText(ctx, dir, "diff", "--name-only", "-z", "--diff-filter=A", "HEAD", branch)) {
		adds[p] = true
	}
	for _, p := range untracked {
		if !adds[p] {
			continue
		}
		full, jerr := safeJoin(dir, p)
		if jerr != nil {
			continue
		}
		if st, serr := os.Lstat(full); serr != nil || !st.Mode().IsRegular() {
			continue
		}
		mine := strings.TrimSpace(gitText(ctx, dir, "hash-object", "--", full))
		theirs := strings.TrimSpace(gitText(ctx, dir, "rev-parse", branch+":"+p))
		if mine != "" && mine == theirs {
			if rerr := os.Remove(full); rerr != nil {
				return dropped, movedAside, fmt.Errorf("remove the untracked copy of %s: %w", p, rerr)
			}
			dropped = append(dropped, p)
			continue
		}
		aside, jerr := safeJoin(filepath.Join(dir, ".datawatch", "merge-aside", taskID), p)
		if jerr != nil {
			return dropped, movedAside, jerr
		}
		if merr := os.MkdirAll(filepath.Dir(aside), 0o755); merr != nil {
			return dropped, movedAside, fmt.Errorf("make the aside folder for %s: %w", p, merr)
		}
		to, identical, rerr := moveKeepingBoth(full, aside, nil)
		if rerr != nil {
			return dropped, movedAside, fmt.Errorf("move %s aside: %w", p, rerr)
		}
		if rel, relErr := filepath.Rel(dir, to); relErr == nil {
			to = filepath.ToSlash(rel)
		}
		movedAside = append(movedAside, movedFile{From: p, To: to, Identical: identical})
	}
	return dropped, movedAside, nil
}

func splitNul(s string) []string {
	var out []string
	for _, p := range strings.Split(s, "\x00") {
		if p = strings.Trim(p, "\r\n"); p != "" {
			out = append(out, p)
		}
	}
	return out
}
