// internal/mcp/dashboard_data.go — read-only MCP status tools for the
// dashboard's data endpoints. Each returns the REST body verbatim.
//
//	dashboard_work_map_get    — GET /api/dashboard/work-map
//	dashboard_lane_pulse_get  — GET /api/dashboard/lane-pulse
//	dashboard_series_get      — GET /api/dashboard/series/<series>

package mcp

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// dashboardSeriesNames are the six series endpoints, in card order.
var dashboardSeriesNames = []string{
	"lane-utilization", "throughput-forecast", "verifier-health",
	"release-train", "review-backlog", "defects-per-day",
}

func (s *Server) toolDashboardWorkMapGet() mcpsdk.Tool {
	return mcpsdk.NewTool("dashboard_work_map_get",
		mcpsdk.WithDescription("Work map — the same data as GET /api/dashboard/work-map: one node per project, Automation, Automaton, task and live session, an edge for every parent-to-child link, and the running sessions list."),
	)
}

func (s *Server) toolDashboardLanePulseGet() mcpsdk.Tool {
	return mcpsdk.NewTool("dashboard_lane_pulse_get",
		mcpsdk.WithDescription("Lane pulse — the same data as GET /api/dashboard/lane-pulse: busy, waiting and idle traces per capacity lane and node, plus the burn panel."),
		mcpsdk.WithString("window", mcpsdk.Description("time window as a duration, e.g. 6h (default) up to 7 days")),
	)
}

func (s *Server) toolDashboardSeriesGet() mcpsdk.Tool {
	return mcpsdk.NewTool("dashboard_series_get",
		mcpsdk.WithDescription("One dashboard data series — the same data as GET /api/dashboard/series/<series>. Series: "+strings.Join(dashboardSeriesNames, ", ")+". lane-utilization and verifier-health take window; throughput-forecast, release-train and defects-per-day take days; review-backlog takes limit."),
		mcpsdk.WithString("series", mcpsdk.Required(), mcpsdk.Description("series name: "+strings.Join(dashboardSeriesNames, " | "))),
		mcpsdk.WithString("window", mcpsdk.Description("duration for lane-utilization and verifier-health, e.g. 24h")),
		mcpsdk.WithNumber("days", mcpsdk.Description("whole days for throughput-forecast, release-train and defects-per-day")),
		mcpsdk.WithNumber("limit", mcpsdk.Description("row limit for review-backlog")),
	)
}

func (s *Server) handleDashboardWorkMapGetMCP(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body, err := s.proxyGet("/api/dashboard/work-map", nil)
	if err != nil {
		return nil, err
	}
	return mcpsdk.NewToolResultText(string(body)), nil
}

func (s *Server) handleDashboardLanePulseGetMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if w := strings.TrimSpace(req.GetString("window", "")); w != "" {
		q.Set("window", w)
	}
	body, err := s.proxyGet("/api/dashboard/lane-pulse", q)
	if err != nil {
		return nil, err
	}
	return mcpsdk.NewToolResultText(string(body)), nil
}

func (s *Server) handleDashboardSeriesGetMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := strings.TrimSpace(req.GetString("series", ""))
	known := false
	for _, n := range dashboardSeriesNames {
		if n == name {
			known = true
		}
	}
	if !known {
		return nil, fmt.Errorf("unknown series %q: use one of %s", name, strings.Join(dashboardSeriesNames, ", "))
	}
	q := url.Values{}
	if w := strings.TrimSpace(req.GetString("window", "")); w != "" {
		q.Set("window", w)
	}
	if d := req.GetInt("days", 0); d > 0 {
		q.Set("days", fmt.Sprint(d))
	}
	if l := req.GetInt("limit", 0); l > 0 {
		q.Set("limit", fmt.Sprint(l))
	}
	body, err := s.proxyGet("/api/dashboard/series/"+name, q)
	if err != nil {
		return nil, err
	}
	return mcpsdk.NewToolResultText(string(body)), nil
}
