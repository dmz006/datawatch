// The branch reaper behind the API wrapper. The server asserts the wrapper
// against its reapReader / reapRunner interfaces; without these methods the
// branch-reap routes answer 503 in a running daemon. Each method only
// delegates to the Manager's reaper in reap_surface.go, so the reaper's rules
// apply unchanged: a branch whose commits are not all on the Automaton's work
// branch or the base branch is never touched, and a backup ref is written
// before any branch is deleted.
package autonomous

import "context"

// ReapPreview is the dry run: the branches that would be reaped and held plus
// the backup refs that already exist. It writes nothing and deletes nothing.
func (a *API) ReapPreview(ctx context.Context) (ReapReport, []ReapBackup, error) {
	return a.M.ReapPreview(ctx)
}

// ReapRun performs one reap pass (backup first, then delete). A second pass
// while one runs gets ErrReapBusy.
func (a *API) ReapRun(ctx context.Context) (ReapReport, error) {
	return a.M.ReapRun(ctx)
}

// ReapRestore recreates branch from its newest backup ref and returns the
// backup ref used and the commit the branch now points at.
func (a *API) ReapRestore(ctx context.Context, branch string) (string, string, error) {
	return a.M.ReapRestore(ctx, branch)
}
