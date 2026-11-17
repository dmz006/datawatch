package autonomous

// Server surfaces for branch refresh. The daemon wires *API into the server
// (not *Manager), so the server's optional-interface assertions in
// internal/server/branch_refresh_api.go (branchRefreshStatser /
// branchRefreshOverrideSetter) need these delegating methods or the routes
// answer 501 "not supported by this manager".

// BranchRefreshStats reports the branch refresh numbers over every
// Automaton (GET /api/autonomous/branch-refresh/stats).
func (a *API) BranchRefreshStats() BranchRefreshStats {
	return a.M.BranchRefreshStats()
}

// SetBranchRefreshOverride sets an Automaton's branch refresh override
// (POST /api/autonomous/prds/{id}/branch-refresh); nil clears it.
func (a *API) SetBranchRefreshOverride(id string, v *bool) error {
	return a.M.SetBranchRefreshOverride(id, v)
}
