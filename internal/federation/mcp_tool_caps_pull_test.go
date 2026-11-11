// Observer pull protocol (pull mode) — MCP tool → capability mapping for
// the observer_pull_status tool.

package federation_test

import (
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

// TestMCPToolCaps_ObserverPullStatus pins the mapping for the pull-mode
// read tool: it reads the local pull state of peers opted in to pull mode,
// which is observer data, so it rides CapObserversRead (the same capability
// observer_peers_list / observer_peer_stats use). Read-only-style built-in
// groups that can call the other observer read tools must therefore be able
// to call observer_pull_status with no new grant.
func TestMCPToolCaps_ObserverPullStatus(t *testing.T) {
	cap, ok := federation.RequiredCapForMCPTool("observer_pull_status")
	if !ok {
		t.Fatal(`MCPToolCap["observer_pull_status"] missing — the tool is admin-only until classified`)
	}
	if cap != federation.CapObserversRead {
		t.Errorf("observer_pull_status mapped to %q, want %q", cap, federation.CapObserversRead)
	}
	// Consistency with the rest of the observer read surface.
	if got, ok := federation.RequiredCapForMCPTool("observer_peers_list"); ok && got != cap {
		// observer_peers_list may legitimately map to a listing capability;
		// only fail if it is a *write* capability.
		if got == federation.CapObserversWrite {
			t.Errorf("observer_peers_list maps to %q (a write cap) while observer_pull_status maps to %q", got, cap)
		}
	}

	for gname := range federation.BuiltinGroups {
		g, ok := federation.BuiltinGroups[gname]
		if !ok {
			continue
		}
		resolved := federation.Resolve(g.Caps, nil)
		if !federation.Check(resolved, federation.CapObserversRead) {
			continue // group can't read observer data at all — fine
		}
		// A group that can read observer data must not be blocked from the
		// pull-status tool by a missing (or different) cap.
		if !federation.Check(resolved, cap) {
			t.Errorf("built-in group %q has CapObserversRead but not the cap observer_pull_status requires (%q)", gname, cap)
		}
	}
}
