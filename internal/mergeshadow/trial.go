package mergeshadow

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// TrialMerge merges workBranch into defaultBranch inside a throwaway git
// worktree and returns the paths that conflict. Nothing is committed.
//
// The worktree is created under scratchDir (a directory inside the daemon data
// directory, supplied by the caller), checked out detached at defaultBranch,
// and merged with --no-commit --no-ff. It is removed and pruned on every exit
// path, including errors and cancellation. No branch, tag or other ref of the
// real repository is updated, and nothing is pushed.
//
// A clean merge returns an empty (non-nil) slice.
func TrialMerge(ctx context.Context, repoPath, defaultBranch, workBranch, scratchDir string) (conflicts []string, err error) {
	return TrialMergeWith(ctx, repoPath, defaultBranch, workBranch, scratchDir, nil)
}

// TrialMergeWith is TrialMerge with a hook. When inside is not nil it runs
// after the merge has been staged and the conflicted paths are known, while
// the trial worktree still exists, so the caller can run gates against the
// merged tree. inside receives the worktree path and the conflicted paths;
// an error it returns is returned with the conflicts. The worktree is removed
// once inside returns, on every path.
func TrialMergeWith(ctx context.Context, repoPath, defaultBranch, workBranch, scratchDir string, inside func(worktree string, conflicts []string) error) (conflicts []string, err error) {
	if repoPath == "" || defaultBranch == "" || workBranch == "" || scratchDir == "" {
		return nil, errors.New("mergeshadow: repo path, branches and scratch directory are required")
	}
	for _, b := range []string{defaultBranch, workBranch} {
		if strings.HasPrefix(b, "-") {
			return nil, fmt.Errorf("mergeshadow: invalid branch name %q", b)
		}
	}
	absRepo, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, err
	}
	absScratch, err := filepath.Abs(scratchDir)
	if err != nil {
		return nil, err
	}
	if rel, rerr := filepath.Rel(absRepo, absScratch); rerr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, errors.New("mergeshadow: scratch directory must be outside the project tree")
	}
	if err := os.MkdirAll(absScratch, 0o750); err != nil {
		return nil, err
	}
	holder, err := os.MkdirTemp(absScratch, "trial-merge-")
	if err != nil {
		return nil, err
	}
	wt := filepath.Join(holder, "wt")

	added := false
	defer func() {
		// Cleanup must survive a cancelled ctx, so it uses its own context.
		bg := context.Background()
		if added {
			_, _ = runGit(bg, absRepo, "worktree", "remove", "--force", wt)
		}
		_ = os.RemoveAll(holder)
		_, _ = runGit(bg, absRepo, "worktree", "prune")
	}()

	// --detach: the checkout never creates or moves a branch, and works even
	// when the default branch is already checked out in the real repository.
	if _, err := runGit(ctx, absRepo, "worktree", "add", "--detach", wt, defaultBranch); err != nil {
		added = dirExists(wt)
		return nil, fmt.Errorf("mergeshadow: create worktree: %w", err)
	}
	added = true

	_, mergeErr := runGit(ctx, wt, "-c", "user.name=datawatch", "-c", "user.email=datawatch@localhost",
		"merge", "--no-commit", "--no-ff", workBranch)
	if cerr := ctx.Err(); cerr != nil {
		return nil, cerr
	}
	out, lerr := runGit(ctx, wt, "diff", "--name-only", "--diff-filter=U", "-z")
	if lerr != nil {
		return nil, fmt.Errorf("mergeshadow: list conflicts: %w", lerr)
	}
	conflicts = []string{}
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			conflicts = append(conflicts, p)
		}
	}
	if mergeErr != nil && len(conflicts) == 0 {
		return nil, fmt.Errorf("mergeshadow: trial merge: %w", mergeErr)
	}
	if inside != nil {
		if ierr := inside(wt, conflicts); ierr != nil {
			return conflicts, ierr
		}
	}
	return conflicts, nil
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
