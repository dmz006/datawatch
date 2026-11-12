package mcp

import (
	"context"
	"net/url"
	"strconv"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The decisions tool is read-only and proxies GET /api/autonomous/decisions,
// the operator decisions newest first, each with the tasks that name it and
// its current answer.

func (s *Server) toolAutonomousDecisions() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_decisions",
		mcpsdk.WithDescription("Read the operator decisions, newest first: each with its question, label, state (open, answered, withdrawn or expired), who asked and when, the Automaton it belongs to, task_ids (the tasks of that Automaton whose spec names the decision), waiting_task_ids (those currently waiting on it) and answer (null until answered; then the answer id, the operator's words verbatim in quote, quote_sha256, actor, surface and answered_at). Optional: prd_id (only that Automaton's decisions), state (\"open\", \"answered\", \"withdrawn\" or \"expired\" only), limit (default 100, maximum 1000). Read-only; the same object as GET /api/autonomous/decisions."),
		mcpsdk.WithString("prd_id",
			mcpsdk.Description("Optional — only the decisions linked to this Automaton.")),
		mcpsdk.WithString("state",
			mcpsdk.Description(`Optional — only decisions in this state: "open", "answered", "withdrawn" or "expired" (anything else is refused).`)),
		mcpsdk.WithNumber("limit",
			mcpsdk.Description("Optional — at most this many decisions (default 100, maximum 1000).")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

// DecisionToolNames lists the decision tools, for the parity test.
func DecisionToolNames() []string {
	return []string{"autonomous_decisions"}
}

func (s *Server) handleAutonomousDecisions(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if v := req.GetString("prd_id", ""); v != "" {
		q.Set("prd_id", v)
	}
	if v := req.GetString("state", ""); v != "" {
		q.Set("state", v)
	}
	if n := req.GetInt("limit", 0); n != 0 {
		q.Set("limit", strconv.Itoa(n))
	}
	out, err := s.proxyGet("/api/autonomous/decisions", q)
	if err != nil {
		return toolError("autonomous_decisions", err), nil
	}
	return textOK(string(out)), nil
}
