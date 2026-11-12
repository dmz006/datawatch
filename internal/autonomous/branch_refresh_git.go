// Branch refresh git layer. BranchRefreshGit brings an Automaton work branch
// up to date with its base by merging the base into it (never a rebase or a
// history rewrite). All git work goes through a MergeGitRunner so tests can
// substitute a fake runner for git.
package autonomous

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// branchRefreshChangelog is the one file whose conflicts are resolved
// automatically, by taking the base branch's side.
const branchRefreshChangelog = "CHANGELOG.md"

// BranchRefreshGit runs the git steps of a branch refresh.
type BranchRefreshGit struct {
	git MergeGitRunner
}

// NewBranchRefreshGit returns a BranchRefreshGit over git; a nil runner uses
// the production runner.
func NewBranchRefreshGit(git MergeGitRunner) *BranchRefreshGit {
	if git == nil {
		git = NewMergeGitRunner()
	}
	return &BranchRefreshGit{git: git}
}

// CommitsBehind returns how many commits base has that HEAD lacks.
func (b *BranchRefreshGit) CommitsBehind(ctx context.Context, dir, base string) (int, error) {
	out, err := b.git.Run(ctx, dir, "rev-list", "--count", "HEAD.."+base)
	if err != nil {
		return 0, fmt.Errorf("rev-list: %w", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("rev-list count %q: %w", strings.TrimSpace(out), err)
	}
	return n, nil
}

// MainHead returns the commit id base points at.
func (b *BranchRefreshGit) MainHead(ctx context.Context, dir, base string) (string, error) {
	out, err := b.git.Run(ctx, dir, "rev-parse", base)
	if err != nil {
		return "", fmt.Errorf("rev-parse: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// Merge merges base into the branch checked out in dir with a no-fast-forward
// merge. A conflict confined to CHANGELOG.md is resolved by taking base's
// side of that file only and committing. Any other conflicted file aborts the
// merge, leaving the branch exactly as it was, and its name is returned in
// conflictFiles with a nil error. Files the Automaton did not change are
// never rewritten by this layer.
func (b *BranchRefreshGit) Merge(ctx context.Context, dir, base string) (conflictFiles []string, err error) {
	_, mergeErr := b.git.Run(ctx, dir, "merge", "--no-ff", "--no-edit", base)
	if mergeErr == nil {
		return nil, nil
	}
	out, lerr := b.git.Run(ctx, dir, "diff", "--name-only", "--diff-filter=U")
	if lerr != nil {
		b.abort(ctx, dir)
		return nil, fmt.Errorf("merge %s: %w (listing conflicts: %v)", base, mergeErr, lerr)
	}
	conflicted := splitLines(out)
	if len(conflicted) == 0 {
		// Failed without a conflict (dirty tree, bad ref): nothing to resolve.
		b.abort(ctx, dir)
		return nil, fmt.Errorf("merge %s: %w", base, mergeErr)
	}
	var others []string
	for _, f := range conflicted {
		if f != branchRefreshChangelog {
			others = append(others, f)
		}
	}
	if len(others) > 0 {
		if _, aerr := b.git.Run(ctx, dir, "merge", "--abort"); aerr != nil {
			return others, fmt.Errorf("merge --abort: %w", aerr)
		}
		return others, nil
	}
	for _, args := range [][]string{
		{"checkout", "--theirs", "--", branchRefreshChangelog},
		{"add", "--", branchRefreshChangelog},
		{"commit", "--no-edit"},
	} {
		if _, rerr := b.git.Run(ctx, dir, args...); rerr != nil {
			b.abort(ctx, dir)
			return nil, fmt.Errorf("resolve %s (git %s): %w", branchRefreshChangelog, args[0], rerr)
		}
	}
	return nil, nil
}

// abort backs out an in-progress merge, ignoring errors (best effort).
func (b *BranchRefreshGit) abort(ctx context.Context, dir string) {
	_, _ = b.git.Run(ctx, dir, "merge", "--abort")
}
