// The housekeeping action-list surface forwarded through the API wrapper.
//
// The REST server (internal/server) hands its manager surface to the routes
// as an asserted interface; cmd/datawatch wires that surface as *API, the
// wrapper around *Manager. Without the methods below the assertion would
// fail on the live daemon and every housekeeping route would answer 503
// "Housekeeping actions unavailable". Each method here has the identical
// signature to its Manager counterpart in unlanded_manager.go and simply
// forwards to a.M.
package autonomous

import (
	"context"
	"errors"
)

// errHousekeepingNotConfigured is returned by a forwarding method when the
// wrapper was constructed without a Manager.
var errHousekeepingNotConfigured = errors.New("autonomous housekeeping is unavailable: no manager is configured")

// UnlandedWork lists the archived Automaton branches with work not on the
// default branch. With no Manager configured it reads as an empty list,
// matching the Manager's off-state contract (off or empty is never an
// error).
func (a *API) UnlandedWork(ctx context.Context) ([]UnlandedBranch, error) {
	if a == nil || a.M == nil {
		return []UnlandedBranch{}, nil
	}
	return a.M.UnlandedWork(ctx)
}

// KeepUnlanded records a keep decision for a listed branch.
func (a *API) KeepUnlanded(ctx context.Context, branch, note string) error {
	if a == nil || a.M == nil {
		return errHousekeepingNotConfigured
	}
	return a.M.KeepUnlanded(ctx, branch, note)
}

// ArchiveUnlanded moves a listed branch under the archive ref namespace.
func (a *API) ArchiveUnlanded(ctx context.Context, branch string) (string, error) {
	if a == nil || a.M == nil {
		return "", errHousekeepingNotConfigured
	}
	return a.M.ArchiveUnlanded(ctx, branch)
}

// DeleteUnlandedWorktree backs up and deletes a listed branch and worktree.
func (a *API) DeleteUnlandedWorktree(ctx context.Context, branch string, confirm bool) (DeleteWorktreeResult, error) {
	if a == nil || a.M == nil {
		return DeleteWorktreeResult{Branch: branch}, errHousekeepingNotConfigured
	}
	return a.M.DeleteUnlandedWorktree(ctx, branch, confirm)
}

// UncommittedWorkDiff returns the diff of one worktree under the worktree
// base.
func (a *API) UncommittedWorkDiff(ctx context.Context, worktree string) (string, error) {
	if a == nil || a.M == nil {
		return "", errHousekeepingNotConfigured
	}
	return a.M.UncommittedWorkDiff(ctx, worktree)
}

// CommitUncommittedWork commits the uncommitted files of one worktree.
func (a *API) CommitUncommittedWork(ctx context.Context, worktree, message string) (string, error) {
	if a == nil || a.M == nil {
		return "", errHousekeepingNotConfigured
	}
	return a.M.CommitUncommittedWork(ctx, worktree, message)
}

// FixPermissions restores owner write permission on the given paths.
func (a *API) FixPermissions(paths []string) (int, error) {
	if a == nil || a.M == nil {
		return 0, errHousekeepingNotConfigured
	}
	return a.M.FixPermissions(paths)
}

// KeepLanding forwards to the manager's landing keep decision, so the REST
// layer (which holds this wrapper, not the manager) can serve it.
func (a *API) KeepLanding(id, actor, reason string) (*PRD, error) {
	return a.M.KeepLanding(id, actor, reason)
}

// DropLanding forwards to the manager's landing drop decision.
func (a *API) DropLanding(id, actor, reason string) (*PRD, error) {
	return a.M.DropLanding(id, actor, reason)
}
