// Chat verb for output filters: see them, including which ones type into
// sessions, and turn one off. Creating or enabling one is done in the web app,
// CLI or REST.

package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/session"
)

const filterUsage = "filter list | filter disable <id>"

func parseFilterCommand(text string) Command {
	parts := strings.Fields(text)
	cmd := Command{Type: CmdOutputFilter, Text: "list"}
	if len(parts) > 1 {
		cmd.Text = strings.ToLower(strings.Join(parts[1:], " "))
	}
	return cmd
}

func (r *Router) handleOutputFilter(cmd Command) {
	args := strings.Fields(cmd.Text)
	switch {
	case len(args) == 1 && args[0] == "list":
		body, err := r.commGet("/api/filters", nil)
		if err != nil {
			r.send(fmt.Sprintf("[%s] filter list failed: %v", r.hostname, err))
			return
		}
		var list []session.FilterPattern
		if err := json.Unmarshal([]byte(body), &list); err != nil {
			r.send(fmt.Sprintf("[%s] filter list failed: %v", r.hostname, err))
			return
		}
		r.send(fmt.Sprintf("[%s] %s", r.hostname, formatOutputFilters(list)))
	case len(args) == 2 && args[0] == "disable":
		b, _ := json.Marshal(map[string]any{"id": args[1], "enabled": false})
		if _, err := r.commJSON(http.MethodPatch, "/api/filters", string(b)); err != nil {
			r.send(fmt.Sprintf("[%s] filter disable failed: %v", r.hostname, err))
			return
		}
		r.send(fmt.Sprintf("[%s] filter %s disabled.", r.hostname, args[1]))
	default:
		r.send(fmt.Sprintf("[%s] Usage: %s", r.hostname, filterUsage))
	}
}

// formatOutputFilters lists filters without their patterns or values: those
// can quote prompt wording that should not be echoed into a chat or pane.
func formatOutputFilters(list []session.FilterPattern) string {
	if len(list) == 0 {
		return "No output filters."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d output filter(s):", len(list))
	for _, f := range list {
		state := "off"
		if f.Enabled {
			state = "on"
		}
		fmt.Fprintf(&b, "\n  %s  %-13s %s", f.ID, f.Action, state)
		if f.Action.TypesInput() {
			b.WriteString("  TYPES INPUT")
		}
		if f.DisabledReason != "" {
			b.WriteString("  (" + f.DisabledReason + ")")
		}
	}
	return b.String()
}
