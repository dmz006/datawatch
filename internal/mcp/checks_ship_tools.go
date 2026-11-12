package mcp

import (
	"context"
	"net/url"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The checks and ship status tools are read-only and proxy
// GET /api/autonomous/checks and GET /api/autonomous/ship.

func statusToolOptions() []mcpsdk.ToolOption {
	return []mcpsdk.ToolOption{
		mcpsdk.WithString("prd_id",
			mcpsdk.Description("Optional — only the findings of this Automaton.")),
		mcpsdk.WithString("project_dir",
			mcpsdk.Description("Optional — resolve the policy for this project directory (per-project overrides apply). Empty uses the global values.")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	}
}

func (s *Server) toolChecksStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("checks_status",
		append([]mcpsdk.ToolOption{mcpsdk.WithDescription("Read the automatic checks: the resolved mode of each check (pre-start verifier, plan rules, after-done checklist, registry-versus-README — each off, shadow or on) and the findings they recorded, newest first. Read-only; the same object as GET /api/autonomous/checks.")}, statusToolOptions()...)...)
}

func (s *Server) toolShipStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("ship_status",
		append([]mcpsdk.ToolOption{mcpsdk.WithDescription("Read the ship step: the resolved push, tag and CI-watch modes (off, shadow or on), tag format, remote and CI workflows, and the outcomes recorded for finished Automata, newest first. Read-only; the same object as GET /api/autonomous/ship.")}, statusToolOptions()...)...)
}

// ChecksShipToolNames lists the checks and ship tools, for the parity test.
func ChecksShipToolNames() []string { return []string{"checks_status", "ship_status"} }

func (s *Server) statusProxy(tool, path string, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if v := req.GetString("prd_id", ""); v != "" {
		q.Set("prd_id", v)
	}
	if v := req.GetString("project_dir", ""); v != "" {
		q.Set("project_dir", v)
	}
	out, err := s.proxyGet(path, q)
	if err != nil {
		return toolError(tool, err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) handleChecksStatus(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	return s.statusProxy("checks_status", "/api/autonomous/checks", req)
}

func (s *Server) handleShipStatus(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	return s.statusProxy("ship_status", "/api/autonomous/ship", req)
}
