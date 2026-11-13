// Gate repair over the chat channel: the engine read, rendered with the same
// renderer the CLI uses. A read — nothing is repaired, no alert is raised and
// no mode changes (the mode changes only through the config set paths).

package router

import (
	"encoding/json"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const gateRepairsUsage = "usage: autonomous gate-repairs"

// GateRepairsCommVerbs lists the comm verbs, for the parity test.
func GateRepairsCommVerbs() []string {
	return []string{"gate-repairs", "gate_repairs"}
}

func (r *Router) handleGateRepairsVerb(verb string, args []string) {
	switch verb {
	case "gate-repairs", "gate_repairs":
		r.replyGateRepairs(args)
	default:
		r.reply("autonomous gate-repairs failed", gateRepairsUsage)
	}
}

func (r *Router) replyGateRepairs(args []string) {
	if len(args) > 1 {
		r.reply("autonomous gate-repairs failed",
			"unknown argument "+strings.Join(args[1:], " ")+"\n"+gateRepairsUsage)
		return
	}
	out, err := r.commGet("/api/autonomous/gate-repairs", nil)
	if err != nil {
		r.reply("autonomous gate-repairs failed", err.Error())
		return
	}
	var body autonomous.GateRepairStatus
	if err := json.Unmarshal([]byte(out), &body); err != nil {
		r.reply("autonomous gate-repairs failed", "unparseable read view: "+err.Error())
		return
	}
	var b strings.Builder
	autonomous.RenderGateRepairs(&b, &body)
	r.reply("autonomous gate-repairs", strings.TrimRight(b.String(), "\n"))
}
