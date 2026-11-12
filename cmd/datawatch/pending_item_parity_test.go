package main

import (
	"testing"

	"github.com/dmz006/datawatch/internal/mcp"
	"github.com/dmz006/datawatch/internal/router"
)

// pendingItemParity maps each pending item operation to its counterpart on the
// MCP, CLI and chat surfaces. The REST routes are under
// /api/autonomous/pending-items; there are no exemptions.
var pendingItemParity = []struct {
	op, mcp, cli, comm string
}{
	{"list", "autonomous_pending_items", "pending-items", "pending-items"},
	{"add", "autonomous_pending_item_add", "pending-item-add", "pending-item-add"},
	{"attach", "autonomous_pending_item_attach", "pending-item-attach", "pending-item-attach"},
	{"refresh", "autonomous_pending_items_refresh", "pending-items-refresh", "pending-items-refresh"},
}

func TestPendingItemParity(t *testing.T) {
	mcpTools := toSet(mcp.PendingItemToolNames())
	commVerbs := toSet(router.PendingItemCommVerbs())
	cliVerbs := map[string]bool{}
	for _, c := range newAutonomousCmd().Commands() {
		cliVerbs[c.Name()] = true
	}
	for _, p := range pendingItemParity {
		if !mcpTools[p.mcp] {
			t.Errorf("%s has no MCP tool %q", p.op, p.mcp)
		}
		if !cliVerbs[p.cli] {
			t.Errorf("%s has no CLI subcommand %q", p.op, p.cli)
		}
		if !commVerbs[p.comm] {
			t.Errorf("%s has no comm verb %q", p.op, p.comm)
		}
		if router.Parse("autonomous "+p.comm).Type != router.CmdAutonomous {
			t.Errorf("comm verb %q does not parse under the autonomous command", p.comm)
		}
	}
	if len(pendingItemParity) != len(mcp.PendingItemToolNames()) || len(pendingItemParity) != len(router.PendingItemCommVerbs()) {
		t.Errorf("parity table has %d rows; MCP lists %d tools and comm lists %d verbs",
			len(pendingItemParity), len(mcp.PendingItemToolNames()), len(router.PendingItemCommVerbs()))
	}
}
