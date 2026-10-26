package mcp

import (
	"context"
	"net/url"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The triage tool is read-only and proxies GET /api/autonomous/triage,
// the triage records of the Automata that are waiting (failed or
// blocked), newest first, beside the alert threshold in effect.

func (s *Server) toolAutonomousTriage() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_triage",
		mcpsdk.WithDescription("Read the triage records: every Automaton that is waiting — failed or blocked — with its failed task (or block reason), the resolved retry budget, and the notes added while it waits, newest first. \"alert_minutes\" is the alert threshold currently in effect, in whole minutes (0 when the alert is off). Optional: automation_id (only that Automation's records), prd_id (only that Automaton's record), status (\"failed\" or \"blocked\" only). Read-only; the same object as GET /api/autonomous/triage."),
		mcpsdk.WithString("automation_id",
			mcpsdk.Description("Optional — only the triage records of this Automation.")),
		mcpsdk.WithString("prd_id",
			mcpsdk.Description("Optional — only the triage record of this Automaton.")),
		mcpsdk.WithString("status",
			mcpsdk.Description(`Optional — only records in this waiting state: "failed" or "blocked" (anything else is refused).`)),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleAutonomousTriage(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if v := req.GetString("automation_id", ""); v != "" {
		q.Set("automation_id", v)
	}
	if v := req.GetString("prd_id", ""); v != "" {
		q.Set("prd_id", v)
	}
	if v := req.GetString("status", ""); v != "" {
		q.Set("status", v)
	}
	out, err := s.proxyGet("/api/autonomous/triage", q)
	if err != nil {
		return toolError("autonomous_triage", err), nil
	}
	return textOK(string(out)), nil
}
