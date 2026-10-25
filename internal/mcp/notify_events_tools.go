// MCP tool for the per-Automaton notify_events lifecycle event set
// (proxies POST /api/autonomous/prds/{id}/set_notify_events).

package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ----- autonomous_prd_set_notify_events -------------------------------------

func (s *Server) toolAutonomousPRDSetNotifyEvents() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_notify_events",
		mcpsdk.WithDescription("Set the lifecycle event kinds that produce notifications for this Automaton, independent of who is subscribed. Default set (when unset at this tier): prd.needs_review, prd.blocked, prd.failed, prd.completed, prd.cancelled. The opt-in kinds task.session_started, task.session_cleared (the session-end kind), scope_warning and guardrail_warn are off by default — add them to the list to be notified. Warnings never block or change any work. Precedence: Automaton > guardrail profile > global default; an empty or omitted list here clears the Automaton-level set and inherits from the next tier. A subscription's own event filter can only narrow this resolved set, never widen it."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton ID")),
		mcpsdk.WithArray("notify_events",
			mcpsdk.WithStringItems(),
			mcpsdk.Description("Event kinds to notify on (e.g. [\"prd.completed\",\"task.session_started\"]); empty or omitted clears the Automaton-level set (inherit from guardrail profile / global default)")),
	)
}

func (s *Server) handleAutonomousPRDSetNotifyEvents(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	events := req.GetStringSlice("notify_events", nil)
	if events == nil {
		events = []string{}
	}
	body, _ := json.Marshal(map[string]any{"notify_events": events})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_notify_events", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
