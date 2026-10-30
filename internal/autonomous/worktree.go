// BL407 Phase 1 — local git-worktree isolation. A PRD that would
// otherwise run bare in whatever ProjectDir happened to be set (or,
// for self-build/dogfooding work, directly in the daemon's own
// checkout) instead gets its own git worktree on its own branch,
// sharing the base repo's .git object store. See
// docs/plans/2026-10-10-prd-git-workflow.md Decisions 1-2 and 6.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
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
func defaultWorktreeDir() string { return config.DefaultPRDWorktreeDir() }

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
		if _, serr := os.Stat(filepath.Join(path, ".git")); serr == nil {
			// A .git entry is not proof of a worktree: a standalone
			// repository initialised in the same path also has one.
			if cerr := CheckProjectWorktree(path, baseRepo); cerr != nil {
				return "", "", fmt.Errorf("worktree reuse refused: %w", cerr)
			}
			// Reused worktree: still make sure the go:embed placeholder
			// exists (a daemon restart doesn't conjure it back into
			// life if the worktree was pruned and re-added by hand).
			prepareWorktreeBestEffort(path)
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
			msg := fmt.Sprintf("worktree add (%s): %s / retry on existing branch: %s", branch, out, out2)
			if cerr := CheckProjectWorktree(path, baseRepo); cerr != nil {
				var ierr *WorktreeIntegrityError
				if errors.As(cerr, &ierr) {
					msg += " / existing directory: " + ierr.Reason
				}
			}
			return "", "", fmt.Errorf("%s: %w", msg, werr2)
		}
	}
	// Fresh worktree: drop the git-ignored go:embed placeholder in so
	// the first build doesn't fail on internal/docsindex.
	prepareWorktreeBestEffort(path)
	return path, branch, nil
}

// prepareWorktreeBestEffort runs prepareWorktree on a created/reused
// worktree and logs any failure instead of propagating it: a broken
// placeholder or exclude setup is worth seeing in the daemon log, but
// must not roll back a worktree that was otherwise created fine.
func prepareWorktreeBestEffort(path string) {
	if perr := prepareWorktree(path); perr != nil {
		log.Printf("[autonomous] worktree prepare at %s (non-fatal): %v", path, perr)
	}
}

// ensureWorktreeIfConfigured is the shared gate Run() (BL407 Phase 1)
// and Decompose()/DecomposeStreaming() (this fix, found live
// 2026-10-11 while creating BL404's own PRD) all call: when a PRD has
// no explicit ProjectDir/ProjectProfile/ClusterProfile and the
// operator has configured WorktreeBaseRepo, create (or, on a later
// call for the same PRD, reuse — EnsureWorktree is idempotent) an
// isolated git worktree and point the PRD at it. No-op otherwise
// (worktree mode not configured, or an explicit work-source already
// set). Extracted to one place specifically to avoid a third
// hand-copied duplicate of this gating logic — Decompose() and
// DecomposeStreaming() are already a documented drifted-duplicate
// pair (see decomposeStreamingCore's own "operator-reported" comment)
// and Run() had its own third copy before this fix. created reports
// whether a worktree was actually created or reused this call, so
// callers can decide whether/how to log it (Run() and Decompose()
// want different log phrasing).
func (m *Manager) ensureWorktreeIfConfigured(prd *PRD) (created bool, err error) {
	if prd.ProjectDir != "" || prd.ProjectProfile != "" || prd.ClusterProfile != "" {
		return false, nil
	}
	m.mu.Lock()
	baseRepo := m.cfg.WorktreeBaseRepo
	worktreeDir := m.cfg.WorktreeDir
	m.mu.Unlock()
	if baseRepo == "" {
		return false, nil
	}
	path, branch, werr := EnsureWorktree(prd, baseRepo, worktreeDir)
	if werr != nil {
		return false, fmt.Errorf("worktree setup: %w", werr)
	}
	prd.ProjectDir = path
	prd.Git.Branch = branch
	return true, nil
}

func runGitIn(dir string, args ...string) (string, error) {
	return runGitInCtx(context.Background(), dir, args...)
}

// mergeGitWaitDelay bounds how long Wait lingers for git's output pipes once
// ctx has killed the process, so a grandchild holding a pipe open cannot keep
// a cancelled call from returning.
const mergeGitWaitDelay = 2 * time.Second

// runGitInCtx is runGitIn with a context: when ctx ends, the git process is
// killed and the call returns.
func runGitInCtx(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- fixed git subcommands; dir/branch/path come from operator-controlled config and PRD fields, same trust model as every other git shell-out in this codebase (e.g. session.ProjectGit)
	cmd.Dir = dir
	cmd.WaitDelay = mergeGitWaitDelay
	out, err := cmd.CombinedOutput()
	if cerr := ctx.Err(); cerr != nil && err != nil {
		return string(out), fmt.Errorf("git %s: %w", args[0], cerr)
	}
	return string(out), err
}
