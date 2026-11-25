package autonomous

// The daemon wires *API (not *Manager) into the server, so the server's
// optional branch refresh interfaces are satisfied only through these
// delegating methods; without them the routes answer 501.

// BranchRefreshStats reports the branch refresh numbers over every Automaton.
func (a *API) BranchRefreshStats() BranchRefreshStats {
	return a.M.BranchRefreshStats()
}

// SetBranchRefreshOverride sets an Automaton's branch refresh override; nil
// clears it.
func (a *API) SetBranchRefreshOverride(id string, v *bool) error {
	return a.M.SetBranchRefreshOverride(id, v)
}
