package mcp

import (
	"context"
	"net/url"
	"strconv"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The progress tools are read-only and proxy the REST routes, so every surface
// returns the same metrics object.

func (s *Server) toolAutonomousProgress() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_progress",
		mcpsdk.WithDescription("Read the Automata progress metrics: counts by kind (feature, bug, other) and in total, throughput per active day, worker utilization, lead time per stage, inactive windows left out of every rate, the remaining work split into known records and estimated work, and the completion range in days with the method behind each figure. Read-only; same object as GET /api/autonomous/progress. since and until are dates (YYYY-MM-DD); until includes the whole day. automation_id and tag_id narrow the count to one Automation or to the Automata carrying one tag."),
		mcpsdk.WithString("automation_id", mcpsdk.Description("Optional — count only the Automata of this Automation")),
		mcpsdk.WithString("tag_id", mcpsdk.Description("Optional — count only the Automata carrying this tag")),
		mcpsdk.WithString("since", mcpsdk.Description("Optional — first day counted, YYYY-MM-DD (default: the configured date, else the first record's creation date)")),
		mcpsdk.WithString("until", mcpsdk.Description("Optional — last day counted, YYYY-MM-DD, inclusive (default: now)")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleAutonomousProgress(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	for _, k := range []string{"automation_id", "tag_id", "since", "until"} {
		if v := req.GetString(k, ""); v != "" {
			q.Set(k, v)
		}
	}
	out, err := s.proxyGet("/api/autonomous/progress", q)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousProgressTrend() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_progress_trend",
		mcpsdk.WithDescription("Read the progress trend from the saved daily snapshots: days, points (one per snapshot, oldest first: date, reason, completion_low_days, completion_high_days, known_tasks, estimated_tasks, throughput_3d, utilization, completed_prds) and direction (converging, widening, flat or unknown) of the completion range. Read-only; same object as GET /api/autonomous/progress/trend."),
		mcpsdk.WithNumber("days", mcpsdk.Description("Optional — how many days back to read, a whole number from 1 to 365 (default 30)")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleAutonomousProgressTrend(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	// days arrives as a number, or as text from a client that quotes it; the
	// REST route decides whether it is valid.
	switch v := req.GetArguments()["days"].(type) {
	case string:
		if v != "" {
			q.Set("days", v)
		}
	case float64:
		q.Set("days", strconv.FormatFloat(v, 'f', -1, 64))
	}
	out, err := s.proxyGet("/api/autonomous/progress/trend", q)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
