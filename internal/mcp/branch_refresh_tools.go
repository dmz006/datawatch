// MCP tool for the per-Automaton branch refresh override. It proxies to
// POST /api/autonomous/prds/{id}/branch-refresh. The two daemon-wide keys
// (autonomous.branch_refresh_enabled, autonomous.branch_refresh_behind_limit)
// are set with the existing config_set tool.
package mcp

import (
	"context"
	"net/http"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolAutonomousPRDSetBranchRefresh() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_branch_refresh",
		mcpsdk.WithDescription("Set the branch refresh override of one Automaton: enabled=true or false forces the between-task refresh of its branch from the base branch on or off; clear=true removes the override so the daemon setting autonomous.branch_refresh_enabled applies. The daemon-wide settings autonomous.branch_refresh_enabled and autonomous.branch_refresh_behind_limit are set with config_set."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton ID")),
		mcpsdk.WithBoolean("enabled", mcpsdk.Description("true or false to force the refresh on or off for this Automaton; omit when clear=true")),
		mcpsdk.WithBoolean("clear", mcpsdk.Description("true removes the override so the daemon setting applies")),
	)
}

func (s *Server) handleAutonomousPRDSetBranchRefresh(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	if id == "" {
		return mcpsdk.NewToolResultText("error: id is required"), nil
	}
	args := req.GetArguments()
	var enabled any
	if req.GetBool("clear", false) {
		enabled = nil
	} else if v, ok := args["enabled"].(bool); ok {
		enabled = v
	} else {
		return mcpsdk.NewToolResultText("error: enabled must be true or false, or pass clear=true to remove the override"), nil
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/branch-refresh", map[string]any{"enabled": enabled})
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(out)), nil
}
