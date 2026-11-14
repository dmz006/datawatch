package mcp

import (
	"context"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The unlanded-work tool is read-only. autonomous_unlanded_work proxies
// GET /api/autonomous/unlanded-work: the archived Automaton branches whose work
// is not on the default branch. It never keeps, archives or deletes anything;
// the operator uses the Housekeeping actions, the CLI or the chat verbs for that.

func (s *Server) toolAutonomousUnlandedWork() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_unlanded_work",
		mcpsdk.WithDescription("Read the list of archived Automaton branches with work that is not on the default branch (\"branches\", an empty list when nothing is unlanded). Each entry names the Automaton (\"automaton_id\", \"title\"), the branch (\"branch\"), how many commits are not on the default branch (\"commits\"), the time of the last commit (\"last_activity\"), the merge state (\"merge_state\": needs_decision, superseded or landed), the recommended action (\"recommendation\") and why (\"reason\"). Read-only; the same object as GET /api/autonomous/unlanded-work. Nothing is kept, archived or deleted."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

// UnlandedWorkToolNames lists the unlanded-work tools, for the parity test.
func UnlandedWorkToolNames() []string {
	return []string{"autonomous_unlanded_work"}
}

func (s *Server) handleAutonomousUnlandedWork(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/autonomous/unlanded-work", nil)
	if err != nil {
		return toolError("autonomous_unlanded_work", err), nil
	}
	return textOK(string(out)), nil
}
