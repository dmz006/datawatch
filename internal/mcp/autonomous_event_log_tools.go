// MCP tool for the Automata event log. It is a read-only, stateless proxy of
// the daemon REST route on the loopback, like the other autonomous read tools
// in this package (autonomous_events_tools.go is the pattern followed here):
//
//	GET /api/autonomous/event-log?kind=&project=&q=&limit=&before= → autonomous_event_log
//
// The tool never writes: every filter is optional and is passed through only
// when non-empty, so the daemon applies its own defaults.
package mcp

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// autonomousEventLogPath is the REST route autonomous_event_log proxies.
const autonomousEventLogPath = "/api/autonomous/event-log"

// toolAutonomousEventLog reads the Automata event log with optional filters.
func (s *Server) toolAutonomousEventLog() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_event_log",
		mcpsdk.WithDescription("Read the Automata event log: the recorded lifecycle events of Automata work, newest first, with optional filters. Read-only. Filter by kind (the event type), project (the project the event belongs to), q (a free-text search over the event text) and page back with before (the opaque cursor from a previous page's pagination field). Pass limit to cap the page size. Proxies GET /api/autonomous/event-log."),
		mcpsdk.WithString("kind",
			mcpsdk.Description("Keep only events of this kind (the event type name). Omit to keep every kind."),
		),
		mcpsdk.WithString("project",
			mcpsdk.Description("Keep only events that belong to this project. Omit to keep every project."),
		),
		mcpsdk.WithString("q",
			mcpsdk.Description("Free-text search over the event text. Omit for no text filter."),
		),
		mcpsdk.WithNumber("limit",
			mcpsdk.Description("Maximum events to return. A positive whole number; the daemon applies its default when omitted."),
		),
		mcpsdk.WithString("before",
			mcpsdk.Description("Return only events older than this opaque cursor, to page back through the log. Pass the cursor from the previous page's pagination field. Omit for the newest page."),
		),
	)
}

// handleAutonomousEventLog proxies GET /api/autonomous/event-log.
func (s *Server) handleAutonomousEventLog(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const tool = "autonomous_event_log"
	q := url.Values{}
	for _, key := range []string{"kind", "project", "q"} {
		if v := strings.TrimSpace(req.GetString(key, "")); v != "" {
			q.Set(key, v)
		}
	}
	if limit, ok := getNumberArg(req, "limit"); ok {
		if limit < 1 || limit != float64(int(limit)) {
			return toolErrorf(tool, "limit must be a positive whole number"), nil
		}
		q.Set("limit", strconv.Itoa(int(limit)))
	}
	if before := strings.TrimSpace(req.GetString("before", "")); before != "" {
		q.Set("before", before)
	}
	respBody, err := s.proxyGet(autonomousEventLogPath, q)
	if err != nil {
		return toolError(tool, err), nil
	}
	return textOK(string(respBody)), nil
}
