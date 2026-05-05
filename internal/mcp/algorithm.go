// BL258 v6.9.0 — MCP tools for the Algorithm Mode 7-phase harness.
// All proxy to the REST surface.
//
//	algorithm_list                  — list every active session
//	algorithm_get                   — read one session's state
//	algorithm_start                 — register a session in Algorithm Mode
//	algorithm_advance               — close current phase, advance
//	algorithm_edit                  — replace last recorded phase output
//	algorithm_abort                 — terminate mid-algorithm
//	algorithm_reset                 — clear state

package mcp

import (
	"context"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolAlgorithmList() mcpsdk.Tool {
	return mcpsdk.NewTool("algorithm_list",
		mcpsdk.WithDescription("BL258 — list every session currently in Algorithm Mode (the 7-phase Observe→Improve harness)."),
	)
}
func (s *Server) toolAlgorithmGet() mcpsdk.Tool {
	return mcpsdk.NewTool("algorithm_get",
		mcpsdk.WithDescription("BL258 — read one session's algorithm state (current phase, history, timestamps)."),
		mcpsdk.WithString("session_id", mcpsdk.Required(), mcpsdk.Description("session id")),
	)
}
func (s *Server) toolAlgorithmStart() mcpsdk.Tool {
	return mcpsdk.NewTool("algorithm_start",
		mcpsdk.WithDescription("BL258 — register a session in Algorithm Mode beginning at Observe."),
		mcpsdk.WithString("session_id", mcpsdk.Required(), mcpsdk.Description("session id")),
	)
}
func (s *Server) toolAlgorithmAdvance() mcpsdk.Tool {
	return mcpsdk.NewTool("algorithm_advance",
		mcpsdk.WithDescription("BL258 — close the current phase by recording its output and advance to the next."),
		mcpsdk.WithString("session_id", mcpsdk.Required(), mcpsdk.Description("session id")),
		mcpsdk.WithString("output", mcpsdk.Description("free-form text capturing what was done in this phase")),
	)
}
func (s *Server) toolAlgorithmEdit() mcpsdk.Tool {
	return mcpsdk.NewTool("algorithm_edit",
		mcpsdk.WithDescription("BL258 — replace the output captured at the most recent phase gate."),
		mcpsdk.WithString("session_id", mcpsdk.Required(), mcpsdk.Description("session id")),
		mcpsdk.WithString("output", mcpsdk.Required(), mcpsdk.Description("revised phase output")),
	)
}
func (s *Server) toolAlgorithmAbort() mcpsdk.Tool {
	return mcpsdk.NewTool("algorithm_abort",
		mcpsdk.WithDescription("BL258 — terminate the algorithm mid-flight; subsequent advance calls error."),
		mcpsdk.WithString("session_id", mcpsdk.Required(), mcpsdk.Description("session id")),
	)
}
func (s *Server) toolAlgorithmReset() mcpsdk.Tool {
	return mcpsdk.NewTool("algorithm_reset",
		mcpsdk.WithDescription("BL258 — discard the session's algorithm state so a fresh Start begins from Observe."),
		mcpsdk.WithString("session_id", mcpsdk.Required(), mcpsdk.Description("session id")),
	)
}

// ── handlers ────────────────────────────────────────────────────────────

func (s *Server) handleAlgorithmList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/algorithm", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleAlgorithmGet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := mustString(req, "session_id")
	out, err := s.proxyGet("/api/algorithm/"+id, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleAlgorithmStart(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := mustString(req, "session_id")
	out, err := s.proxyJSON("POST", "/api/algorithm/"+id+"/start", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleAlgorithmAdvance(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := mustString(req, "session_id")
	body := map[string]any{"output": optString(req, "output")}
	out, err := s.proxyJSON("POST", "/api/algorithm/"+id+"/advance", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleAlgorithmEdit(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := mustString(req, "session_id")
	body := map[string]any{"output": mustString(req, "output")}
	out, err := s.proxyJSON("POST", "/api/algorithm/"+id+"/edit", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleAlgorithmAbort(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := mustString(req, "session_id")
	out, err := s.proxyJSON("POST", "/api/algorithm/"+id+"/abort", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleAlgorithmReset(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := mustString(req, "session_id")
	out, err := s.proxyJSON("DELETE", "/api/algorithm/"+id, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
