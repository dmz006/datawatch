// autonomous supervisor watch over the chat channel: the supervisor
// watch read, so the operator can ask from Signal / Telegram what the
// daemon-side watch has found without curl. The reply renders the same
// read view the GET route serves (internal/server/supervisor_watch.go)
// through the same renderer the CLI uses
// (autonomous.RenderSupervisorWatch), so both surfaces return the same
// lines: the mode in effect (off, shadow or on), the last poll, one
// line per finding, and the comparison against the supervisor script's
// output. A read — nothing is written and no alert is raised by the
// verb. The alerts the watch itself raises are system alerts and reach
// the chat channels through the existing alert fan-out, not through
// this verb.

package router

import (
	"encoding/json"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const (
	// supervisorWatchUsage is the usage line for the supervisor watch
	// comm verb. The read takes no arguments.
	supervisorWatchUsage = "usage: autonomous supervisor-watch"
)

// SupervisorWatchCommVerbs lists the supervisor watch comm verbs for
// the parity test.
func SupervisorWatchCommVerbs() []string {
	return []string{"supervisor-watch", "supervisor_watch"}
}

// handleSupervisorWatchVerb answers the autonomous command's
// supervisor watch verbs.
func (r *Router) handleSupervisorWatchVerb(verb string, args []string) {
	switch verb {
	case "supervisor-watch", "supervisor_watch":
		r.replySupervisorWatch(args)
	default:
		r.reply("autonomous supervisor-watch failed", supervisorWatchUsage)
	}
}

// renderSupervisorWatchText renders the comm reply body for the read
// view with the CLI's renderer, without the trailing newline.
func renderSupervisorWatchText(body *autonomous.SupervisorWatchStatus) string {
	var b strings.Builder
	autonomous.RenderSupervisorWatch(&b, body)
	return strings.TrimRight(b.String(), "\n")
}

func (r *Router) replySupervisorWatch(args []string) {
	if len(args) > 1 {
		r.reply("autonomous supervisor-watch failed",
			"unknown argument "+strings.Join(args[1:], " ")+"\n"+supervisorWatchUsage)
		return
	}
	out, err := r.commGet("/api/autonomous/supervisor-watch", nil)
	if err != nil {
		r.reply("autonomous supervisor-watch failed", err.Error())
		return
	}
	var body autonomous.SupervisorWatchStatus
	if err := json.Unmarshal([]byte(out), &body); err != nil {
		r.reply("autonomous supervisor-watch failed", "unparseable read view: "+err.Error())
		return
	}
	r.reply("autonomous supervisor-watch", renderSupervisorWatchText(&body))
}
