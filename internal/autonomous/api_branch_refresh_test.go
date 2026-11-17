package autonomous

import "testing"

// TestAPI_ExposesBranchRefreshSurfaces pins the delegate methods the server
// relies on. The daemon wires *API (not *Manager) into the server, and the
// server's optional-interface assertions in internal/server/branch_refresh_api.go
// (branchRefreshStatser / branchRefreshOverrideSetter) only succeed when *API
// carries these methods — without them GET
// /api/autonomous/branch-refresh/stats answers 501 "not supported by this
// manager" and the override route 501 as well.
func TestAPI_ExposesBranchRefreshSurfaces(t *testing.T) {
	var a *API

	stater, ok := any(a).(interface {
		BranchRefreshStats() BranchRefreshStats
	})
	if !ok {
		t.Fatalf("*API does not expose BranchRefreshStats()")
	}
	_ = stater

	setter, ok := any(a).(interface {
		SetBranchRefreshOverride(id string, v *bool) error
	})
	if !ok {
		t.Fatalf("*API does not expose SetBranchRefreshOverride(id string, v *bool) error")
	}
	_ = setter
}
