// autonomous uncommitted-work over the chat channel: the uncommitted-work
// status read, so the operator can ask from Signal / Telegram which
// Automaton tasks are held because files were left uncommitted, which files
// they are, and which question waits for an answer — without curl. The reply
// renders the same shared text the CLI does
// (internal/autonomous/uncommitted_work_text.go). A read — nothing is
// committed, discarded or released.

package router

import (
	"net/url"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// uncommittedWorkUsage is the usage line for the uncommitted-work comm verb.
// The optional argument narrows the read to one Automaton.
const uncommittedWorkUsage = "usage: autonomous uncommitted-work [automaton-id]"

// UncommittedWorkCommVerbs lists the uncommitted-work comm verbs for the
// parity test.
func UncommittedWorkCommVerbs() []string { return []string{"uncommitted-work"} }

// handleUncommittedWorkVerb answers the autonomous command's uncommitted-work
// verb.
func (r *Router) handleUncommittedWorkVerb(verb string, args []string) {
	switch verb {
	case "uncommitted-work", "uncommitted_work":
		r.replyUncommittedWork(args)
	default:
		r.reply("autonomous uncommitted-work failed", uncommittedWorkUsage)
	}
}

func (r *Router) replyUncommittedWork(args []string) {
	if len(args) > 2 {
		r.reply("autonomous uncommitted-work failed", uncommittedWorkUsage)
		return
	}
	q := url.Values{}
	if len(args) == 2 && strings.TrimSpace(args[1]) != "" {
		q.Set("automaton", strings.TrimSpace(args[1]))
	}
	out, err := r.commGet("/api/autonomous/uncommitted-work", q)
	if err != nil {
		r.reply("autonomous uncommitted-work failed", err.Error())
		return
	}
	st, err := autonomous.ParseUncommittedWorkStatus([]byte(out))
	if err != nil {
		r.reply("autonomous uncommitted-work failed", err.Error())
		return
	}
	if len(st.Held) == 0 {
		r.reply("autonomous uncommitted-work", "No Automaton has uncommitted work held (policy: "+st.Policy+", scope: "+st.Scope+").")
		return
	}
	r.reply("autonomous uncommitted-work", strings.TrimRight(autonomous.RenderUncommittedWorkStatus(st), "\n"))
}
