// BL107 — MCP tool exposing the agent audit trail query.

package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/agents"
)

func (s *Server) toolAgentAudit() mcpsdk.Tool {
	return mcpsdk.NewTool("agent_audit",
		mcpsdk.WithDescription("Query the agent audit trail (JSON-lines file). Returns the most-recent N events matching optional event/agent/project filters."),
		mcpsdk.WithString("event",
			mcpsdk.Description("Filter to one event type (spawn, terminate, result, bootstrap, spawn_fail, idle_reap, crash_respawn, …)"),
		),
		mcpsdk.WithString("agent_id",
			mcpsdk.Description("Filter to a single agent ID"),
		),
		mcpsdk.WithString("project",
			mcpsdk.Description("Filter to one Project Profile name"),
		),
		mcpsdk.WithNumber("limit",
			mcpsdk.Description("Maximum events to return (default 100)"),
		),
	)
}

func (s *Server) handleAgentAudit(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.agentAuditPath == "" {
		return mcpsdk.NewToolResultText("agent audit not enabled"), nil
	}
	if s.agentAuditCEF {
		return mcpsdk.NewToolResultText(
			"agent audit file is CEF-formatted; query your SIEM instead"), nil
	}
	limit := int(req.GetFloat("limit", 100))
	if limit <= 0 {
		limit = 100
	}
	filter := agents.ReadEventsFilter{
		Event:   req.GetString("event", ""),
		AgentID: req.GetString("agent_id", ""),
		Project: req.GetString("project", ""),
	}
	events, err := agents.ReadEvents(s.agentAuditPath, filter, limit)
	if err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("error: %v", err)), nil
	}
	body, _ := json.MarshalIndent(events, "", "  ")
	return mcpsdk.NewToolResultText(string(body)), nil
}
