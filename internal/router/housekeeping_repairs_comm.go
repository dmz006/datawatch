// Housekeeping repairs over the chat channel: the read view, rendered with the
// same renderer the CLI uses. A read — nothing is repaired, no alert is raised
// and no mode changes (modes change only through the config set paths).

package router

import (
	"encoding/json"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const housekeepingRepairsUsage = "usage: autonomous housekeeping-repairs"

// HousekeepingRepairsCommVerbs lists the comm verbs, for the parity test.
func HousekeepingRepairsCommVerbs() []string {
	return []string{"housekeeping-repairs", "housekeeping_repairs"}
}

func (r *Router) handleHousekeepingRepairsVerb(verb string, args []string) {
	switch verb {
	case "housekeeping-repairs", "housekeeping_repairs":
		r.replyHousekeepingRepairs(args)
	default:
		r.reply("autonomous housekeeping-repairs failed", housekeepingRepairsUsage)
	}
}

func (r *Router) replyHousekeepingRepairs(args []string) {
	if len(args) > 1 {
		r.reply("autonomous housekeeping-repairs failed",
			"unknown argument "+strings.Join(args[1:], " ")+"\n"+housekeepingRepairsUsage)
		return
	}
	out, err := r.commGet("/api/autonomous/housekeeping-repairs", nil)
	if err != nil {
		r.reply("autonomous housekeeping-repairs failed", err.Error())
		return
	}
	var body autonomous.HousekeepingRepairsView
	if err := json.Unmarshal([]byte(out), &body); err != nil {
		r.reply("autonomous housekeeping-repairs failed", "unparseable read view: "+err.Error())
		return
	}
	var b strings.Builder
	autonomous.RenderHousekeepingRepairs(&b, &body)
	r.reply("autonomous housekeeping-repairs", strings.TrimRight(b.String(), "\n"))
}
