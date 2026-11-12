package mcp

import (
	"context"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/federation"
)

// toolSecretScanStats declares the MCP mirror of
// GET /api/git/secret-scan/stats — the daemon's pre-push secret-scan
// counters and the effective git.secret_scan mode.
func (s *Server) toolSecretScanStats() mcpsdk.Tool {
	return mcpsdk.NewTool(
		"secret_scan_stats",
		mcpsdk.WithDescription(
			"Read the daemon's pre-push secret-scan counters since daemon start: runs, findings, shadow_reports (shadow-mode findings, push proceeded), refusals (enforce-mode refusals), last_run_at and last_refusal_at — plus the effective git.secret_scan mode (off, shadow or enforce). Read-only: it proxies GET /api/git/secret-scan/stats and changes nothing. The counters are in-memory and reset on daemon restart; needs the health read capability when called by a federation peer.",
		),
	)
}

// handleSecretScanStats proxies the REST endpoint and returns its JSON
// body verbatim as text content; a non-2xx or missing loopback is an MCP
// error naming this tool, same as every other proxying tool.
func (s *Server) handleSecretScanStats(ctx context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if deny := mcpFedCap(ctx, federation.CapHealthRead); deny != nil {
		return deny, nil
	}
	out, err := s.proxyGet("/api/git/secret-scan/stats", nil)
	if err != nil {
		return toolError("secret_scan_stats", err), nil
	}
	return textOK(string(out)), nil
}
