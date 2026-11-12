package mcp

import (
	"context"
	"net/url"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The uncommitted-work tool is read-only. autonomous_uncommitted_work proxies
// GET /api/autonomous/uncommitted-work: the policy and scope in effect and the
// tasks and Automata held because work was left uncommitted. It never commits,
// discards or releases anything; the operator answers the held question for that.

func (s *Server) toolAutonomousUncommittedWork() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_uncommitted_work",
		mcpsdk.WithDescription("Read the Automaton uncommitted-work status: the policy in effect (\"policy\"), the scope it checks (\"scope\") and the list of holds (\"held\", an empty list when nothing is held). Each held entry names the Automaton (\"automaton_id\", \"name\"), the task (\"task_id\", \"task_title\"), what the hold is on (\"kind\", task or worktree), the uncommitted files (\"files\"), the files outside the task's declared files (\"outside_scope\"), why it is held (\"reason\"), the question waiting for an answer (\"question_id\": commit, discard or retry) and when the hold was placed (\"held_at\"). Read-only; the same object as GET /api/autonomous/uncommitted-work. Nothing is committed or discarded."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
		mcpsdk.WithString("automaton", mcpsdk.Description("Optional Automaton ID; narrows the holds to that Automaton.")),
	)
}

// UncommittedWorkToolNames lists the uncommitted-work tools, for the parity test.
func UncommittedWorkToolNames() []string {
	return []string{"autonomous_uncommitted_work"}
}

func (s *Server) handleAutonomousUncommittedWork(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if id := req.GetString("automaton", ""); id != "" {
		q.Set("automaton", id)
	}
	out, err := s.proxyGet("/api/autonomous/uncommitted-work", q)
	if err != nil {
		return toolError("autonomous_uncommitted_work", err), nil
	}
	return textOK(string(out)), nil
}
