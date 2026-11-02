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
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

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
func (s *Server) buildCostReport(req costreport.Request) (*costreport.Report, error) {
	if s.manager == nil {
		return nil, errCostReportUnavailable
	}
	return costreport.Build(s.costReportCfg, req, managerCostReportSource{m: s.manager}, s.costReportRate, time.Now())
}

// toolCostReport declares the cost_report tool: cost report over the
// daemon's recorded session spend, all three arguments optional.
func (s *Server) toolCostReport() mcpsdk.Tool {
	return mcpsdk.NewTool("cost_report",
		mcpsdk.WithDescription("Cost report over the daemon's recorded session spend (the internal/costreport engine, "+
			"same data as GET /api/cost/report). Grouped by automaton, automation, backend or week; token totals "+
			"always, USD estimates when rates are configured. group_by, since and until are all optional — the "+
			"grouping and window defaults come from the cost_report config section, not fixed literals."),
		mcpsdk.WithString("group_by", mcpsdk.Description("Optional. One of automaton, automation, backend, week. Defaults to the cost_report config.")),
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
