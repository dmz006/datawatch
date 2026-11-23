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

// AutomatonMergeState forwards to the manager's derived merge_state reader so
// the server, which holds this wrapper, can show finished_not_merged on views.
func (a *API) AutomatonMergeState(p *PRD) string {
	return a.M.AutomatonMergeState(p)
}

// AutomatonMergeStateOf forwards the field-based reader used by index-backed
// views.
func (a *API) AutomatonMergeStateOf(id string, status PRDStatus, branch, base string) string {
	return a.M.AutomatonMergeStateOf(id, status, branch, base)
}
