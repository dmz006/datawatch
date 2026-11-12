// autonomous checks / ship over the chat channel: the same reads as the
// REST routes, rendered with the shared text the CLI uses. Reads only —
// nothing is run, pushed or tagged.

package router

import (
	"net/url"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const (
	checksUsage = "usage: autonomous checks [automaton-id]"
	shipUsage   = "usage: autonomous ship [automaton-id]"
)

// ChecksShipCommVerbs lists the checks and ship comm verbs for the parity test.
func ChecksShipCommVerbs() []string { return []string{"checks", "ship"} }

// handleChecksShipVerb answers the autonomous command's checks and ship verbs.
func (r *Router) handleChecksShipVerb(verb string, args []string) {
	usage, path, title := checksUsage, "/api/autonomous/checks", "autonomous checks"
	if verb == "ship" {
		usage, path, title = shipUsage, "/api/autonomous/ship", "autonomous ship"
	}
	if len(args) > 2 {
		r.reply(title+" failed", usage)
		return
	}
	q := url.Values{}
	if len(args) == 2 && strings.TrimSpace(args[1]) != "" {
		q.Set("prd_id", strings.TrimSpace(args[1]))
	}
	out, err := r.commGet(path, q)
	if err != nil {
		r.reply(title+" failed", err.Error())
		return
	}
	var text string
	if verb == "ship" {
		s, perr := autonomous.ParseShipStatus([]byte(out))
		if perr != nil {
			r.reply(title+" failed", perr.Error())
			return
		}
		text = autonomous.RenderShipStatusText(s)
	} else {
		s, perr := autonomous.ParseChecksStatus([]byte(out))
		if perr != nil {
			r.reply(title+" failed", perr.Error())
			return
		}
		text = autonomous.RenderChecksStatusText(s)
	}
	r.reply(title, strings.TrimRight(text, "\n"))
}
