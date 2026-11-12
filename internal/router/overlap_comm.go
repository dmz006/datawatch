// autonomous overlap over the chat channel: the search-before-create read, so
// the operator can check a plan's spec for overlaps from Signal / Telegram
// before creating it. The reply renders the same shared text the CLI does
// (internal/autonomous/overlap_text.go): one warning per line. A read —
// nothing is written.

package router

import (
	"net/url"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const (
	// overlapUsage is the usage line for the overlap comm verb. The text after
	// the verb is the spec of the plan about to be created.
	overlapUsage = "usage: autonomous overlap <text>"
)

// OverlapCommVerbs lists the overlap comm verbs for the parity test.
func OverlapCommVerbs() []string { return []string{"overlap"} }

// handleOverlapVerb answers the autonomous command's overlap verb.
func (r *Router) handleOverlapVerb(verb string, args []string) {
	switch verb {
	case "overlap":
		r.replyOverlap(args)
	default:
		r.reply("autonomous overlap failed", overlapUsage)
	}
}

func (r *Router) replyOverlap(args []string) {
	var spec string
	if len(args) > 1 {
		spec = strings.TrimSpace(strings.Join(args[1:], " "))
	}
	if spec == "" {
		r.reply("autonomous overlap failed", overlapUsage)
		return
	}
	q := url.Values{}
	q.Set("spec", spec)
	out, err := r.commGet("/api/autonomous/overlap", q)
	if err != nil {
		r.reply("autonomous overlap failed", err.Error())
		return
	}
	ws, err := autonomous.ParseOverlapWarnings([]byte(out))
	if err != nil {
		r.reply("autonomous overlap failed", err.Error())
		return
	}
	r.reply("autonomous overlap", strings.TrimRight(autonomous.RenderOverlapText(ws), "\n"))
}
