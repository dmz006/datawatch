// autonomous_standard_tasks — MCP read of the built-in standard tasks the
// decomposer adds to each story. The reply is the same object the REST read
// (GET /api/autonomous/standard-tasks) returns: {enabled, tasks[]}. The rows
// come from autonomous.ListStandardTasks inside the daemon, which holds the
// live config; this tool proxies that read so the MCP server never builds a
// second, default-config copy of the list.

package mcp

import (
	"context"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolAutonomousStandardTasks() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_standard_tasks",
		mcpsdk.WithDescription("Lists the built-in standard tasks the decomposer adds to each story. "+
			"Each row shows whether the task is on for the project and its mode. "+
			"Returns {enabled, tasks[]}; enabled is the global switch."),
		mcpsdk.WithString("project", mcpsdk.Description("Optional project directory or alias. Selects the per-project override; empty reports the global enabled value.")),
	)
}

func (s *Server) handleAutonomousStandardTasks(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if p := strings.TrimSpace(req.GetString("project", "")); p != "" {
		q.Set("project", p)
	}
	out, err := s.proxyGet("/api/autonomous/standard-tasks", q)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
