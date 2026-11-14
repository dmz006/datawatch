// Sprint Sx2 — chat surface for the gate repair engine.
//
// Reads GET /api/autonomous/gate-repair and runs POST /api/autonomous/gate-repair/run
// through the in-process HTTP loopback; the text is the shared renderer the CLI uses,
// and --json prints the REST body untouched (same as the CLI's).

package router

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const gateRepairUsage = "usage: autonomous gate-repair status | run [automaton=ID task=ID]"

// GateRepairCommVerbs lists the comm verbs for the parity test.
func GateRepairCommVerbs() []string { return []string{"gate-repair", "gate_repair"} }

// handleGateRepairVerb dispatches the gate repair verb (the run surface is the
// only autonomous chat verb that also runs a pass; its targets are the named
// automaton= / task= arguments, mirroring the MCP autonomous_gate_repair_run).
func (r *Router) handleGateRepairVerb(verb string, args []string) {
	switch verb {
	case "gate-repair", "gate_repair":
		r.replyGateRepair(args)
	default:
		r.reply("autonomous gate-repair failed", gateRepairUsage)
	}
}

// replyGateRepair routes the subcommand: status (read) or run (one pass, or one
// failed gate). args[0] is the verb itself; an unknown subcommand (or bare
// use) is refused with the usage and makes no daemon call.
func (r *Router) replyGateRepair(args []string) {
	if len(args) < 2 {
		r.reply("autonomous gate-repair failed", "missing subcommand\n"+gateRepairUsage)
		return
	}
	switch args[1] {
	case "status":
		if len(args) > 2 {
			r.reply("autonomous gate-repair status failed",
				"unknown argument "+strings.Join(args[2:], " ")+"\n"+gateRepairUsage)
			return
		}
		r.replyGateRepairStatus()
	case "run":
		r.replyGateRepairRun(args[2:])
	default:
		r.reply("autonomous gate-repair failed", "unknown subcommand "+args[1]+"\n"+gateRepairUsage)
	}
}

// replyGateRepairStatus proxies GET /api/autonomous/gate-repair and renders.
func (r *Router) replyGateRepairStatus() {
	body, err := r.commGet("/api/autonomous/gate-repair", nil)
	if err != nil {
		r.reply("autonomous gate-repair status failed", err.Error())
		return
	}
	var s autonomous.GateRepairStatus
	if err := json.Unmarshal([]byte(body), &s); err != nil {
		r.reply("autonomous gate-repair status failed", err.Error())
		return
	}
	var b strings.Builder
	autonomous.RenderGateRepairs(&b, &s)
	r.reply("autonomous gate-repair status", strings.TrimRight(b.String(), "\n"))
}

// gateRepairRunBody is the POST body: both ids or neither.
func gateRepairRunBody(automaton, task string) (map[string]string, bool) {
	if (automaton == "") != (task == "") {
		return nil, false
	}
	if automaton == "" {
		return map[string]string{}, true
	}
	return map[string]string{"automaton_id": automaton, "task_id": task}, true
}

// replyGateRepairRun proxies POST /api/autonomous/gate-repair/run with the
// optional automaton= / task= targets and renders the run's outcomes and the
// fresh status.
func (r *Router) replyGateRepairRun(args []string) {
	automaton, task := "", ""
	for _, a := range args {
		k, v, ok := strings.Cut(a, "=")
		if !ok {
			r.reply("autonomous gate-repair run failed",
				"unknown argument "+a+"\n"+gateRepairUsage)
			return
		}
		switch k {
		case "automaton":
			automaton = v
		case "task":
			task = v
		default:
			r.reply("autonomous gate-repair run failed",
				"unknown argument "+a+"\n"+gateRepairUsage)
			return
		}
	}
	body, ok := gateRepairRunBody(automaton, task)
	if !ok {
		r.reply("autonomous gate-repair run failed",
			"pass automaton=ID task=ID to repair one gate, or neither to run a pass\n"+gateRepairUsage)
		return
	}
	raw, err := json.Marshal(body)
	if err != nil {
		r.reply("autonomous gate-repair run failed", err.Error())
		return
	}
	res, err := r.commJSON(http.MethodPost, "/api/autonomous/gate-repair/run", string(raw))
	if err != nil {
		r.reply("autonomous gate-repair run failed", err.Error())
		return
	}
	var out struct {
		Runs   []autonomous.GateRepairOutcome `json:"runs"`
		Status autonomous.GateRepairStatus    `json:"status"`
	}
	if err := json.Unmarshal([]byte(res), &out); err != nil {
		r.reply("autonomous gate-repair run failed", err.Error())
		return
	}
	var b strings.Builder
	autonomous.RenderGateRepairRun(&b, out.Runs)
	b.WriteString("\n")
	autonomous.RenderGateRepairs(&b, &out.Status)
	r.reply("autonomous gate-repair", strings.TrimRight(b.String(), "\n"))
}
