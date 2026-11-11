// Manager entry points for the Housekeeping action list: the unlanded branch
// list and its actions, the uncommitted-work diff and commit, and the
// permission fix. Each one wires the stored Automata, the configured base
// repository and the worktree base folder to the plain functions in this
// package. When no base repository is configured every read is an empty list
// and every action reports that nothing is configured.
package autonomous

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoBaseRepo is returned by an action when no base repository is set.
var ErrNoBaseRepo = errors.New("no base repository is configured")

// ErrUnlandedNotListed is returned when a branch is not in the unlanded list.
var ErrUnlandedNotListed = errors.New("branch is not in the unlanded list")

func (m *Manager) housekeepingPaths() (repo, wtBase, stateDir string) {
	m.mu.Lock()
	repo = strings.TrimSpace(m.cfg.WorktreeBaseRepo)
	wtBase = strings.TrimSpace(m.cfg.WorktreeDir)
	m.mu.Unlock()
	if wtBase == "" {
		wtBase = defaultWorktreeDir(m.dataDir)
	}
	return repo, wtBase, m.dataDir
}

// UnlandedWork lists the archived Automaton branches with work not on the
// default branch, leaving out branches the operator chose to keep. Empty when
// there is no base repository or no store.
func (m *Manager) UnlandedWork(ctx context.Context) ([]UnlandedBranch, error) {
	out := []UnlandedBranch{}
	if m == nil || m.store == nil {
		return out, nil
	}
	repo, _, stateDir := m.housekeepingPaths()
	if repo == "" {
		return out, nil
	}
	list, err := ListUnlandedBranches(ctx, NewMergeGitRunner(), repo, m.store.ListPRDs())
	if err != nil {
		return out, err
	}
	for _, b := range list {
		if !UnlandedKept(stateDir, b.Branch) {
			out = append(out, b)
		}
	}
	return out, nil
}

// KeepUnlanded records a keep decision for a listed branch.
func (m *Manager) KeepUnlanded(ctx context.Context, branch, note string) error {
	if _, err := m.listedUnlanded(ctx, branch); err != nil {
		return err
	}
	_, _, stateDir := m.housekeepingPaths()
	return Keep(stateDir, branch, note)
}

// ArchiveUnlanded moves a listed branch under the archive ref namespace.
func (m *Manager) ArchiveUnlanded(ctx context.Context, branch string) (string, error) {
	if _, err := m.listedUnlanded(ctx, branch); err != nil {
		return "", err
	}
	repo, _, _ := m.housekeepingPaths()
	return ArchiveBranch(ctx, NewMergeGitRunner(), repo, branch)
}

// DeleteUnlandedWorktree backs up and deletes a listed branch and worktree.
func (m *Manager) DeleteUnlandedWorktree(ctx context.Context, branch string, confirm bool) (DeleteWorktreeResult, error) {
	entry, err := m.listedUnlanded(ctx, branch)
	if err != nil {
		return DeleteWorktreeResult{Branch: branch}, err
	}
	repo, wtBase, _ := m.housekeepingPaths()
	return DeleteWorktree(ctx, NewMergeGitRunner(), DeleteWorktreeOptions{
		Branch: entry, Confirm: confirm, BaseRepo: repo, WorktreeBase: wtBase,
	})
}

func (m *Manager) listedUnlanded(ctx context.Context, branch string) (UnlandedBranch, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return UnlandedBranch{}, errors.New("branch is required")
	}
	if m == nil || m.store == nil {
		return UnlandedBranch{}, ErrNoBaseRepo
	}
	if repo, _, _ := m.housekeepingPaths(); repo == "" {
		return UnlandedBranch{}, ErrNoBaseRepo
	}
	list, err := m.UnlandedWork(ctx)
	if err != nil {
		return UnlandedBranch{}, err
	}
	for _, b := range list {
		if b.Branch == branch {
			return b, nil
		}
	}
	return UnlandedBranch{}, ErrUnlandedNotListed
}

// UncommittedWorkDiff returns the diff of one worktree under the worktree base.
func (m *Manager) UncommittedWorkDiff(ctx context.Context, worktree string) (string, error) {
	_, wtBase, _ := m.housekeepingPaths()
	return UncommittedDiff(ctx, wtBase, worktree)
}

// CommitUncommittedWork commits the uncommitted files of one worktree.
func (m *Manager) CommitUncommittedWork(ctx context.Context, worktree, message string) (string, error) {
	_, wtBase, _ := m.housekeepingPaths()
	return CommitUncommitted(ctx, wtBase, worktree, message)
}

// FixPermissions restores owner write permission on the given paths, or on
// every folder directly under the worktree base when none are given. It
// returns the number of entries changed.
func (m *Manager) FixPermissions(paths []string) (int, error) {
	_, wtBase, _ := m.housekeepingPaths()
	if len(paths) == 0 {
		entries, err := os.ReadDir(wtBase)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return 0, nil
			}
			return 0, err
		}
		for _, e := range entries {
			if e.IsDir() {
				paths = append(paths, filepath.Join(wtBase, e.Name()))
			}
		}
	}
	return FixCleanupPermissions(wtBase, paths)
}
