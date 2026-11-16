// MCP tools for landing state — thin proxies over the landing keep/drop
// REST endpoints:
//
//	autonomous_landing_keep → POST /api/autonomous/prds/{id}/landing/keep
//	autonomous_landing_drop → POST /api/autonomous/prds/{id}/landing/drop
//
// The landing record itself (state: pending / landed / kept / dropped,
// commit, actor, at, reason) already rides on every Automaton (PRD) read;
// these two tools are the operator decisions that move a pending record to
// kept or dropped. Both need a non-empty reason and are refused once the
// work branch has landed.

package mcp

import (
	"context"
	"net/http"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// LandingToolNames lists the landing MCP tools in registration order so the
// parity test enumerates the MCP surface without a second, hand-maintained
// list drifting from the code.
func LandingToolNames() []string {
	return []string{"autonomous_landing_keep", "autonomous_landing_drop"}
}

func (s *Server) toolAutonomousLandingKeep() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_landing_keep",
		mcpsdk.WithDescription("Record that an Automaton's work branch is kept unmerged on purpose: its landing record moves from pending to kept, with who decided (actor) and the reason. The landing record — state, commit, actor, at, reason — is already part of every Automaton read; this tool changes it. The reason must be non-empty. Refused once the branch has landed, for an Automaton with no branch, or for an already-kept/dropped record. Returns the updated Automaton."),
		mcpsdk.WithString("id", mcpsdk.Description("The id of the Automaton whose branch is kept (required)")),
		mcpsdk.WithString("reason", mcpsdk.Description("Why the work is not being merged (required)")),
		mcpsdk.WithString("actor", mcpsdk.Description("Optional: who is deciding (defaults to the operator)")),
	)
}

func (s *Server) toolAutonomousLandingDrop() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_landing_drop",
		mcpsdk.WithDescription("Record that an Automaton's work branch is dropped and its work will not be merged: its landing record moves from pending to dropped, with who decided (actor) and the reason. The landing record — state, commit, actor, at, reason — is already part of every Automaton read; this tool changes it. The reason must be non-empty. Refused once the branch has landed, for an Automaton with no branch, or for an already-kept/dropped record. Returns the updated Automaton."),
		mcpsdk.WithString("id", mcpsdk.Description("The id of the Automaton whose branch is dropped (required)")),
		mcpsdk.WithString("reason", mcpsdk.Description("Why the work will not be merged (required)")),
		mcpsdk.WithString("actor", mcpsdk.Description("Optional: who is deciding (defaults to the operator)")),
	)
}

func (s *Server) handleAutonomousLandingKeep(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	args := req.GetArguments()
	id, _ := args["id"].(string)
	reason, _ := args["reason"].(string)
	if strings.TrimSpace(id) == "" {
		return toolErrorf("autonomous_landing_keep", "id is required"), nil
	}
	if strings.TrimSpace(reason) == "" {
		return toolErrorf("autonomous_landing_keep", "reason is required"), nil
	}
	body := map[string]string{"reason": strings.TrimSpace(reason)}
	if actor, _ := args["actor"].(string); strings.TrimSpace(actor) != "" {
		body["actor"] = strings.TrimSpace(actor)
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+strings.TrimSpace(id)+"/landing/keep", body)
	if err != nil {
		return toolError("autonomous_landing_keep", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) handleAutonomousLandingDrop(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	args := req.GetArguments()
	id, _ := args["id"].(string)
	reason, _ := args["reason"].(string)
	if strings.TrimSpace(id) == "" {
		return toolErrorf("autonomous_landing_drop", "id is required"), nil
	}
	if strings.TrimSpace(reason) == "" {
		return toolErrorf("autonomous_landing_drop", "reason is required"), nil
	}
	body := map[string]string{"reason": strings.TrimSpace(reason)}
	if actor, _ := args["actor"].(string); strings.TrimSpace(actor) != "" {
		body["actor"] = strings.TrimSpace(actor)
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+strings.TrimSpace(id)+"/landing/drop", body)
	if err != nil {
		return toolError("autonomous_landing_drop", err), nil
	}
	return textOK(string(out)), nil
}
