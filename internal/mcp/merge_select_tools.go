// MCP tools for the merge selection rule — thin proxies:
//
//	autonomous_prd_accept_for_merge    → POST /api/autonomous/prds/{id}/landing/accept
//	autonomous_prd_merge_selection → GET /api/autonomous/merge?selection=true (or ?automaton=ID)

package mcp

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// MergeSelectToolNames lists the merge selection tools, for the parity test.
func MergeSelectToolNames() []string {
	return []string{"autonomous_prd_accept_for_merge", "autonomous_prd_merge_selection"}
}

func (s *Server) toolAutonomousPRDAcceptForMerge() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_accept_for_merge",
		mcpsdk.WithDescription("Record that the operator or the supervisor accepts one exact commit of an Automaton's branch for merging, and why. Use it only for work whose review is pending or timed out, or that is needs_review. Failed, blocked, cancelled, archived and review-blocked work cannot be accepted. commit must be the full id of the commit the branch points at; an acceptance never covers a later commit. A session doing Automaton work cannot accept. It records and audits the actor; it merges and pushes nothing. Returns the updated Automaton."),
		mcpsdk.WithString("id", mcpsdk.Description("The id of the Automaton (required)")),
		mcpsdk.WithString("commit", mcpsdk.Description("The full commit id being accepted (required)")),
		mcpsdk.WithString("reason", mcpsdk.Description("Why it is accepted (required)")),
		mcpsdk.WithString("actor", mcpsdk.Description("Optional: who is accepting (defaults to the operator; a call the daemon knows comes from a session is recorded as that session)")),
	)
}

func (s *Server) handleAutonomousPRDAcceptForMerge(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "autonomous_prd_accept_for_merge"
	args := req.GetArguments()
	arg := func(k string) string { v, _ := args[k].(string); return strings.TrimSpace(v) }
	for _, k := range []string{"id", "commit", "reason"} {
		if arg(k) == "" {
			return toolErrorf(op, "%s is required", k), nil
		}
	}
	body := map[string]string{"commit": arg("commit"), "reason": arg("reason"), "actor": arg("actor")}
	if sid, scoped := s.scopedCallerSessionID(ctx); scoped && sid != "" {
		body["actor"] = "session:" + sid
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+url.PathEscape(arg("id"))+"/landing/accept", body)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDMergeSelection() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_merge_selection",
		mcpsdk.WithDescription("Read the merge selection rule's answer: which Automaton branches datawatch may merge and why (review_passed or accepted), and every excluded one with its reasons. Without id it answers for the branches waiting to merge, the candidates of the supervisor loop merge job; with id, for that Automaton. Read-only: nothing is merged or pushed."),
		mcpsdk.WithString("id", mcpsdk.Description("Optional — one Automaton id")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleAutonomousPRDMergeSelection(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "autonomous_prd_merge_selection"
	q := url.Values{"selection": {"true"}}
	if id, _ := req.GetArguments()["id"].(string); strings.TrimSpace(id) != "" {
		q.Set("automaton", strings.TrimSpace(id))
	}
	out, err := s.proxyGet("/api/autonomous/merge", q)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(string(out)), nil
}
