// The unlanded-work housekeeping surface on the API wrapper. The server
// (internal/server/unlanded_work_api.go) type-asserts its AutonomousAPI to the
// unlandedWorkOps interface, and the concrete type it gets is this wrapper,
// not the *Manager — so every method here delegates to the wrapped Manager
// unchanged. Without these seven methods the assertion fails and the whole
// housekeeping action list returns 503 "Housekeeping actions unavailable".

package autonomous

import (
	"context"
)

// UnlandedWork lists the archived Automaton branches with work not on the
// default branch (see Manager.UnlandedWork).
func (a *API) UnlandedWork(ctx context.Context) ([]UnlandedBranch, error) {
	return a.M.UnlandedWork(ctx)
}

// KeepUnlanded records that a listed branch is to be left alone.
func (a *API) KeepUnlanded(ctx context.Context, branch, note string) error {
	return a.M.KeepUnlanded(ctx, branch, note)
}

// ArchiveUnlanded moves a listed branch under the archive refs.
func (a *API) ArchiveUnlanded(ctx context.Context, branch string) (string, error) {
	return a.M.ArchiveUnlanded(ctx, branch)
}

// DeleteUnlandedWorktree backs up and deletes a listed branch and worktree.
func (a *API) DeleteUnlandedWorktree(ctx context.Context, branch string, confirm bool) (DeleteWorktreeResult, error) {
	return a.M.DeleteUnlandedWorktree(ctx, branch, confirm)
}

// UncommittedWorkDiff returns the diff of one worktree's uncommitted files.
func (a *API) UncommittedWorkDiff(ctx context.Context, worktree string) (string, error) {
	return a.M.UncommittedWorkDiff(ctx, worktree)
}

// CommitUncommittedWork commits one worktree's uncommitted files.
func (a *API) CommitUncommittedWork(ctx context.Context, worktree, message string) (string, error) {
	return a.M.CommitUncommittedWork(ctx, worktree, message)
}

// FixPermissions fixes read-only permission denials on listed paths.
func (a *API) FixPermissions(paths []string) (int, error) {
	return a.M.FixPermissions(paths)
}
