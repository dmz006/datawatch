// Gate repair surface tools: autonomous_gate_repair_status and
// autonomous_gate_repair_run.
//
// The status tool is read-only and proxies GET /api/autonomous/gate-repair —
// the engine's mode, its records (with the before/after texts of a file edit)
// and the repair budget split per class per task. It takes no arguments; one
// that is supplied is refused rather than ignored.
//
// The run tool proxies POST /api/autonomous/gate-repair/run — it runs the
// engine now. With both automaton_id and task_id it resolves exactly that gate;
// with neither it runs one pass across every failed task gate on the daemon.
// Supplying exactly one of the two is refused (the REST route 400s it too). The
// engine mode is changed only through the existing config set paths
// (autonomous.gate_repair.mode), never by these tools.
package mcp

import (
	"context"
	"net/http"
	"sort"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

const gateRepairStatusUsage = "autonomous_gate_repair_status takes no arguments (read the gate repair mode, records and budget)"

const gateRepairRunUsage = `usage: autonomous_gate_repair_run [automaton_id=ID task_id=ID]
  automaton_id + task_id  run the gate repair for exactly that failed gate
  (no arguments)          run one pass across every failed task gate on the daemon
  exactly one of the two  refused — give both or neither`

func (s *Server) toolGateRepairStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_gate_repair_status",
		mcpsdk.WithDescription("Read the gate repair surface: the mode in effect (\"off\", \"report\" or \"on\"), the repair records newest last (for each failed gate: the failure class found, what it would do or did, how to undo it, and the before/after script texts of a file edit), and the repair budget spent split per class per task ({task_id, class}). Takes no arguments; any argument is refused. Read-only: reading never repairs anything, raises an alert or changes a mode (change the mode with the config set tool, key autonomous.gate_repair.mode). The same object as GET /api/autonomous/gate-repair."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) toolGateRepairRun() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_gate_repair_run",
		mcpsdk.WithDescription("Run the gate repair engine now and return the outcome(s) plus a fresh status. Give both automaton_id and task_id to resolve exactly that failed gate, or give neither to run one pass across every failed task gate on the daemon. Giving exactly one of the two is refused. The engine honours its mode and repair budget (report records what it would do and acts on nothing; the mode is changed with the config set tool, key autonomous.gate_repair.mode). The same request as POST /api/autonomous/gate-repair/run."),
		mcpsdk.WithString("automaton_id",
			mcpsdk.Description(`Optional — the Automaton owning the failed gate. Must be given together with task_id (give both, or give neither to run a pass).`)),
		mcpsdk.WithString("task_id",
			mcpsdk.Description(`Optional — the failed task whose gate to repair. Must be given together with automaton_id (give both, or give neither to run a pass).`)),
	)
}

// GateRepairToolNames lists the gate repair surface tools, for the parity test.
func GateRepairToolNames() []string {
	return []string{"autonomous_gate_repair_status", "autonomous_gate_repair_run"}
}

func (s *Server) handleGateRepairStatus(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if args := req.GetArguments(); len(args) > 0 {
		names := make([]string, 0, len(args))
		for k := range args {
			names = append(names, k)
		}
		sort.Strings(names)
		return toolErrorf("autonomous_gate_repair_status", "unknown argument(s) %s; %s", strings.Join(names, ", "), gateRepairStatusUsage), nil
	}
	out, err := s.proxyGet("/api/autonomous/gate-repair", nil)
	if err != nil {
		return toolError("autonomous_gate_repair_status", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) handleGateRepairRun(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	automatonID := strings.TrimSpace(req.GetString("automaton_id", ""))
	taskID := strings.TrimSpace(req.GetString("task_id", ""))

	args := req.GetArguments()
	var extra []string
	for k := range args {
		if k != "automaton_id" && k != "task_id" {
			extra = append(extra, k)
		}
	}
	if len(extra) > 0 {
		names := make([]string, 0, len(extra))
		names = append(names, extra...)
		sort.Strings(names)
		return toolErrorf("autonomous_gate_repair_run", "unknown argument(s) %s; %s", strings.Join(names, ", "), gateRepairRunUsage), nil
	}
	if (automatonID == "") != (taskID == "") {
		return toolErrorf("autonomous_gate_repair_run", "give both automaton_id and task_id, or neither; got only %s", whicheverSet(automatonID, taskID)), nil
	}

	body := map[string]any{}
	if automatonID != "" {
		body["automaton_id"] = automatonID
		body["task_id"] = taskID
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/gate-repair/run", body)
	if err != nil {
		return toolError("autonomous_gate_repair_run", err), nil
	}
	return textOK(string(out)), nil
}

// whicheverSet names the single argument that is set, for the "both or
// neither" refusal message.
func whicheverSet(automatonID, taskID string) string {
	if automatonID == "" {
		return "task_id"
	}
	return "automaton_id"
}
