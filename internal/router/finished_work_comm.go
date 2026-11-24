// Finished work over the chat channel: `autonomous finished-work` shows the
// finished work waiting to land (owner and open question per row) and
// `autonomous finished-work run sweep|audit` runs one bounded sweep pass or the
// audit now. The read changes nothing.

package router

import (
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const finishedWorkUsage = "usage: autonomous finished-work | autonomous finished-work run sweep|audit"

// FinishedWorkCommVerbs lists the comm verbs, for the parity test.
func FinishedWorkCommVerbs() []string {
	return []string{"finished-work", "finished_work"}
}

func (r *Router) handleFinishedWorkVerb(args []string) {
	if len(args) == 1 {
		r.replyFinishedWork()
		return
	}
	if len(args) == 3 && strings.EqualFold(args[1], "run") {
		run := strings.ToLower(args[2])
		if run == "sweep" || run == "audit" {
			r.replyFinishedWorkRun(run)
			return
		}
	}
	r.reply("autonomous finished-work failed", finishedWorkUsage)
}

func (r *Router) replyFinishedWork() {
	out, err := r.commGet("/api/autonomous/finished-work", nil)
	if err != nil {
		r.reply("autonomous finished-work failed", err.Error())
		return
	}
	d, err := autonomous.ParseFinishedWorkDigest([]byte(out))
	if err != nil {
		r.reply("autonomous finished-work failed", "unparseable read view: "+err.Error())
		return
	}
	var b strings.Builder
	if err := autonomous.RenderFinishedWorkDigest(&b, d); err != nil {
		r.reply("autonomous finished-work failed", err.Error())
		return
	}
	r.reply("autonomous finished-work", strings.TrimRight(b.String(), "\n"))
}

func (r *Router) replyFinishedWorkRun(run string) {
	out, err := r.commJSON(http.MethodPost, "/api/autonomous/finished-work/run", `{"run":"`+run+`"}`)
	if err != nil {
		r.reply("autonomous finished-work failed", err.Error())
		return
	}
	var b strings.Builder
	if err := autonomous.RenderFinishedWorkRun(&b, run, []byte(out)); err != nil {
		r.reply("autonomous finished-work failed", "unparseable result: "+err.Error())
		return
	}
	r.reply("autonomous finished-work "+run, strings.TrimRight(b.String(), "\n"))
}
