// MCP tools for the burn guard effort ceiling: the per-Automation map of task
// kind to ceiling, and the per-task ceiling. Both proxy to REST.
package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolAutomationSetEffortCeiling() mcpsdk.Tool {
	opts := append(automationControlBase("Set the effort ceiling of an Automation: a map of task kind (docs, parity, live_check, final_gate) to low, medium, high or cheaper_backend. A ceiling only lowers a task's effort or moves it to the cheaper backend; it never raises one. An empty map clears the ceiling."),
		mcpsdk.WithObject("effort_ceiling", mcpsdk.Description("Task kind to ceiling, for example {\"docs\":\"low\",\"final_gate\":\"cheaper_backend\"}. Empty clears it.")),
	)
	return mcpsdk.NewTool("automation_set_effort_ceiling", opts...)
}

func (s *Server) handleAutomationSetEffortCeiling(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	extra := map[string]any{}
	if v, ok := req.GetArguments()["effort_ceiling"]; ok && v != nil {
		extra["effort_ceiling"] = v
	}
	return s.automationControlCall(req, "set_effort_ceiling", extra)
}

func (s *Server) toolAutonomousPRDSetTaskEffortCeiling() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_task_effort_ceiling",
		mcpsdk.WithDescription("Set the effort ceiling of one task: low, medium, high or cheaper_backend. It only lowers effort or moves the task to the cheaper backend. An empty ceiling clears it."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton ID")),
		mcpsdk.WithString("task_id", mcpsdk.Required(), mcpsdk.Description("Task ID")),
		mcpsdk.WithString("ceiling", mcpsdk.Description("low, medium, high, cheaper_backend; empty clears")),
	)
}

func (s *Server) handleAutonomousPRDSetTaskEffortCeiling(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]any{
		"task_id": req.GetString("task_id", ""),
		"ceiling": req.GetString("ceiling", ""),
		"actor":   "operator",
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_task_effort_ceiling", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
