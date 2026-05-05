// BL259 Phase 1 v6.10.0 — MCP tools for the Evals framework. All
// proxy to REST.
//
//	eval_list_suites  — list defined eval suites
//	eval_run          — execute a suite, return Run
//	eval_list_runs    — list runs (most recent first)
//	eval_get_run      — read one run by id

package mcp

import (
	"context"
	"net/url"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolEvalListSuites() mcpsdk.Tool {
	return mcpsdk.NewTool("eval_list_suites",
		mcpsdk.WithDescription("BL259 — list eval suites defined under ~/.datawatch/evals/."),
	)
}
func (s *Server) toolEvalRun() mcpsdk.Tool {
	return mcpsdk.NewTool("eval_run",
		mcpsdk.WithDescription("BL259 — execute an eval suite by name. Returns the Run with per-case results, pass_rate, and pass/fail vs threshold."),
		mcpsdk.WithString("suite", mcpsdk.Required(), mcpsdk.Description("suite name (matches ~/.datawatch/evals/<name>.yaml)")),
	)
}
func (s *Server) toolEvalListRuns() mcpsdk.Tool {
	return mcpsdk.NewTool("eval_list_runs",
		mcpsdk.WithDescription("BL259 — list past eval runs (most recent first), optionally filtered by suite."),
		mcpsdk.WithString("suite", mcpsdk.Description("filter by suite name (optional)")),
		mcpsdk.WithString("limit", mcpsdk.Description("max runs to return (default unlimited)")),
	)
}
func (s *Server) toolEvalGetRun() mcpsdk.Tool {
	return mcpsdk.NewTool("eval_get_run",
		mcpsdk.WithDescription("BL259 — fetch one persisted eval run by id."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("run id")),
	)
}

// ── handlers ────────────────────────────────────────────────────────────

func (s *Server) handleEvalListSuites(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/evals/suites", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleEvalRun(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	suite := mustString(req, "suite")
	out, err := s.proxyJSON("POST", "/api/evals/run?suite="+url.QueryEscape(suite), nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleEvalListRuns(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if v := optString(req, "suite"); v != "" {
		q.Set("suite", v)
	}
	if v := optString(req, "limit"); v != "" {
		q.Set("limit", v)
	}
	path := "/api/evals/runs"
	if enc := q.Encode(); enc != "" {
		path = path + "?" + enc
	}
	out, err := s.proxyGet(path, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleEvalGetRun(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := mustString(req, "id")
	out, err := s.proxyGet("/api/evals/runs/"+id, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
