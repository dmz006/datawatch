// cost_report — MCP tool exposing the cost report engine
// (internal/costreport), the same engine call GET /api/cost/report makes
// (costreport.Build over the daemon's recorded sessions), with the
// daemon's cost_report config block as the source of every default.
// group_by / since / until are all optional: a zero-valued field in the
// request falls back to the cost_report config (EffectiveGroupBy,
// EffectiveWindowDays) inside costreport.Build — no defaults are
// hardcoded in this file.

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/costreport"
	"github.com/dmz006/datawatch/internal/session"
)

// errCostReportUnavailable — the daemon has no session manager, mirroring
// the REST handler's 503 "manager not available" for this tool.
var errCostReportUnavailable = errors.New("cost_report: the daemon has no session manager")

// managerCostReportSource feeds costreport.Build with the daemon's
// recorded sessions: one Record per session that has usage, stamped with
// the session's last update time. Same adapter shape the REST handler
// uses for GET /api/cost/report.
type managerCostReportSource struct {
	m *session.Manager
}

func (src managerCostReportSource) Records(_ costreport.SourceOptions) (*costreport.SourceResult, error) {
	res := &costreport.SourceResult{}
	if src.m == nil {
		return res, nil
	}
	for _, sess := range src.m.ListSessions() {
		if sess == nil || (sess.TokensIn == 0 && sess.TokensOut == 0) {
			continue
		}
		res.Records = append(res.Records, costreport.Record{
			SessionID: sess.ID,
			Backend:   sess.BackendFamily,
			At:        sess.UpdatedAt,
			TokensIn:  sess.TokensIn,
			TokensOut: sess.TokensOut,
		})
	}
	return res, nil
}

// costReportRate — the daemon's effective rate for a backend (exact
// backend, family fallback, defaults), so USD estimates match what
// AddUsage prices with.
func (s *Server) costReportRate(backend string) (session.CostRate, bool) {
	if s.manager == nil {
		return session.CostRate{}, false
	}
	return s.manager.CostRateFor(backend)
}

// parseCostReportBound accepts RFC3339 or YYYY-MM-DD, the same bound
// formats the REST handler's since/until query parameters accept.
func parseCostReportBound(v string) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, true
	}
	if t, err := time.Parse("2006-01-02", v); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// buildCostReport runs the engine (costreport.Build) with the
// cost_report config block — the same engine call the REST route makes.
// group_by=release is rejected with a clear message: the MCP tool's
// manager view exposes no shipped units (no PRD ownership linkage), so
// release grouping is a REST/CLI-only grouping; everything else (the
// billing fields included) is the same shape as GET /api/cost/report.
func (s *Server) buildCostReport(req costreport.Request) (*costreport.Report, error) {
	if s.manager == nil {
		return nil, errCostReportUnavailable
	}
	if req.GroupBy == costreport.GroupByRelease {
		return nil, errors.New("group_by=release requires a shipped units source; the cost_report tool exposes none — use group_by=automaton, automation, backend or week (or GET /api/cost/report)")
	}
	billing := func(backend string) config.EffectiveBillingProfile {
		return s.usageCfg.EffectiveBillingProfile(backend)
	}
	return costreport.Build(s.costReportCfg, req, managerCostReportSource{m: s.manager}, nil, s.costReportRate, billing, time.Now())
}

// toolCostReport declares the cost_report tool: cost report over the
// daemon's recorded session spend, all three arguments optional.
func (s *Server) toolCostReport() mcpsdk.Tool {
	return mcpsdk.NewTool("cost_report",
		mcpsdk.WithDescription("Cost report over the daemon's recorded session spend (the internal/costreport engine, "+
			"same data as GET /api/cost/report). Grouped by automaton, automation or backend (release is REST/CLI-only: it needs shipped-unit state this tool does not expose); "+
			"or week (ISO 8601 week buckets); token totals always, USD estimates when rates are configured. Rows carry "+
			"billing_mode + billing_label from the usage.billing profiles (bundled_plan → api_equivalent_usd, "+
			"'API-equivalent, not billed', plus estimated_usage_points / share_of_monthly_price where the plan is priced; "+
			"free_local → not billed; api_metered assumed when no profile is set), and the report carries a "+
			"retention_note (the ledger's 31-day retention window, and whether this window's amounts fell back to "+
			"per-session lifetime totals). When the REST loopback is up the tool returns the GET /api/cost/report body unchanged, "+
			"so every row also carries the per-Automaton columns (tokens, time, sessions, tool calls) and the report carries "+
			"window_note and trace_usage; the same per-Automaton figures appear under cost in autonomous_prd_get. group_by, since and until are all optional — the "+
			"grouping and window defaults come from the cost_report config section, not fixed literals."),
		mcpsdk.WithString("group_by", mcpsdk.Description("Optional. One of automaton, automation, backend, week (release is REST/CLI-only). Defaults to the cost_report config.")),
		mcpsdk.WithString("since", mcpsdk.Description("Optional lower bound, RFC3339 or YYYY-MM-DD. Defaults to the cost_report config window.")),
		mcpsdk.WithString("until", mcpsdk.Description("Optional upper bound, RFC3339 or YYYY-MM-DD. Defaults to now.")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

// handleCostReport — validate the optional arguments, fall back to the
// cost_report config for anything absent, run costreport.Build and
// return the Report as JSON text.
func (s *Server) handleCostReport(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	reportReq := costreport.Request{}
	if v := req.GetString("group_by", ""); v != "" {
		reportReq.GroupBy = costreport.GroupBy(v)
	}
	if v := req.GetString("since", ""); v != "" {
		t, ok := parseCostReportBound(v)
		if !ok {
			return mcpsdk.NewToolResultError("cost_report: since must be RFC3339 or YYYY-MM-DD"), nil
		}
		reportReq.Since = t
	}
	if v := req.GetString("until", ""); v != "" {
		t, ok := parseCostReportBound(v)
		if !ok {
			return mcpsdk.NewToolResultError("cost_report: until must be RFC3339 or YYYY-MM-DD"), nil
		}
		reportReq.Until = t
	}

	// Prefer the REST report: it reads the spend ledger, so rows carry the
	// per-Automaton columns and notes. Fall back to the in-process engine
	// when the loopback is unavailable.
	if s.webPort != 0 {
		q := url.Values{}
		for _, k := range []string{"group_by", "since", "until"} {
			if v := req.GetString(k, ""); v != "" {
				q.Set(k, v)
			}
		}
		if body, err := s.proxyGet("/api/cost/report", q); err == nil {
			return textOK(string(body)), nil
		}
	}

	rep, err := s.buildCostReport(reportReq)
	if err != nil {
		return mcpsdk.NewToolResultError("cost_report: " + err.Error()), nil
	}
	out, jerr := json.Marshal(rep)
	if jerr != nil {
		return mcpsdk.NewToolResultError("cost_report: " + jerr.Error()), nil
	}
	return textOK(string(out)), nil
}

// toolCostCharts declares the cost_charts tool: the combined usage report,
// read-only, both arguments optional.
func (s *Server) toolCostCharts() mcpsdk.Tool {
	return mcpsdk.NewTool("cost_charts",
		mcpsdk.WithDescription("Combined usage report over the daemon's spend ledger and Automaton attempts: cost over time "+
			"(GET /api/cost/series), tool usage (GET /api/cost/tool-usage) and retries and failure categories "+
			"(GET /api/cost/failures) joined into one payload with cost_over_time, tool_usage and failures sections, "+
			"each exactly as its REST route returns it. Cost over time and tool usage group by automaton, project or model; "+
			"retries and failures group by project, automation or automaton. A group_by one view does not know leaves "+
			"that view on its default and the payload's notes say so. Tool usage carries a count per group, never a tool name. "+
			"Read-only; needs the REST loopback. group_by and window are optional."),
		mcpsdk.WithString("group_by", mcpsdk.Description("Optional. One of automaton, project, model, automation. Each view falls back to its own default where it does not group by this.")),
		mcpsdk.WithString("window", mcpsdk.Description("Optional. How far back from now: 36h, 7d, 2w. Defaults to the cost_report default window (twelve weeks for retries and failures).")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

// handleCostCharts reads the three REST views through the loopback and
// returns the joined payload as JSON text, the same join the CLI and the
// comm channel use.
func (s *Server) handleCostCharts(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	creq := costreport.ChartsRequest{
		GroupBy: req.GetString("group_by", ""),
		Window:  req.GetString("window", ""),
	}
	charts, err := costreport.FetchCharts(creq, s.proxyGet)
	if err != nil {
		return mcpsdk.NewToolResultError("cost_charts: " + err.Error()), nil
	}
	out, err := json.Marshal(charts)
	if err != nil {
		return mcpsdk.NewToolResultError("cost_charts: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}
