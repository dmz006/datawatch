// MCP tool for the listing of every lifecycle event kind. Like the other
// Automata event tools it holds no state: it proxies the daemon REST route on
// the loopback, so it returns exactly what the REST endpoint returns.
//
//	GET /api/autonomous/event-kinds → autonomous_event_kinds
package mcp

import (
	"context"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// autonomousEventKindsPath is the REST route autonomous_event_kinds proxies.
const autonomousEventKindsPath = "/api/autonomous/event-kinds"

// toolAutonomousEventKinds lists every subscribable event kind.
func (s *Server) toolAutonomousEventKinds() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_event_kinds",
		mcpsdk.WithDescription("List every lifecycle event kind a subscription can match, grouped by group. Read-only. Returns {\"groups\":[{\"group\":<name>,\"kinds\":[{\"kind\":<name>,\"description\":<text>,\"wake_default\":<bool>}]}]}; built-in kinds and kinds registered by extensions both appear. wake_default is true when the kind is in the default set that wakes the supervisor. Use it to pick the kinds to pass to autonomous_subscribe. Takes no arguments. Proxies GET /api/autonomous/event-kinds."),
	)
}

// handleAutonomousEventKinds proxies GET /api/autonomous/event-kinds.
func (s *Server) handleAutonomousEventKinds(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	respBody, err := s.proxyGet(autonomousEventKindsPath, nil)
	if err != nil {
		return toolError("autonomous_event_kinds", err), nil
	}
	return textOK(string(respBody)), nil
}
