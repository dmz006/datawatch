package mcp

import (
	"context"
	"net/url"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The merge tools are read-only. autonomous_merge proxies
// GET /api/autonomous/merge and autonomous_safe_to_delete proxies
// GET /api/autonomous/safe-to-delete. Neither merges, deletes or pushes.

func (s *Server) toolAutonomousMerge() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_merge",
		mcpsdk.WithDescription("Read the Automaton merge state: the merge-pending mode in effect (\"mode\": off, shadow or on), the alert threshold in whole minutes (\"alert_minutes\", 0 when off), one entry per finished Automaton with a work branch (\"automata\"), and in shadow mode the decisions the pass would have acted on, newest first (\"shadow_decisions\"). While the mode is off both lists are empty. Read-only; the same object as GET /api/autonomous/merge. Nothing is merged, deleted or pushed."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) toolAutonomousSafeToDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_safe_to_delete",
		mcpsdk.WithDescription("Read the safe-to-delete report for Automaton work branches and worktrees: \"safe\" lists the branches that can be deleted, \"held\" lists the ones that must stay (not merged yet, or a worktree with uncommitted changes) and \"held_dirty\" counts the dirty ones. Read-only; the same object as GET /api/autonomous/safe-to-delete. It never deletes anything — deleting is a separate step the operator approves."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

// MergeToolNames lists the merge tools, for the parity test.
func MergeToolNames() []string {
	return []string{"autonomous_merge", "autonomous_safe_to_delete"}
}

func (s *Server) handleAutonomousMerge(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/autonomous/merge", url.Values{})
	if err != nil {
		return toolError("autonomous_merge", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) handleAutonomousSafeToDelete(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/autonomous/safe-to-delete", url.Values{})
	if err != nil {
		return toolError("autonomous_safe_to_delete", err), nil
	}
	return textOK(string(out)), nil
}
