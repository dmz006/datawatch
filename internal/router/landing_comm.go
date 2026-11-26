// autonomous landing over the chat channel: the landing record of one
// Automaton (state, commit, actor, at, reason) and the operator keep/drop
// decisions on a pending record. `autonomous landing <id>` shows the
// record; `autonomous landing keep|drop <id> <reason...>` records a
// decision. The record itself already rides on the Automaton read
// (GET /api/autonomous/prds/<id>, its landing field) — this verb is the
// same data plus the two operator decisions.

package router

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const landingUsage = "usage: autonomous landing <id> | landing keep <id> <reason...> [actor=<who>] | landing drop <id> <reason...> [actor=<who>] | landing accept <id> <commit> <reason...> [actor=<who>] | landing selection [<id>]"

// LandingCommVerbs lists the landing comm verbs for the parity test.
func LandingCommVerbs() []string {
	return []string{"landing", "landing-keep", "landing-drop"}
}

// handleLandingVerb answers the autonomous command's landing verb.
func (r *Router) handleLandingVerb(verb string, args []string) {
	switch verb {
	case "landing":
		r.replyLanding("show", args)
	case "landing-keep", "landing_keep":
		r.replyLanding("keep", args)
	case "landing-drop", "landing_drop":
		r.replyLanding("drop", args)
	default:
		r.reply("autonomous landing failed", landingUsage)
	}
}

func (r *Router) replyLanding(action string, args []string) {
	if action == "show" && len(args) >= 2 {
		switch strings.ToLower(strings.TrimSpace(args[1])) {
		case "accept":
			r.replyLandingAccept(args[2:])
			return
		case "selection":
			r.replyLandingSelection(args[2:])
			return
		}
	}
	if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
		r.reply("autonomous landing failed", landingUsage)
		return
	}
	// args[0] is the verb token itself (landing / landing-keep /
	// landing-drop), as the dispatch passes it.
	first := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(args[1]), "_", "-"))
	if action == "show" && (first == "keep" || first == "drop") {
		if len(args) < 3 || strings.TrimSpace(args[2]) == "" {
			r.reply("autonomous landing "+first+" failed", landingUsage)
			return
		}
		// `landing keep|drop <id> <reason...>`: the decision word comes
		// first, as the usage line documents it (both orders work).
		r.replyLandingDecision(first, strings.TrimSpace(args[2]), args[3:])
		return
	}
	id := strings.TrimSpace(args[1])
	rest := args[2:]
	if action == "show" && len(rest) > 0 {
		switch a := strings.ToLower(strings.ReplaceAll(rest[0], "_", "-")); a {
		case "keep":
			action, rest = "keep", rest[1:]
		case "drop":
			action, rest = "drop", rest[1:]
		default:
			r.reply("autonomous landing failed", landingUsage)
			return
		}
	}
	if action == "show" {
		out, err := r.commGet("/api/autonomous/prds/"+id, nil)
		if err != nil {
			r.reply("autonomous landing "+id, err.Error())
			return
		}
		if line := autonomous.LandingLineFromPRD([]byte(out)); line != "" {
			r.reply("autonomous landing "+id, line)
			return
		}
		r.reply("autonomous landing "+id, "no landing record yet — the Automaton has no work branch, or its landing was never started")
		return
	}
	r.replyLandingDecision(action, id, rest)
}

// replyLandingDecision posts a keep or drop decision for one PRD, parsing
// the reason words and the optional actor=<who> token from rest.
func (r *Router) replyLandingDecision(action, id string, rest []string) {
	var reasonParts []string
	payload := map[string]string{}
	for _, a := range rest {
		if strings.HasPrefix(a, "actor=") {
			if who := strings.TrimSpace(a[len("actor="):]); who != "" {
				payload["actor"] = who
			}
			continue
		}
		if strings.TrimSpace(a) != "" {
			reasonParts = append(reasonParts, a)
		}
	}
	reason := strings.Join(reasonParts, " ")
	if reason == "" {
		r.reply("autonomous landing "+action+" failed", "a reason is required: autonomous landing "+action+" <id> <reason...> [actor=<who>]")
		return
	}
	payload["reason"] = reason
	body, _ := json.Marshal(payload)
	out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/landing/"+action, string(body))
	if err != nil {
		r.reply("autonomous landing "+action+" failed", err.Error())
		return
	}
	line := autonomous.LandingLineFromPRD([]byte(out))
	if line == "" {
		line = "landing recorded"
	}
	r.reply("autonomous landing "+action+" "+id, line)
}

// replyLandingAccept records the acceptance of one exact commit for merging:
// `landing accept <id> <commit> <reason...> [actor=<who>]`.
func (r *Router) replyLandingAccept(args []string) {
	payload := map[string]string{}
	var words []string
	for _, a := range args {
		if who, ok := strings.CutPrefix(a, "actor="); ok {
			payload["actor"] = strings.TrimSpace(who)
		} else if strings.TrimSpace(a) != "" {
			words = append(words, a)
		}
	}
	if len(words) < 3 {
		r.reply("autonomous landing accept failed", "usage: autonomous landing accept <id> <commit> <reason...> [actor=<who>]")
		return
	}
	id := words[0]
	payload["commit"], payload["reason"] = words[1], strings.Join(words[2:], " ")
	body, _ := json.Marshal(payload)
	if _, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/landing/accept", string(body)); err != nil {
		r.reply("autonomous landing accept failed", err.Error())
		return
	}
	r.reply("autonomous landing accept "+id, "accepted commit "+payload["commit"]+" for merge; nothing has been merged or pushed")
}

// replyLandingSelection shows what the merge selection rule picks and why it
// leaves the rest out: `landing selection [<id>]`.
func (r *Router) replyLandingSelection(args []string) {
	q := url.Values{"selection": {"true"}}
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		q.Set("automaton", strings.TrimSpace(args[0]))
	}
	out, err := r.commGet("/api/autonomous/merge", q)
	if err != nil {
		r.reply("autonomous landing selection failed", err.Error())
		return
	}
	sel, err := autonomous.ParseMergeSelection([]byte(out))
	if err != nil {
		r.reply("autonomous landing selection failed", err.Error())
		return
	}
	r.reply("autonomous landing selection", strings.TrimRight(autonomous.RenderMergeSelection(sel), "\n"))
}
