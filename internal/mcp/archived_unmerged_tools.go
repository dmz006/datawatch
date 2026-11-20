// MCP tools for archived Automata whose work branch still holds unmerged
// commits. Both are stateless proxies of the daemon REST routes on the
// loopback:
//
//	GET  /api/autonomous/archived-unmerged                    → archived_unmerged_list
//	POST /api/autonomous/archived-unmerged/{id}/carry-forward → archived_unmerged_carry_forward
//
// Neither tool deletes or merges a branch; carry-forward only creates a new,
// unapproved draft.
package mcp

import (
	"context"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// archivedUnmergedPath is the REST base of the two tools.
const archivedUnmergedPath = "/api/autonomous/archived-unmerged"

// toolArchivedUnmergedList lists archived Automata with unmerged branch work.
func (s *Server) toolArchivedUnmergedList() mcpsdk.Tool {
	return mcpsdk.NewTool("archived_unmerged_list",
		mcpsdk.WithDescription("List archived Automata whose work branch still holds commits that never reached the base branch, newest archive first. Each entry gives the title, former status, branch, commits ahead and whether a draft was already carried from it. Read-only: git is only read, and nothing is deleted or merged. Proxies GET /api/autonomous/archived-unmerged."),
	)
}

// handleArchivedUnmergedList proxies GET /api/autonomous/archived-unmerged.
func (s *Server) handleArchivedUnmergedList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const tool = "archived_unmerged_list"
	respBody, err := s.proxyGet(archivedUnmergedPath, nil)
	if err != nil {
		return toolError(tool, err), nil
	}
	return textOK(string(respBody)), nil
}

// toolArchivedUnmergedCarryForward starts a new draft on an archived branch.
func (s *Server) toolArchivedUnmergedCarryForward() mcpsdk.Tool {
	return mcpsdk.NewTool("archived_unmerged_carry_forward",
		mcpsdk.WithDescription("Carry an archived Automaton's unmerged work forward: create a new draft with the same title, spec and branch so the work is not lost. The draft is not approved and has run nothing; the archived record and its branch are not changed and nothing is merged or deleted. Refused when the record is not archived, has no branch, the branch is gone or has nothing ahead, or it was already carried (pass force to carry again). Proxies POST /api/autonomous/archived-unmerged/{id}/carry-forward and returns the new draft."),
		mcpsdk.WithString("id",
			mcpsdk.Required(),
			mcpsdk.Description("Id of the archived Automaton to carry forward (from archived_unmerged_list)."),
		),
		mcpsdk.WithBoolean("force",
			mcpsdk.Description("Carry again even though a draft was already carried from this record. Default false."),
		),
		mcpsdk.WithString("actor",
			mcpsdk.Description("Who is carrying it forward, recorded on the new draft. Defaults to operator."),
		),
	)
}

// handleArchivedUnmergedCarryForward proxies the carry-forward POST.
func (s *Server) handleArchivedUnmergedCarryForward(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const tool = "archived_unmerged_carry_forward"
	id := strings.TrimSpace(req.GetString("id", ""))
	if id == "" {
		return toolErrorf(tool, "id is required"), nil
	}
	body := map[string]any{}
	if req.GetBool("force", false) {
		body["force"] = true
	}
	if actor := strings.TrimSpace(req.GetString("actor", "")); actor != "" {
		body["actor"] = actor
	}
	respBody, err := s.proxyJSON("POST", archivedUnmergedPath+"/"+url.PathEscape(id)+"/carry-forward", body)
	if err != nil {
		return toolError(tool, err), nil
	}
	return textOK(string(respBody)), nil
}
