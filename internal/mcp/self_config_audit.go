// BL412 — self_config_audit: the read-only MCP view over the self-config
// audit trail.
//
// Every attempt that went through the self-config path — a config-write
// MCP tool, a session/peer REST config PUT, or a comm `configure` command
// — leaves exactly one record in the shared audit sink
// (federation.ConfigAuditor). This tool wraps the in-process REST view
// GET /api/audit/self-config, same loopback pattern as audit_query.

package mcp

import (
	"context"
	"net/url"
	"strconv"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// toolSelfConfigAudit declares the read-only self-config audit view.
func (s *Server) toolSelfConfigAudit() mcpsdk.Tool {
	return mcpsdk.NewTool("self_config_audit",
		mcpsdk.WithDescription("Read the self-config audit log: one record per attempt through the "+
			"self-config path on this daemon (config-write MCP tools, session/peer REST config PUTs, comm "+
			"configure), allowed AND (by default) denied, newest-first. Each record names the time, surface "+
			"(mcp/rest/comm), tool or route, the changed key(s) with secret values redacted, the caller "+
			"(session or peer), whether the write was allowed, and the denial reason. "+
			"Use it to answer 'what config did the agent change, and what was it refused to change'."),
		mcpsdk.WithNumber("limit", mcpsdk.Description("Max entries (default 100, max 1000)")),
	)
}

// handleSelfConfigAudit proxies GET /api/audit/self-config.
func (s *Server) handleSelfConfigAudit(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if n := int(req.GetFloat("limit", 0)); n > 0 {
		q.Set("limit", strconv.Itoa(n))
	}
	out, err := s.proxyGet("/api/audit/self-config", q)
	if err != nil {
		return toolError("self_config_audit", err), nil
	}
	return textOK(string(out)), nil
}
