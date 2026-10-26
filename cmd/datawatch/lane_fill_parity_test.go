package main

import (
	"net/http"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
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

// triageParity maps the triage REST read to its route and its MCP
// counterpart. The cli and comm entries are deliberately empty: unlike the
// lane fill read, the triage read ships on REST and MCP only. The record
// already shows on the Automaton itself in the reads an operator already
// has, and the read exists to serve operator-facing tools; the CLI,
// comm and client surfaces (PWA, Android, iPhone/iOS) ship together with
// the autonomous how-to pass, as one client slice rather than surface by
// surface. The plan's "Parity surface" section
// (docs/plans/2026-10-26-autonomous-triage-read-route.md) states each
// exclusion with its reason, and the checks below pin them so the
// exclusion cannot drift silently.
var triageParity = struct {
	method, path, mcp string
	cli, comm         string // empty: intentionally excluded for this read slice
}{
	http.MethodGet, "/api/autonomous/triage", "autonomous_triage", "", "",
}

// TestTriageParity is the triage half of the lane fill parity test: it
// pins the triage read's parity decisions. Shipped: the REST route and
// method, the MCP tool, and its capability mapping. Excluded by design:
// the CLI — no "triage" subcommand under `datawatch autonomous` — and the
// comm channel, pinned in internal/router by
// TestTriageCommVerbIsExcluded. When a later slice ships any of those
// surfaces it updates triageParity and the plan's "Parity surface"
// section together, and these pins become inclusion checks.
func TestTriageParity(t *testing.T) {
	ops := server.TriageRESTOperations()
	if len(ops) != 1 {
		t.Fatalf("the server lists %d triage REST operations; want exactly 1 (the read)", len(ops))
	}
	op := ops[0]
	if op.Method != triageParity.method || op.Path != triageParity.path {
		t.Errorf("REST operation %q is %s %s; want %s %s", op.Name, op.Method, op.Path, triageParity.method, triageParity.path)
	}

	mcpTools := toSet(mcp.TriageToolNames())
	if !mcpTools[triageParity.mcp] {
		t.Error("the triage MCP tool is not listed in mcp.TriageToolNames()")
	}
	if got := federation.MCPToolCap[triageParity.mcp]; got != federation.CapAutonomousRead {
		t.Errorf("autonomous_triage capability = %q; want %q (the same read capability as the sibling reads)", got, federation.CapAutonomousRead)
	}

	if triageParity.cli != "" {
		t.Fatalf("triageParity has a CLI entry but TestTriageParity still asserts the exclusion; update the test with the plan's Parity surface section")
	}
	for _, c := range newAutonomousCmd().Commands() {
		if c.Name() == "triage" {
			t.Errorf(`the CLI subcommand %q is part of the triage slice now; it was excluded for this read by design — remove this pin and update triageParity and the plan's "Parity surface" section together`, c.Name())
		}
	}
}
