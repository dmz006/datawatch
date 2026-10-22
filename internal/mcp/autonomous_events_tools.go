// MCP tool for the read side of the mcp_queue lifecycle subscription
// sink. Like the subscription tools (autonomous_subscription_tools.go) it
// holds no state: it proxies the daemon REST route on the loopback.
//
//	GET /api/autonomous/events?queue=&since=&limit= → autonomous_events_pending
//
// The same surface over CLI is `datawatch autonomous events`
// (cmd/datawatch/cli_autonomous_events.go).
package mcp

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// autonomousEventsPath is the REST route autonomous_events_pending proxies.
const autonomousEventsPath = "/api/autonomous/events"

// toolAutonomousEventsPending polls a named pending-events queue.
func (s *Server) toolAutonomousEventsPending() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_events_pending",
		mcpsdk.WithDescription("Poll the pending lifecycle events of a named queue, for an MCP client that cannot receive pushes. Returns {\"events\":[...],\"next_cursor\":<n>,\"dropped\":<bool>}. Pass the returned next_cursor back as since_cursor on your next call so you only see new events (start with 0). dropped=true means events you had not yet seen were evicted because the bounded queue overflowed. To create a queue, subscribe with autonomous_subscribe using sink_type \"mcp_queue\" and sink_target set to the queue name; events matching the subscription's filter then accumulate there. Proxies GET /api/autonomous/events. The CLI equivalent is `datawatch autonomous events --queue <name>`."),
		mcpsdk.WithString("queue",
			mcpsdk.Required(),
			mcpsdk.Description("Queue name: the sink_target of an mcp_queue subscription."),
		),
		mcpsdk.WithNumber("since_cursor",
			mcpsdk.Description("Return only events after this cursor. Default 0 (everything retained). Pass back the next_cursor from the previous call."),
		),
		mcpsdk.WithNumber("limit",
			mcpsdk.Description("Maximum events to return. Default 50, capped at 200."),
		),
	)
}

// handleAutonomousEventsPending proxies GET /api/autonomous/events.
func (s *Server) handleAutonomousEventsPending(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	queue := strings.TrimSpace(req.GetString("queue", ""))
	if queue == "" {
		return toolErrorf("autonomous_events_pending", "queue is required"), nil
	}
	q := url.Values{"queue": {queue}}
	if since, ok := getNumberArg(req, "since_cursor"); ok {
		if since < 0 || since != float64(uint64(since)) {
			return toolErrorf("autonomous_events_pending", "since_cursor must be a non-negative whole number"), nil
		}
		q.Set("since", strconv.FormatUint(uint64(since), 10))
	}
	if limit, ok := getNumberArg(req, "limit"); ok {
		if limit < 1 || limit != float64(int(limit)) {
			return toolErrorf("autonomous_events_pending", "limit must be a positive whole number"), nil
		}
		q.Set("limit", strconv.Itoa(int(limit)))
	}
	respBody, err := s.proxyJSON(http.MethodGet, autonomousEventsPath+"?"+q.Encode(), nil)
	if err != nil {
		return toolError("autonomous_events_pending", err), nil
	}
	return textOK(string(respBody)), nil
}
