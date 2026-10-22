// Comm-channel verbs for Automations: "automation activate <id>" starts a
// draft Automation; "automation list" and "automation get <id>" read them.
// Sibling of cmd/datawatch/cli_automation.go and the automation_* MCP tools;
// activate calls POST /api/automations/{id}/activate, list and get call
// GET /api/automations and GET /api/automations/{id}. Building or editing
// an Automation stays off comm (REST, MCP, CLI).

package router

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func (r *Router) handleAutomationCmd(cmd Command) {
	args := strings.Fields(cmd.Text)
	if len(args) == 0 {
		r.send("usage: automation activate <id> | list | get <id>")
		return
	}
	switch args[0] {
	case "activate":
		if len(args) < 2 {
			r.send("usage: automation activate <id>")
			return
		}
		resp, err := r.commJSON(http.MethodPost,
			"/api/automations/"+url.PathEscape(args[1])+"/activate", `{"actor":"comm"}`)
		if err != nil {
			r.send("automation activate error: " + err.Error())
			return
		}
		r.send(resp)
	case "list":
		resp, err := r.commJSON(http.MethodGet, "/api/automations", "")
		if err != nil {
			r.send("automation list error: " + err.Error())
			return
		}
		r.send(resp)
	case "get":
		if len(args) < 2 {
			r.send("usage: automation get <id>")
			return
		}
		resp, err := r.commJSON(http.MethodGet, "/api/automations/"+url.PathEscape(args[1]), "")
		if err != nil {
			r.send("automation get error: " + err.Error())
			return
		}
		r.send(resp)
	default:
		r.send(fmt.Sprintf("automation: unknown subcommand %q. Try: activate, list, get", args[0]))
	}
}
