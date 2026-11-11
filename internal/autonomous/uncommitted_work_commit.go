// Uncommitted-work diff and commit for one Automaton worktree: what the
// operator reads before pressing Commit, and the commit itself.
//
// Both calls work on a single worktree and refuse a directory that is not
// inside the Automaton worktree base, so a bad path can never reach another
// repository. The commit stages exactly the files the uncommitted-work check
// lists (never a blanket add) and refuses an empty message.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Errors the diff and commit return for a rejected request.
var (
	ErrUncommittedEmptyMessage = errors.New("commit message is empty")
	ErrUncommittedOutsideBase  = errors.New("worktree is outside the Automaton worktree base")
	ErrUncommittedNothing      = errors.New("the worktree has no uncommitted files")
)

// checkWorktreeUnderBase resolves worktree and base (following symlinks) and
// returns the resolved worktree when it is a directory strictly inside base
// and is itself the top of a git working tree.
func checkWorktreeUnderBase(ctx context.Context, base, worktree string) (string, error) {
	if strings.TrimSpace(base) == "" || strings.TrimSpace(worktree) == "" {
		return "", fmt.Errorf("%w: base and worktree are both required", ErrUncommittedOutsideBase)
	}
	rb, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", fmt.Errorf("worktree base %s: %w", base, err)
	}
	rw, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		return "", fmt.Errorf("worktree %s: %w", worktree, err)
	}
	rb, rw = filepath.Clean(rb), filepath.Clean(rw)
	rel, err := filepath.Rel(rb, rw)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: %s", ErrUncommittedOutsideBase, worktree)
	}
	top, err := runGitInCtx(ctx, rw, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("worktree %s is not a git working tree: %w", worktree, err)
	}
	if rt, rerr := filepath.EvalSymlinks(strings.TrimSpace(top)); rerr != nil || filepath.Clean(rt) != rw {
		return "", fmt.Errorf("%w: %s is not the top of a worktree", ErrUncommittedOutsideBase, worktree)
	}
	return rw, nil
}

// uncommittedPathsInside returns the paths of files and fails when one would
// leave the worktree.
func uncommittedPathsInside(files []UncommittedFile) ([]string, error) {
	paths := make([]string, 0, len(files))
	for _, f := range files {
		c := filepath.Clean(f.Path)
		if filepath.IsAbs(c) || c == ".." || strings.HasPrefix(c, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("%w: file %q", ErrUncommittedOutsideBase, f.Path)
		}
		paths = append(paths, f.Path)
	}
	return paths, nil
}

// UncommittedDiff returns the diff of the uncommitted files of one Automaton
// worktree: tracked changes against HEAD and the full content of new files.
// It lists the same files the uncommitted-work check lists, so the operator
// sees what Commit would record. It changes nothing in the worktree. An empty
// result means the worktree is clean.
func UncommittedDiff(ctx context.Context, base, worktree string) (string, error) {
	dir, err := checkWorktreeUnderBase(ctx, base, worktree)
	if err != nil {
		return "", err
	}
	files, err := DetectUncommittedWork(ctx, dir, nil)
	if err != nil {
		return "", err
	}
	if _, err := uncommittedPathsInside(files); err != nil {
		return "", err
	}
	var b strings.Builder
	for _, f := range files {
		if f.Status == "??" {
			b.WriteString(newFileDiff(ctx, dir, f.Path))
			continue
		}
		out, err := runGitInCtx(ctx, dir, "diff", "HEAD", "--no-color", "--", f.Path)
		if err != nil {
			return "", fmt.Errorf("diff of %s in %s: %w: %s", f.Path, worktree, err, strings.TrimSpace(out))
		}
		b.WriteString(out)
	}
	return b.String(), nil
}

// newFileDiff renders an untracked file as an added-file diff. git diff
// --no-index exits 1 when the files differ, which is the normal result.
func newFileDiff(ctx context.Context, dir, rel string) string {
	cmd := exec.CommandContext(ctx, "git", "diff", "--no-index", "--no-color", "--", os.DevNull, rel) // #nosec G204 -- fixed git subcommand; rel is a path git status reported inside the worktree
	cmd.Dir = dir
	cmd.WaitDelay = mergeGitWaitDelay
	out, _ := cmd.Output()
	return string(out)
}

// CommitUncommitted stages and commits the uncommitted files of one Automaton
// worktree with message and returns the new HEAD sha. It stages exactly the
// files the uncommitted-work check lists, with explicit paths. An empty
// message, a worktree outside base, and a clean worktree are all rejected
// before anything is staged. A failing commit hook is a failed commit.
func CommitUncommitted(ctx context.Context, base, worktree, message string) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", ErrUncommittedEmptyMessage
	}
	dir, err := checkWorktreeUnderBase(ctx, base, worktree)
	if err != nil {
		return "", err
	}
	files, err := DetectUncommittedWork(ctx, dir, nil)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", ErrUncommittedNothing
	}
	paths, err := uncommittedPathsInside(files)
	if err != nil {
		return "", err
	}
	return DefaultTaskCommit(ctx, dir, paths, message)
}
