// BL362 — channel_diagnostics MCP tool.
//
// Forwards to GET /api/channel/diagnostics so agents can inspect the
// bridge state (kind, per-session ports, live /health probes, hints)
// without needing REST access or shell-out.

package mcp

import (
	"context"
	"net/http"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolChannelDiagnostics() mcpsdk.Tool {
	return mcpsdk.NewTool("channel_diagnostics",
		mcpsdk.WithDescription("Diagnose the MCP channel bridge: bridge kind (go|js), per-session listening ports, live /health probe results, and actionable remediation hints. Use this when sessions fail to connect or when MCP errors appear with no clear cause."),
	)
}

func (s *Server) handleChannelDiagnostics(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodGet, "/api/channel/diagnostics", nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}
