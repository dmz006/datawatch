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
		mcpsdk.WithDescription("The burn guard settings (usage.burn_guard): enabled, interval_minutes, margin_points, ladder, step_back, cache price per million tokens, refit_on_reading, effort_ceiling, retry_limit_claude, ask_on_change, max_projection_gap_points (the disagreement between the guard and the usage advice before the guard takes the lower projection), and the pace_line policy: mode (off, shadow or on; unset defaults to shadow), week_tolerance (whole number 0 to 10, default 2), points_to_week_factor (1 to 10, default 1.3), window_percent_per_point (1 to 25, default 4), window_stop_percent (50 to 95, default 85) and reading_freshness_minutes (1 to 1440, default 60)."),
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
		mcpsdk.WithArray("ladder", mcpsdk.Description("Ordered rungs applied while over the pace line: lower_inflight, route_entry, route_cheaper_model, hold_entry (the legacy first-version names route_pi_prompt and hold_claude are accepted and normalize to route_entry and hold_entry). route_cheaper_model keeps the backend and sends new work to a cheaper model on it. Each rung acts on the ladder's entry, not on any fixed backend"), mcpsdk.WithStringItems()),
		mcpsdk.WithBoolean("step_back", mcpsdk.Description("Undo rungs in reverse once the projection is back under the line")),
		mcpsdk.WithNumber("cache_write_points_per_million", mcpsdk.Description("Weekly points per million cache-write tokens, above 0 and at most 1000")),
		mcpsdk.WithNumber("cache_read_points_per_million", mcpsdk.Description("Weekly points per million cache-read tokens, above 0 and at most 1000")),
		mcpsdk.WithBoolean("refit_on_reading", mcpsdk.Description("Refit the cost model whenever a usage reading arrives")),
		mcpsdk.WithNumber("retry_limit_claude", mcpsdk.Description("Most retries of one task on Claude, a whole number from 0 to 20")),
		mcpsdk.WithBoolean("ask_on_change", mcpsdk.Description("When false (default) the guard sends one alert per change and no question; when true it keeps at most one open question")),
		mcpsdk.WithNumber("max_projection_gap_points", mcpsdk.Description("Points of disagreement between the guard and the usage advice before the guard takes the lower projection; 0 never compares; default 25; see the burn guard how-to")),
		mcpsdk.WithString("mode", mcpsdk.Description("The pace_line policy mode: off, shadow or on; an unset mode reads shadow. Shadow first, then on")),
		mcpsdk.WithNumber("week_tolerance", mcpsdk.Description("Whole number from 0 to 10, default 2: how many points the week reading may run over the pace line and still be ok")),
		mcpsdk.WithNumber("points_to_week_factor", mcpsdk.Description("Number from 1 to 10, default 1.3: one point of ledger burn counts as this many points of the weekly allowance")),
		mcpsdk.WithNumber("window_percent_per_point", mcpsdk.Description("Number from 1 to 25, default 4: one point burned is this percent of the current window")),
		mcpsdk.WithNumber("window_stop_percent", mcpsdk.Description("Number from 50 to 95, default 85: a window is over at this percent used")),
		mcpsdk.WithNumber("reading_freshness_minutes", mcpsdk.Description("Whole number from 1 to 1440, default 60: a reading older than this is stale and the guard asks the operator for a fresh one")),
		mcpsdk.WithString("entries", mcpsdk.Description("The paid LLM entries the guard evaluates, as a JSON list: each {llm, usage_source: readings|none, plan: flat|metered, period_target (percent for flat, dollars for metered), model, ladder, held_models, route_to, route_cheaper_model, points_per_usd}. route_cheaper_model names the cheaper model the route_cheaper_model rung sends that entry's new work to (empty = resolved per model rates, Haiku for Claude). An entry with no usage source is never held. An empty list returns to the single default entry")),
	)
}

var burnGuardConfigArgs = []string{
	"enabled", "interval_minutes", "margin_points", "ladder", "step_back",
	"cache_write_points_per_million", "cache_read_points_per_million",
	"refit_on_reading", "retry_limit_claude", "ask_on_change",
	"max_projection_gap_points", "entries",
	"mode", "week_tolerance", "points_to_week_factor",
	"window_percent_per_point", "window_stop_percent", "reading_freshness_minutes",
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
