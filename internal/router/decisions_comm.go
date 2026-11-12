// autonomous decisions over the chat channel: the decision line, so the
// operator can read the newest decisions from Signal / Telegram without
// curl. The reply renders the same shared text the CLI does
// (internal/autonomous/decision_text.go), at most ten lines, newest first.
// A read — nothing is written.

package router

import (
	"net/url"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const (
	// decisionsUsage is the usage line for the decisions comm verb. The
	// optional automaton id narrows the read to one Automaton's decisions.
	decisionsUsage = "usage: autonomous decisions [automaton-id]"

	// decisionsCommLines is the most lines one reply carries.
	decisionsCommLines = 10
)

// DecisionsCommVerbs lists the decisions comm verbs for the parity test.
func DecisionsCommVerbs() []string { return []string{"decisions"} }

// handleDecisionsVerb answers the autonomous command's decisions verbs.
func (r *Router) handleDecisionsVerb(verb string, args []string) {
	switch verb {
	case "decisions":
		r.replyDecisions(args)
	default:
		r.reply("autonomous decisions failed", decisionsUsage)
	}
}

func (r *Router) replyDecisions(args []string) {
	if len(args) > 2 {
		r.reply("autonomous decisions failed", decisionsUsage)
		return
	}
	q := url.Values{}
	if len(args) == 2 && strings.TrimSpace(args[1]) != "" {
		q.Set("prd_id", strings.TrimSpace(args[1]))
	}
	q.Set("limit", "10")
	out, err := r.commGet("/api/autonomous/decisions", q)
	if err != nil {
		r.reply("autonomous decisions failed", err.Error())
		return
	}
	es, err := autonomous.ParseDecisionLine([]byte(out))
	if err != nil {
		r.reply("autonomous decisions failed", err.Error())
		return
	}
	r.reply("autonomous decisions", strings.TrimRight(autonomous.RenderDecisionLines(es, decisionsCommLines), "\n"))
}
