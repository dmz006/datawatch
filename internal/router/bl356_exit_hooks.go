package router

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// handleExitHookCmd dispatches exit_hook commands over the comm channel (BL356).
func (r *Router) handleExitHookCmd(cmd Command) {
	if r.exitHookStore == nil {
		r.send("[exit_hook] Exit hook store not available. Start the daemon first.")
		return
	}

	switch cmd.ExitHookVerb {
	case "list", "":
		r.handleExitHookList()
	case "add":
		r.handleExitHookAdd(cmd)
	case "delete":
		r.handleExitHookDeleteCmd(cmd)
	case "enable":
		r.handleExitHookSetEnabled(cmd, true)
	case "disable":
		r.handleExitHookSetEnabled(cmd, false)
	default:
		r.send(fmt.Sprintf("[exit_hook] unknown verb %q. Use: list | add | delete | enable | disable", cmd.ExitHookVerb))
	}
}

func (r *Router) handleExitHookList() {
	entries := r.exitHookStore.List()
	if len(entries) == 0 {
		r.send("[exit_hook] No exit hooks configured.")
		return
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "[exit_hook] %d hook(s):\n", len(entries))
	for _, e := range entries {
		status := "enabled"
		if !e.Enabled {
			status = "disabled"
		}
		line := fmt.Sprintf("  %s  name=%-20s  action=%-8s  %s  cooldown=%ds",
			e.ID, e.Name, string(e.Action), status, e.CooldownSeconds)
		if e.Action == session.ExitHookNotify && e.NotifySession != "" {
			line += fmt.Sprintf("  notify=%s", e.NotifySession)
		}
		if !e.LastFiredAt.IsZero() {
			line += fmt.Sprintf("  last_fired=%s", e.LastFiredAt.Format(time.RFC3339))
		}
		sb.WriteString(line + "\n")
	}
	r.send(sb.String())
}

func (r *Router) handleExitHookAdd(cmd Command) {
	if cmd.ExitHookName == "" {
		r.send("[exit_hook] add requires name=<session-name>")
		return
	}
	if cmd.ExitHookAction != "restart" && cmd.ExitHookAction != "notify" {
		r.send("[exit_hook] add requires action=restart or action=notify")
		return
	}
	cooldown := cmd.ExitHookCooldown
	if cooldown <= 0 {
		cooldown = 300
	}
	e, err := r.exitHookStore.Add(cmd.ExitHookName, session.ExitHookAction(cmd.ExitHookAction),
		cmd.ExitHookNotifySession, cmd.ExitHookMessage, cooldown)
	if err != nil {
		r.send(fmt.Sprintf("[exit_hook] add failed: %v", err))
		return
	}
	r.send(fmt.Sprintf("[exit_hook] added hook %s: name=%s action=%s cooldown=%ds",
		e.ID, e.Name, string(e.Action), e.CooldownSeconds))
}

func (r *Router) handleExitHookDeleteCmd(cmd Command) {
	if cmd.ExitHookID == "" {
		r.send("[exit_hook] delete requires id=<hook-id>")
		return
	}
	if err := r.exitHookStore.Delete(cmd.ExitHookID); err != nil {
		r.send(fmt.Sprintf("[exit_hook] delete failed: %v", err))
		return
	}
	r.send(fmt.Sprintf("[exit_hook] hook %s deleted.", cmd.ExitHookID))
}

func (r *Router) handleExitHookSetEnabled(cmd Command, enabled bool) {
	if cmd.ExitHookID == "" {
		verb := "enable"
		if !enabled {
			verb = "disable"
		}
		r.send(fmt.Sprintf("[exit_hook] %s requires id=<hook-id>", verb))
		return
	}
	if err := r.exitHookStore.SetEnabled(cmd.ExitHookID, enabled); err != nil {
		r.send(fmt.Sprintf("[exit_hook] set-enabled failed: %v", err))
		return
	}
	state := "enabled"
	if !enabled {
		state = "disabled"
	}
	r.send(fmt.Sprintf("[exit_hook] hook %s %s.", cmd.ExitHookID, state))
}

// handleExitHookListJSON is a utility for tests — not currently wired.
func exitHookListJSON(entries []*session.ExitHookEntry) string {
	data, _ := json.MarshalIndent(entries, "", "  ")
	return string(data)
}
