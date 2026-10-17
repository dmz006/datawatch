// BL428 (design docs/plans/2026-10-14-concurrency-and-budget-design.md,
// decision 3a) — canonical project-tree key for the advisory write lock.
//
// The key identifies *which tree a lock protects*, not which PRD/task
// holds it, so that every concurrent holder of one tree agrees on a
// single advisory file an operator can find and inspect:
//
//   - worktree-mode PRDs (prd.Git.Branch != "", BL407) run in their own
//     git worktree and key per worktree;
//   - direct-mode PRDs write to the base checkout directly — a main
//     checkout and its linked worktrees (or any subdir of a checkout)
//     all resolve to the one shared base via the git common dir;
//   - non-git project directories fall back to a per-directory key.
//
// This file is the key-resolution half only; lock acquisition/release
// lands with the executor wiring.
package autonomous

import (
	"fmt"
	"path/filepath"
	"strings"
)

// treeKeyPrefix marks a key as a canonical project-tree key; the
// remainder is an absClean'd absolute path.
const treeKeyPrefix = "tree:"

// absClean resolves p to an absolute, symlink-resolved, cleaned path.
// Symlink errors (e.g. a directory not created yet) fall back to a
// plain Abs so a PRD whose project dir is pending still resolves; an
// Abs failure is defensive and returns the cleaned input unchanged.
func absClean(p string) string {
	ap, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	if rp, err := filepath.EvalSymlinks(ap); err == nil {
		ap = rp
	}
	return filepath.Clean(ap)
}

// gitCommonDir resolves repoRoot's shared git common dir, e.g.
// <base>/.git for a main checkout or linked worktree, or
// <base>/.git/modules/<sub> for a submodule. It runs
// `git -C <root> rev-parse --git-common-dir` and absolutizes the
// output — git prints a path relative to CWD when run at the repo
// root (".git") or from a subdir ("../.git"), and an absolute path
// from elsewhere.
func gitCommonDir(repoRoot string) (string, error) {
	out, err := runGitIn(repoRoot, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	// CombinedOutput interleaves stderr hints; the dir is the first
	// usable line.
	var dir string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "fatal:") || strings.HasPrefix(line, "hint:") {
			continue
		}
		dir = line
		break
	}
	if dir == "" {
		return "", fmt.Errorf("git rev-parse --git-common-dir in %s: empty output %q", repoRoot, out)
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(repoRoot, dir)
	}
	return absClean(dir), nil
}

// sharedBase returns the working-tree root that owns gitDir: the
// directory containing the .git path component. A main checkout, a
// linked worktree's common dir (<base>/.git) and a submodule
// (<base>/.git/modules/<sub>) all map back to <base>. Falls back to
// filepath.Dir for unusual layouts that carry no .git element (bare
// repos, custom GIT_DIR).
func sharedBase(gitDir string) string {
	parts := strings.Split(filepath.ToSlash(gitDir), "/")
	for i, part := range parts {
		if part == ".git" {
			return filepath.ToSlash(filepath.Join(parts[:i]...))
		}
	}
	return filepath.Dir(gitDir)
}

// ResolveTreeKey returns the canonical key the project-tree write lock
// is keyed on for this PRD/task, plus treeRoot — the absClean'd
// directory whose files the task actually writes.
//
//   - worktree mode (prd.Git.Branch != ""): per-worktree key,
//     "tree:" + absClean(root);
//   - direct mode on a git repo: key "tree:" + absClean(base) where
//     base is the shared base of the git common dir — main checkout
//     and linked worktrees of the same repo therefore share one key —
//     while treeRoot stays the actual write root;
//   - non-git fallback: per-directory key "tree:" + absClean(root).
//
// Symlinked, relative and redundant path spellings of the same
// directory all resolve to the same key (absClean). Errors when there
// is no project dir to key on.
func ResolveTreeKey(prd *PRD, task *Task) (string, string, error) {
	root := ResolveTaskProjectDir(prd, task)
	if root == "" {
		prdID, taskID := "<nil>", "<nil>"
		if prd != nil {
			prdID = prd.ID
		}
		if task != nil {
			taskID = task.ID
		}
		return "", "", fmt.Errorf("tree key: no project dir to key on (prd %s, task %s)", prdID, taskID)
	}
	treeRoot := absClean(root)

	// BL407 worktree-mode PRDs own their worktree — each worktree is
	// an independent tree, so no base canonicalization here.
	if prd != nil && prd.Git.Branch != "" {
		return treeKeyPrefix + treeRoot, treeRoot, nil
	}

	if commonDir, err := gitCommonDir(root); err == nil {
		base := absClean(sharedBase(commonDir))
		return treeKeyPrefix + base, treeRoot, nil
	}

	// Not a git repo (or git unavailable) — key per directory.
	return treeKeyPrefix + treeRoot, treeRoot, nil
}

// treeLockPath returns the advisory lock-file location for a resolved
// tree key:
//
//	<canonical tree>/.datawatch/tree.lock
//
// The canonical tree is the path carried by the key — the shared base
// for direct-mode keys (so an operator inspects one file in the main
// checkout even when a linked worktree holds the lock) and the
// worktree itself for worktree-mode keys. treeRoot is only used as a
// fallback when the key carries no usable absolute path.
func treeLockPath(key, treeRoot string) string {
	dir := strings.TrimPrefix(key, treeKeyPrefix)
	if dir == key || !filepath.IsAbs(dir) {
		dir = absClean(treeRoot)
	}
	return filepath.Join(dir, ".datawatch", "tree.lock")
}
