// BL407 Phase 1 — local git-worktree isolation. A PRD that would
// otherwise run bare in whatever ProjectDir happened to be set (or,
// for self-build/dogfooding work, directly in the daemon's own
// checkout) instead gets its own git worktree on its own branch,
// sharing the base repo's .git object store. See
// docs/plans/2026-10-10-prd-git-workflow.md Decisions 1-2 and 6.
package autonomous

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// branchNameFor derives the deterministic branch name a PRD's
// worktree (and, once Phase 4 lands, cluster-dispatched clone) is
// checked out on: automaton/<prd-id>-<slug>. Kept under ~60 chars —
// long enough to be useful, short enough that `git branch -a` output
// stays readable.
func branchNameFor(prd *PRD) string {
	name := "automaton/" + prd.ID
	if slug := slugify(prd.Title); slug != "" {
		name += "-" + slug
	}
	if len(name) > 60 {
		name = name[:60]
	}
	return strings.TrimRight(name, "-")
}

var slugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	return strings.Trim(slugNonAlnum.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// defaultWorktreeDir is used when Config.WorktreeDir is empty.
func defaultWorktreeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".datawatch", "prd-worktrees")
	}
	return filepath.Join(home, ".datawatch", "prd-worktrees")
}

// defaultBranchOf resolves repoDir's current default branch (e.g.
// "main"). Falls back to "main" on any error — same default
// cmd/datawatch/main.go's openTestingTrackerPR already uses — rather
// than failing worktree creation over a cosmetic lookup.
func defaultBranchOf(repoDir string) string {
	out, err := runGitIn(repoDir, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil {
		if ref := strings.TrimSpace(out); ref != "" {
			if idx := strings.LastIndex(ref, "/"); idx >= 0 {
				return ref[idx+1:]
			}
		}
	}
	return "main"
}

// EnsureWorktree creates (or, on a resumed run after a daemon
// restart, reuses) a per-PRD git worktree under worktreeDir, checked
// out on a new branch off baseRepo. Idempotent: called again for a
// PRD that already has Git.Branch set and a live worktree at the
// expected path, it's a no-op that just returns the existing values.
func EnsureWorktree(prd *PRD, baseRepo, worktreeDir string) (path, branch string, err error) {
	baseRepo = strings.TrimSpace(baseRepo)
	if baseRepo == "" {
		return "", "", fmt.Errorf("worktree: base repo not configured")
	}
	if worktreeDir = strings.TrimSpace(worktreeDir); worktreeDir == "" {
		worktreeDir = defaultWorktreeDir()
	}
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		return "", "", fmt.Errorf("worktree: mkdir %s: %w", worktreeDir, err)
	}
	path = filepath.Join(worktreeDir, prd.ID)

	if prd.Git.Branch != "" {
		if st, serr := os.Stat(filepath.Join(path, ".git")); serr == nil {
			_ = st
			return path, prd.Git.Branch, nil
		}
	}

	branch = prd.Git.Branch
	if branch == "" {
		branch = branchNameFor(prd)
	}
	base := strings.TrimSpace(prd.Git.BaseBranch)
	if base == "" {
		base = defaultBranchOf(baseRepo)
	}

	if out, werr := runGitIn(baseRepo, "worktree", "add", "-b", branch, path, base); werr != nil {
		// The branch may already exist from a prior attempt that got
		// this far and then crashed before SavePRD persisted
		// Git.Branch (e.g. daemon killed mid-Run). Retry checking out
		// the existing branch instead of creating a new one.
		if out2, werr2 := runGitIn(baseRepo, "worktree", "add", path, branch); werr2 != nil {
			return "", "", fmt.Errorf("worktree add (%s): %s / retry on existing branch: %s: %w", branch, out, out2, werr2)
		}
	}
	return path, branch, nil
}

func runGitIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...) // #nosec G204 -- fixed git subcommands; dir/branch/path come from operator-controlled config and PRD fields, same trust model as every other git shell-out in this codebase (e.g. session.ProjectGit)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
