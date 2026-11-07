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
		mcpsdk.WithDescription("Read the Automata progress metrics: counts by kind (feature, bug, other) and in total, throughput per active day, worker utilization, lead time per stage, inactive windows left out of every rate, the remaining work split into known records and estimated work, and the completion range in days with the method behind each figure, for the scope filtered. Read-only; same object as GET /api/autonomous/progress. since and until are dates (YYYY-MM-DD); until includes the whole day. automation_id narrows the count to one Automation; tags narrows it to the Automata carrying that tag, several at once. group (project, automation or automaton) adds group_totals: every total split by that axis, with records that have no project shown as (none)."),
		mcpsdk.WithString("automation_id", mcpsdk.Description("Optional — count only the Automata of this Automation")),
		mcpsdk.WithArray("tags", mcpsdk.WithStringItems(), mcpsdk.Description("Optional — count only the Automata carrying these tags")),
		mcpsdk.WithString("tag_match", mcpsdk.Description("Optional — any (default): the Automaton needs at least one of tags; all: every tag in tags")),
		mcpsdk.WithString("group", mcpsdk.Description("Optional — project, automation or automaton: return the totals split by that axis as group_totals")),
		mcpsdk.WithString("project", mcpsdk.Description("Optional — ignored by the progress read; the trend tool reads one project (use group=project here for per-project totals)")),
		mcpsdk.WithString("since", mcpsdk.Description("Optional — first day counted, YYYY-MM-DD (default: the configured date, else the first record's creation date)")),
		mcpsdk.WithString("until", mcpsdk.Description("Optional — last day counted, YYYY-MM-DD, inclusive (default: now)")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleAutonomousProgress(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	for _, k := range []string{"automation_id", "group", "project", "since", "until"} {
		if v := req.GetString(k, ""); v != "" {
			q.Set(k, v)
		}
	}
	for _, v := range req.GetStringSlice("tags", nil) {
		if v != "" {
			q.Add("tag", v)
		}
	}
	if v := req.GetString("tag_match", ""); v != "" {
		q.Set("tag_match", v)
	}
	out, err := s.proxyGet("/api/autonomous/progress", q)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousProgressTrend() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_progress_trend",
		mcpsdk.WithDescription("Read the progress trend from the saved daily snapshots: days, scope (what the snapshots were counting), points (one per snapshot, oldest first: date, reason, scope, completion_low_days, completion_high_days, known_tasks, estimated_tasks, throughput_3d, utilization, completed_prds) and direction (converging, widening, flat or unknown) of the completion range. Read-only; same object as GET /api/autonomous/progress/trend. tags and tag_match name the scope the trend reply reports. project returns that project's own points as project_points, and group (project, automation or automaton) names the axis the grouped totals are read on."),
		mcpsdk.WithNumber("days", mcpsdk.Description("Optional — how many days back to read, a whole number from 1 to 365 (default 30)")),
		mcpsdk.WithString("group", mcpsdk.Description("Optional — project, automation or automaton: the axis the grouped totals are read on")),
		mcpsdk.WithString("project", mcpsdk.Description("Optional — return this project's own points (project_points); records with no project are under (none)")),
		mcpsdk.WithArray("tags", mcpsdk.WithStringItems(), mcpsdk.Description("Optional — the tag scope the trend reply reports")),
		mcpsdk.WithString("tag_match", mcpsdk.Description("Optional — any (default): the Automata carried at least one of tags; all: every tag in tags")),
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
	for _, k := range []string{"group", "project"} {
		if v := req.GetString(k, ""); v != "" {
			q.Set(k, v)
		}
	}
	for _, v := range req.GetStringSlice("tags", nil) {
		if v != "" {
			q.Add("tag", v)
		}
	}
	if v := req.GetString("tag_match", ""); v != "" {
		q.Set("tag_match", v)
	}
	out, err := s.proxyGet("/api/autonomous/progress/trend", q)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ProgressToolNames lists the progress tools, for the parity test.
func ProgressToolNames() []string {
	s := &Server{}
	return []string{s.toolAutonomousProgress().Name, s.toolAutonomousProgressTrend().Name}
}
