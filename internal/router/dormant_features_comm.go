// Dormant features over the chat channel: the read-only registry of Automata
// settings that are off or in shadow mode, rendered with the same renderer the
// CLI uses. A read — nothing is changed and no Automaton runs.

package router

import (
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const dormantFeaturesUsage = "usage: autonomous dormant-features"

// DormantFeaturesCommVerbs lists the comm verbs, for the parity test.
func DormantFeaturesCommVerbs() []string {
	return []string{"dormant-features", "dormant_features"}
}

func (r *Router) handleDormantFeaturesVerb(verb string, args []string) {
	switch verb {
	case "dormant-features", "dormant_features":
		r.replyDormantFeatures(args)
	default:
		r.reply("autonomous dormant-features failed", dormantFeaturesUsage)
	}
}

func (r *Router) replyDormantFeatures(args []string) {
	if len(args) > 1 {
		r.reply("autonomous dormant-features failed",
			"unknown argument "+strings.Join(args[1:], " ")+"\n"+dormantFeaturesUsage)
		return
	}
	out, err := r.commGet("/api/autonomous/dormant-features", nil)
	if err != nil {
		r.reply("autonomous dormant-features failed", err.Error())
		return
	}
	body, err := autonomous.ParseDormantFeatures([]byte(out))
	if err != nil {
		r.reply("autonomous dormant-features failed", "unparseable read view: "+err.Error())
		return
	}
	var b strings.Builder
	if err := autonomous.RenderDormantFeatures(&b, body.Features); err != nil {
		r.reply("autonomous dormant-features failed", err.Error())
		return
	}
	r.reply("autonomous dormant-features", strings.TrimRight(b.String(), "\n"))
}
