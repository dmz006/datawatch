// burn_guard_tools.go — the burn guard MCP tools. They proxy the loopback REST surface:
//
//	burn_guard_status      → GET  /api/usage/burn-guard
//	burn_guard_run         → POST /api/usage/burn-guard/run
//	burn_guard_config_get  → GET  /api/usage/burn-guard/config
//	burn_guard_config_set  → PUT  /api/usage/burn-guard/config
//
// The guard changes only admission; it never ends a running session.

package mcp

import (
	"context"
	"net/http"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolBurnGuardStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("burn_guard_status",
		mcpsdk.WithDescription("The burn guard's state: enabled, state (off, idle, guarding or waiting), the rung in force and the rungs applied, the last projection (projected weekly percent, burn per hour, the pace line and the margin), the last decision, the fitted cost coefficients, the last error and the counters. The guard compares the projected weekly burn with the pace line and steps down a ladder of admission changes when the projection runs over; it never ends a running session."),
	)
}

func (s *Server) handleBurnGuardStatus(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/usage/burn-guard", nil)
	if err != nil {
		return toolError("burn_guard_status", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolBurnGuardRun() mcpsdk.Tool {
	return mcpsdk.NewTool("burn_guard_run",
		mcpsdk.WithDescription("Evaluate the burn guard now instead of waiting for its next interval: it projects the weekly burn, compares it with the pace line plus the margin, and may climb or step back one rung. Returns the decision and the refreshed status. Answers an error when the guard is not running on this daemon. The guard never ends a running session."),
	)
}

func (s *Server) handleBurnGuardRun(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodPost, "/api/usage/burn-guard/run", map[string]any{})
	if err != nil {
		return toolError("burn_guard_run", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolBurnGuardConfigGet() mcpsdk.Tool {
	return mcpsdk.NewTool("burn_guard_config_get",
		mcpsdk.WithDescription("The burn guard settings (usage.burn_guard): enabled, interval_minutes, margin_points, ladder, step_back, cache price per million tokens, refit_on_reading, effort_ceiling, retry_limit_claude, ask_on_change and max_projection_gap_points (the disagreement between the guard and the usage advice before the guard takes the lower projection)."),
	)
}

func (s *Server) handleBurnGuardConfigGet(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/usage/burn-guard/config", nil)
	if err != nil {
		return toolError("burn_guard_config_get", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolBurnGuardConfigSet() mcpsdk.Tool {
	return mcpsdk.NewTool("burn_guard_config_set",
		mcpsdk.WithDescription("Change burn guard settings; give at least one. The values go through the same validation and save as a config set, so one invalid value rejects the whole call and nothing is stored. The effort ceiling is set with automation_set_effort_ceiling, not here."),
		mcpsdk.WithBoolean("enabled", mcpsdk.Description("Turn the guard on or off")),
		mcpsdk.WithNumber("interval_minutes", mcpsdk.Description("Minutes between evaluations, a whole number from 1 to 1440")),
		mcpsdk.WithNumber("margin_points", mcpsdk.Description("Points the projection may run over the pace line before the guard acts, 0 to 50")),
		mcpsdk.WithArray("ladder", mcpsdk.Description("Ordered rungs applied while over the line: lower_inflight, route_pi_prompt, hold_claude"), mcpsdk.WithStringItems()),
		mcpsdk.WithBoolean("step_back", mcpsdk.Description("Undo rungs in reverse once the projection is back under the line")),
		mcpsdk.WithNumber("cache_write_points_per_million", mcpsdk.Description("Weekly points per million cache-write tokens, above 0 and at most 1000")),
		mcpsdk.WithNumber("cache_read_points_per_million", mcpsdk.Description("Weekly points per million cache-read tokens, above 0 and at most 1000")),
		mcpsdk.WithBoolean("refit_on_reading", mcpsdk.Description("Refit the cost model whenever a usage reading arrives")),
		mcpsdk.WithNumber("retry_limit_claude", mcpsdk.Description("Most retries of one task on Claude, a whole number from 0 to 20")),
		mcpsdk.WithBoolean("ask_on_change", mcpsdk.Description("When false (default) the guard sends one alert per change and no question; when true it keeps at most one open question")),
		mcpsdk.WithNumber("max_projection_gap_points", mcpsdk.Description("Points of disagreement between the guard and the usage advice before the guard takes the lower projection; 0 never compares; default 25; see the burn guard how-to")),
	)
}

var burnGuardConfigArgs = []string{
	"enabled", "interval_minutes", "margin_points", "ladder", "step_back",
	"cache_write_points_per_million", "cache_read_points_per_million",
	"refit_on_reading", "retry_limit_claude", "ask_on_change",
	"max_projection_gap_points",
}

func (s *Server) handleBurnGuardConfigSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	args := req.GetArguments()
	body := map[string]any{}
	for _, k := range burnGuardConfigArgs {
		if v, ok := args[k]; ok && v != nil {
			body[k] = v
		}
	}
	if len(body) == 0 {
		return toolErrorf("burn_guard_config_set", "give at least one setting"), nil
	}
	out, err := s.proxyJSON(http.MethodPut, "/api/usage/burn-guard/config", body)
	if err != nil {
		return toolError("burn_guard_config_set", err), nil
	}
	return textOK(string(out)), nil
}
