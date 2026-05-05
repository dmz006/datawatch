// BL260 v6.11.0 — comm-channel verbs for Council Mode.
//
//	council                          → list personas
//	council personas                 → list personas (alias)
//	council run <mode> <proposal>    → run (mode = debate|quick)
//	council runs                     → list past runs
//	council get-run <id>             → fetch one run

package router

import (
	"encoding/json"
	"strings"
)

const councilUsage = `Usage:
  council                                list personas
  council personas                       list personas
  council run <mode> <proposal>          execute (mode = debate|quick)
  council runs                           list past runs
  council get-run <id>                   fetch one run`

func (r *Router) handleCouncilCmd(cmd Command) {
	text := strings.TrimSpace(cmd.Text)
	lower := strings.ToLower(text)

	if text == "" || lower == "personas" {
		out, err := r.commGet("/api/council/personas", nil)
		if err != nil {
			r.reply("council", err.Error())
			return
		}
		r.reply("council", prettyJSON(out))
		return
	}

	parts := strings.SplitN(text, " ", 2)
	verb := strings.ToLower(parts[0])

	switch verb {
	case "run":
		// "council run <mode> <proposal...>"
		rest := ""
		if len(parts) > 1 {
			rest = parts[1]
		}
		modeAndProp := strings.SplitN(rest, " ", 2)
		mode := "quick"
		proposal := rest
		if len(modeAndProp) == 2 && (strings.EqualFold(modeAndProp[0], "debate") || strings.EqualFold(modeAndProp[0], "quick")) {
			mode = strings.ToLower(modeAndProp[0])
			proposal = modeAndProp[1]
		}
		if proposal == "" {
			r.reply("council run", councilUsage)
			return
		}
		body, _ := json.Marshal(map[string]any{"proposal": proposal, "mode": mode})
		out, err := r.commJSON("POST", "/api/council/run", string(body))
		if err != nil {
			r.reply("council run", err.Error())
			return
		}
		r.reply("council run", prettyJSON(out))
	case "runs":
		out, err := r.commGet("/api/council/runs", nil)
		if err != nil {
			r.reply("council runs", err.Error())
			return
		}
		r.reply("council runs", prettyJSON(out))
	case "get-run":
		if len(parts) < 2 {
			r.reply("council get-run", "Usage: council get-run <id>")
			return
		}
		out, err := r.commGet("/api/council/runs/"+strings.TrimSpace(parts[1]), nil)
		if err != nil {
			r.reply("council get-run", err.Error())
			return
		}
		r.reply("council "+parts[1], prettyJSON(out))
	default:
		r.reply("council", councilUsage)
	}
}
