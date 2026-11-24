package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// Finished-work tools: the digest of finished work waiting to land (read-only,
// proxies GET /api/autonomous/finished-work) and the run tool (proxies POST
// /api/autonomous/finished-work/run) that runs one bounded sweep pass or the
// audit now. An existing weekly audit schedule can call the run tool.

func (s *Server) toolAutonomousFinishedWork() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_finished_work",
		mcpsdk.WithDescription("Read the finished work that is waiting to land: Automata that finished while their branch still holds commits not on the default branch, or whose code tasks are all done while docs or gate tasks remain, or whose left-alone date came. Each row has the cause, the branch and commit count, how long it has waited, the owner and the open question. Also the mode (autonomous.finished_work: on, shadow or off) and when the last sweep, daily digest and weekly audit ran. Takes no arguments. Read-only. The same object as GET /api/autonomous/finished-work."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleAutonomousFinishedWork(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if args := req.GetArguments(); len(args) > 0 {
		return toolError("autonomous_finished_work", errNoArguments(args)), nil
	}
	out, err := s.proxyGet("/api/autonomous/finished-work", nil)
	if err != nil {
		return toolError("autonomous_finished_work", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousFinishedWorkRun() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_finished_work_run",
		mcpsdk.WithDescription("Run the finished-work check now. run=sweep makes one bounded pass (at most 100 branch comparisons, resuming after the last Automaton it reached): finished work older than a day gets an owner and one open question, code-complete work is offered for landing, a left-alone branch is asked about again on its date. run=audit reads every Automaton, cancelled and branchless ones included, and writes the result as a decision record (once per week). Both read git read-only and never merge, never touch a session and never change a task or an Automaton status. With autonomous.finished_work=shadow they record findings only; with off they do nothing. An existing weekly audit schedule can call this tool with run=audit. The same object as POST /api/autonomous/finished-work/run."),
		mcpsdk.WithString("run", mcpsdk.Required(), mcpsdk.Description("sweep or audit")),
	)
}

func (s *Server) handleAutonomousFinishedWorkRun(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	run := strings.ToLower(strings.TrimSpace(req.GetString("run", "")))
	body, _ := json.Marshal(map[string]string{"run": run})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/finished-work/run", body)
	if err != nil {
		return toolError("autonomous_finished_work_run", err), nil
	}
	return textOK(string(out)), nil
}

// FinishedWorkToolNames lists the finished-work tools, for the parity test.
func FinishedWorkToolNames() []string {
	return []string{"autonomous_finished_work", "autonomous_finished_work_run"}
}
