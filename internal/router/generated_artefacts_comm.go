// generated_artefacts_comm.go — comm-channel verbs for the generated
// artefacts of an Automaton:
//
//	autonomous generated-artefacts
//	autonomous generated-artefacts set <key> <on|off>
//	autonomous change-record <id>
//	autonomous regenerate <id> <kind>
//
// They call the same REST routes as the CLI and the web UI.
package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	generatedArtefactsUsage = "usage: autonomous generated-artefacts [set <key> <on|off>]"
	changeRecordUsage       = "usage: autonomous change-record <automaton-id>"
	regenerateUsage         = "usage: autonomous regenerate <automaton-id> <kind>"
)

func (r *Router) handleGeneratedArtefactsVerb(verb string, args []string) {
	switch verb {
	case "generated-artefacts", "generated_artefacts":
		r.replyGeneratedArtefacts(args)
	case "change-record", "change_record":
		if len(args) != 2 {
			r.reply("autonomous change-record failed", changeRecordUsage)
			return
		}
		out, err := r.commJSON(http.MethodGet, "/api/autonomous/prds/"+url.PathEscape(args[1])+"/change-record", "")
		if err != nil {
			r.reply("autonomous change-record failed", err.Error())
			return
		}
		r.reply("autonomous change-record", prettyJSON(out))
	case "regenerate":
		if len(args) != 3 {
			r.reply("autonomous regenerate failed", regenerateUsage)
			return
		}
		out, err := r.commJSON(http.MethodPost,
			"/api/autonomous/prds/"+url.PathEscape(args[1])+"/generated/"+url.PathEscape(args[2])+"/regenerate", "{}")
		if err != nil {
			r.reply("autonomous regenerate failed", err.Error())
			return
		}
		r.reply("autonomous regenerate", prettyJSON(out))
	}
}

func (r *Router) replyGeneratedArtefacts(args []string) {
	if len(args) == 1 {
		out, err := r.commJSON(http.MethodGet, "/api/autonomous/generated-artefacts", "")
		if err != nil {
			r.reply("autonomous generated-artefacts failed", err.Error())
			return
		}
		r.reply("autonomous generated-artefacts", prettyJSON(out))
		return
	}
	if strings.ToLower(args[1]) != "set" || len(args) != 4 {
		r.reply("autonomous generated-artefacts failed", generatedArtefactsUsage)
		return
	}
	var on bool
	switch strings.ToLower(args[3]) {
	case "on", "true":
		on = true
	case "off", "false":
		on = false
	default:
		r.reply("autonomous generated-artefacts failed", generatedArtefactsUsage+" (the value must be on or off)")
		return
	}
	body, _ := json.Marshal(map[string]any{"autonomous.generated_artefacts." + args[2]: on})
	if _, err := r.commJSON(http.MethodPut, "/api/config", string(body)); err != nil {
		r.reply("autonomous generated-artefacts failed", err.Error())
		return
	}
	state := "off"
	if on {
		state = "on"
	}
	r.reply("autonomous generated-artefacts", fmt.Sprintf("%s is now %s", args[2], state))
}
