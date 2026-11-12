package router

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

const automationSetEffortCeilingUsage = "usage: automation set-effort-ceiling <id> <kind>=low|medium|high|cheaper_backend ... (kinds: docs, parity, live_check, final_gate) | automation set-effort-ceiling <id> clear=true"

// automationSetEffortCeiling serves "set-effort-ceiling <id> kind=ceiling...".
// The daemon validates the values.
func (r *Router) automationSetEffortCeiling(args []string) {
	if len(args) < 3 {
		r.send(automationSetEffortCeilingUsage)
		return
	}
	ceiling := map[string]string{}
	for _, tok := range args[2:] {
		k, v, ok := strings.Cut(tok, "=")
		if !ok || k == "" {
			r.send(automationSetEffortCeilingUsage)
			return
		}
		if k == "clear" {
			if v == "true" {
				ceiling = map[string]string{}
				break
			}
			continue
		}
		ceiling[k] = v
	}
	b, _ := json.Marshal(map[string]any{"effort_ceiling": ceiling, "actor": "comm"})
	resp, err := r.commJSON(http.MethodPost, "/api/automations/"+url.PathEscape(args[1])+"/set_effort_ceiling", string(b))
	if err != nil {
		r.send("automation set-effort-ceiling error: " + err.Error())
		return
	}
	r.send(resp)
}
