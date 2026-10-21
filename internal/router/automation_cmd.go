// Comm-channel verb for Automations: "automation activate <id>" starts a
// draft Automation. Sibling of cmd/datawatch/cli_automation.go and the
// automation_activate MCP tool; all three call POST
// /api/automations/{id}/activate.

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
		r.send("usage: automation activate <id>")
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
	default:
		r.send(fmt.Sprintf("automation: unknown subcommand %q. Try: activate", args[0]))
	}
}
