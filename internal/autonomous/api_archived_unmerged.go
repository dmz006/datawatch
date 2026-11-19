package autonomous

import "context"

// ArchivedUnmerged forwards to the manager's archived-unmerged finder so the
// REST layer can reach it through the API value.
func (a *API) ArchivedUnmerged(ctx context.Context) []ArchivedUnmergedEntry {
	return a.M.ArchivedUnmerged(ctx)
}

// CarryForwardArchivedForced forwards to the manager's carry-forward. The new
// draft is returned; the archived record and its branch are not changed.
func (a *API) CarryForwardArchivedForced(id, actor string, force bool) (*PRD, error) {
	return a.M.CarryForwardArchivedForced(id, actor, force)
}
