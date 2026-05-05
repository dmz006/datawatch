// BL260 v6.11.0 — MCP tools for Council Mode (multi-agent debate).
// All proxy to REST.
//
//	council_personas    — list registered personas
//	council_run         — execute council on a proposal
//	council_list_runs   — list past runs
//	council_get_run     — fetch one run

package mcp

import (
	"context"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolCouncilPersonas() mcpsdk.Tool {
	return mcpsdk.NewTool("council_personas",
		mcpsdk.WithDescription("BL260 — list registered Council personas (built-in 6 by default + operator-added)."),
	)
}
func (s *Server) toolCouncilRun() mcpsdk.Tool {
	return mcpsdk.NewTool("council_run",
		mcpsdk.WithDescription("BL260 — run a multi-persona Council debate on a proposal. Two modes: debate (3 rounds), quick (1 round). v6.11.0 ships the framework with stubbed LLM responses; real per-persona inference is a v6.11.x follow-up."),
		mcpsdk.WithString("proposal", mcpsdk.Required(), mcpsdk.Description("the question or design proposal to debate")),
		mcpsdk.WithString("personas", mcpsdk.Description("comma-separated persona names; empty = all registered")),
		mcpsdk.WithString("mode", mcpsdk.Description("debate (3 rounds) or quick (1 round); default quick")),
	)
}
func (s *Server) toolCouncilListRuns() mcpsdk.Tool {
	return mcpsdk.NewTool("council_list_runs",
		mcpsdk.WithDescription("BL260 — list past Council runs (most recent first)."),
		mcpsdk.WithString("limit", mcpsdk.Description("max runs to return")),
	)
}
func (s *Server) toolCouncilGetRun() mcpsdk.Tool {
	return mcpsdk.NewTool("council_get_run",
		mcpsdk.WithDescription("BL260 — fetch one persisted Council run by id."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("run id")),
	)
}

func (s *Server) handleCouncilPersonasMCP(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/council/personas", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleCouncilRunMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{
		"proposal": mustString(req, "proposal"),
		"mode":     optString(req, "mode"),
	}
	if names := splitCSV(optString(req, "personas")); len(names) > 0 {
		body["personas"] = names
	}
	out, err := s.proxyJSON("POST", "/api/council/run", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleCouncilListRunsMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	path := "/api/council/runs"
	if v := optString(req, "limit"); v != "" {
		path = path + "?limit=" + v
	}
	out, err := s.proxyGet(path, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleCouncilGetRunMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := mustString(req, "id")
	out, err := s.proxyGet("/api/council/runs/"+id, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
