package main

import (
	"net/http"
	"testing"

	"github.com/dmz006/datawatch/internal/mcp"
	"github.com/dmz006/datawatch/internal/router"
	"github.com/dmz006/datawatch/internal/server"
)

// laneFillParity maps a lane fill REST operation to its route and to its
// counterpart on each verb surface. There are no exemptions: both operations
// are on every surface.
var laneFillParity = map[string]struct {
	method, path, mcp, cli, comm string
}{
	"status": {
		http.MethodGet, "/api/autonomous/lane-fill",
		"autonomous_lane_fill_status", "lane-fill", "lane-fill",
	},
	"set_lane_preference": {
		http.MethodPost, "/api/autonomous/prds/{id}/set_lane_preference",
		"autonomous_prd_set_lane_preference", "prd-set-lane-preference", "set-lane-preference",
	},
}

func TestLaneFillParity(t *testing.T) {
	mcpTools := toSet(mcp.LaneFillToolNames())
	commVerbs := toSet(router.LaneFillCommVerbs())
	cliVerbs := map[string]bool{}
	for _, c := range newAutonomousCmd().Commands() {
		cliVerbs[c.Name()] = true
	}

	ops := server.LaneFillRESTOperations()
	if len(ops) == 0 {
		t.Fatal("the server lists no lane fill REST operations")
	}
	seen := map[string]bool{}
	for _, op := range ops {
		seen[op.Name] = true
		want, ok := laneFillParity[op.Name]
		if !ok {
			t.Errorf("REST operation %q has no entry in laneFillParity; map it to its route, MCP tool, CLI subcommand and comm verb", op.Name)
			continue
		}
		if op.Method != want.method || op.Path != want.path {
			t.Errorf("REST operation %q is %s %s; want %s %s", op.Name, op.Method, op.Path, want.method, want.path)
		}
		if !mcpTools[want.mcp] {
			t.Errorf("REST operation %q has no MCP tool %q", op.Name, want.mcp)
		}
		if !cliVerbs[want.cli] {
			t.Errorf("REST operation %q has no CLI subcommand %q", op.Name, want.cli)
		}
		if !commVerbs[want.comm] {
			t.Errorf("REST operation %q has no comm verb %q", op.Name, want.comm)
		}
		if router.Parse("autonomous "+want.comm).Type != router.CmdAutonomous {
			t.Errorf("comm verb %q does not parse under the autonomous command", want.comm)
		}
	}
	for name := range laneFillParity {
		if !seen[name] {
			t.Errorf("laneFillParity entry %q matches no REST operation", name)
		}
	}
}
