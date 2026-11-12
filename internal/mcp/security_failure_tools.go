package mcp

import (
	"context"
	"net/url"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The security failure list tool is read-only and proxies GET
// /api/autonomous/security-failures: the stored security failure
// reports of failing security runs, newest first, each beside the
// identity of the run it belongs to.

func (s *Server) toolSecurityFailureList() mcpsdk.Tool {
	return mcpsdk.NewTool("security_failure_list",
		mcpsdk.WithDescription("List the stored security failure reports, newest first. Each entry carries the run's identity (\"repo\", \"ref_kind\", \"ref_name\", \"workflow\", \"run_id\", \"run_url\", \"status\") and the stored report under \"report\" (dependency, advisory ids, fixed version, accepted-risk registry result, recommendation, the mode and the action the Automaton line received). Optional: repo (only that repository's reports), status (\"green\", \"failing\", \"pending\", \"hung\", \"rerunning\" or \"superseded\" only). Read-only; the same JSON list as GET /api/autonomous/security-failures."),
		mcpsdk.WithString("repo",
			mcpsdk.Description(`Optional — only the reports of this repository path (e.g. "dmz006/datawatch").`)),
		mcpsdk.WithString("status",
			mcpsdk.Description(`Optional — only reports whose run is in this watch state: "green", "failing", "pending", "hung", "rerunning" or "superseded" (anything else is refused).`)),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

// SecurityFailureToolNames lists the security failure tools, for the parity test.
func SecurityFailureToolNames() []string {
	return []string{"security_failure_list"}
}

func (s *Server) handleSecurityFailureList(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if v := req.GetString("repo", ""); v != "" {
		q.Set("repo", v)
	}
	if v := req.GetString("status", ""); v != "" {
		q.Set("status", v)
	}
	out, err := s.proxyGet("/api/autonomous/security-failures", q)
	if err != nil {
		return toolError("security_failure_list", err), nil
	}
	return textOK(string(out)), nil
}
