// autonomous triage over the chat channel: the triage read, so the
// operator can ask which Automata are waiting for triage from
// Signal / Telegram without curl. The reply renders the same shared text
// the CLI does (internal/autonomous/triage_text.go): the failed and
// blocked Automata, newest first, beside the alert threshold. A read —
// nothing is written and no retry is started.

package router

import (
	"net/url"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const (
	// triageUsage is the usage line for the triage comm verb. The optional
	// automation id narrows the read to one Automation's members.
	triageUsage = "usage: autonomous triage [automation-id]"
)

// TriageCommVerbs lists the triage comm verbs for the parity test.
func TriageCommVerbs() []string { return []string{"triage"} }

// handleTriageVerb answers the autonomous command's triage verbs.
func (r *Router) handleTriageVerb(verb string, args []string) {
	switch verb {
	case "triage":
		r.replyTriage(args)
	default:
		r.reply("autonomous triage failed", triageUsage)
	}
}

func (r *Router) replyTriage(args []string) {
	if len(args) > 2 {
		r.reply("autonomous triage failed", triageUsage)
		return
	}
	q := url.Values{}
	if len(args) == 2 && strings.TrimSpace(args[1]) != "" {
		q.Set("automation_id", strings.TrimSpace(args[1]))
	}
	out, err := r.commGet("/api/autonomous/triage", q)
	if err != nil {
		r.reply("autonomous triage failed", err.Error())
		return
	}
	tl, err := autonomous.ParseTriageList([]byte(out))
	if err != nil {
		r.reply("autonomous triage failed", err.Error())
		return
	}
	r.reply("autonomous triage", strings.TrimRight(autonomous.RenderTriageText(tl), "\n"))
}
