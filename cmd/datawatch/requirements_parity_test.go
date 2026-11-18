package main

import (
	"net/http"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/mcp"
	"github.com/dmz006/datawatch/internal/router"
	"github.com/dmz006/datawatch/internal/server"
)

// requirementsParity maps each requirements surface REST operation to its
// route and its counterpart on every verb surface. There are no exemptions:
// all four operations ship on REST, MCP, CLI and comm.
var requirementsParity = map[string]struct {
	method, path, mcp, cli, cliGroup, comm string
}{
	"requirements_report": {
		http.MethodGet, "/api/automation/requirements-report",
		"automation_requirements_report", "requirements-report", "automation", "requirements-report",
	},
	"model_consent_get": {
		http.MethodGet, "/api/automation/model-consent",
		"automation_model_pull_consent_get", "model-consent", "automation", "model-pull-consent-get",
	},
	"model_consent_set": {
		http.MethodPut, "/api/automation/model-consent",
		"automation_model_pull_consent_set", "model-consent", "automation", "model-pull-consent-set",
	},
	"node_backend_versions": {
		http.MethodGet, "/api/compute/nodes/{name}/versions",
		"compute_node_backend_versions", "versions", "compute-node", "node-versions",
	},
}

func TestRequirementsSurfacesParity(t *testing.T) {
	mcpTools := toSet(mcp.RequirementsSurfacesToolNames())
	commVerbs := toSet(router.RequirementsSurfacesCommVerbs())

	// CLI: the automation command carries requirements-report and
	// model-consent (get|set); the compute node command carries versions.
	automationCLI := map[string]bool{}
	for _, c := range newAutomationCmd().Commands() {
		automationCLI[c.Name()] = true
	}
	nodeCLI := map[string]bool{}
	for _, c := range newComputeCmd().Commands() {
		if c.Name() != "node" {
			continue
		}
		for _, sub := range c.Commands() {
			nodeCLI[sub.Name()] = true
		}
	}

	ops := server.RequirementsSurfacesRESTOperations()
	if len(ops) != len(requirementsParity) {
		t.Fatalf("the server lists %d requirements REST operations; want %d (one per surface operation)", len(ops), len(requirementsParity))
	}
	for _, op := range ops {
		want, ok := requirementsParity[op.Name]
		if !ok {
			t.Errorf("REST operation %q has no entry in requirementsParity; map it to its route, MCP tool, CLI subcommand and comm verb", op.Name)
			continue
		}
		if op.Method != want.method || op.Path != want.path {
			t.Errorf("REST operation %q is %s %s; want %s %s", op.Name, op.Method, op.Path, want.method, want.path)
		}
		if !mcpTools[want.mcp] {
			t.Errorf("REST operation %q has no MCP tool %q", op.Name, want.mcp)
		}
		if want.cliGroup == "automation" {
			if !automationCLI[want.cli] {
				t.Errorf("REST operation %q has no CLI subcommand %q under datawatch automation", op.Name, want.cli)
			}
		} else {
			if !nodeCLI[want.cli] {
				t.Errorf("REST operation %q has no CLI subcommand %q under datawatch compute node", op.Name, want.cli)
			}
		}
		if !commVerbs[want.comm] {
			t.Errorf("REST operation %q has no comm verb %q", op.Name, want.comm)
		}
		if got := router.Parse("automation " + want.comm).Type; got != router.CmdAutomation {
			t.Errorf("comm verb %q does not parse under the automation command (got %q)", want.comm, got)
		}
	}
	for name := range requirementsParity {
		found := false
		for _, op := range ops {
			if op.Name == name {
				found = true
			}
		}
		if !found {
			t.Errorf("requirementsParity entry %q matches no REST operation", name)
		}
	}
}

// TestRequirementsSurfacesCaps pins the MCP capability of every tool: the
// three automation reads on the autonomous read cap, the consent set on the
// config-write cap, and the node versions read on the compute read cap.
func TestRequirementsSurfacesCaps(t *testing.T) {
	caps := map[string]string{
		"automation_requirements_report":    federation.CapAutonomousRead,
		"automation_model_pull_consent_get": federation.CapAutonomousRead,
		"automation_model_pull_consent_set": federation.CapConfigWrite,
		"compute_node_backend_versions":     federation.CapComputeRead,
	}
	for tool, want := range caps {
		got, ok := federation.RequiredCapForMCPTool(tool)
		if !ok {
			t.Errorf("MCP tool %q has no capability entry; want %q", tool, want)
			continue
		}
		if got != want {
			t.Errorf("MCP tool %q capability = %q; want %q", tool, got, want)
		}
	}
}
