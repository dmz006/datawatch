package mcp

import (
	"context"
	"net/url"
	"strconv"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The lane utilization tool is read-only and proxies
// GET /api/autonomous/lane-utilization. It returns the route's JSON unchanged.

func (s *Server) toolAutonomousLaneUtilization() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_lane_utilization",
		mcpsdk.WithDescription("Read how the lanes were used over a window ending now: per-lane busy and idle time bucketed over the window, plus the lane fill status with its newest decisions. Read-only; same object as GET /api/autonomous/lane-utilization. window is a duration such as 6h, 24h or 7d (default 24h); bucket is how many time buckets to split the window into; decisions is how many of the newest planner decisions to return. The daemon validates each value and refuses one out of range with its message."),
		mcpsdk.WithString("window", mcpsdk.Description("Optional — window length as a duration such as 6h, 24h or 7d (default 24h)")),
		mcpsdk.WithNumber("bucket", mcpsdk.Description("Optional — number of time buckets, a whole number")),
		mcpsdk.WithNumber("decisions", mcpsdk.Description("Optional — how many newest decisions to return, a whole number")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleAutonomousLaneUtilization(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	args := req.GetArguments()
	// Numbers arrive as numbers, or as text from a client that quotes them; the
	// REST route decides whether a value is valid, so pass it through as given.
	for _, k := range []string{"window", "bucket", "decisions"} {
		switch v := args[k].(type) {
		case string:
			if v != "" {
				q.Set(k, v)
			}
		case float64:
			q.Set(k, strconv.FormatFloat(v, 'f', -1, 64))
		}
	}
	out, err := s.proxyGet("/api/autonomous/lane-utilization", q)
	if err != nil {
		return toolError("autonomous_lane_utilization", err), nil
	}
	return textOK(string(out)), nil
}
