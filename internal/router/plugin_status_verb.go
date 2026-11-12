// plugin status — the generic service-plugin status verb.
//
//	plugin status <name>
//
// Every service-kind plugin gets this verb — no per-plugin declaration.
// The handler renders the /api/plugins/<name> view in plain text with a
// fixed field whitelist: state, mode, address, version, uptime, last
// health check and the plugin's open service event (any still-open
// health/invocation error). It never pretty-prints the manifest, so a
// secret value an operator might expect in it (env secret refs, the
// connection auth token, anything else) cannot reach the chat channel,
// and the reply itself never carries a secret value.

package router

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// pluginStatusService is the router's view of the service block of
// /api/plugins/<name> (the daemon's serviceView and attachServiceView
// shapes, shared fields only). Omitted fields are left at their zero
// values.
type pluginStatusService struct {
	Mode             string  `json:"mode"`
	State            string  `json:"state"`
	Address          string  `json:"address"`
	Version          string  `json:"version"`
	UptimeSeconds    *int64  `json:"uptime_seconds"`
	LastHealthCheck  *string `json:"last_health_check"`
	LastHealthStatus string  `json:"last_health_status"`
	LastHealthError  string  `json:"last_health_error"`
	LastError        string  `json:"last_error"`
}

// pluginStatusView is the whitelisted slice of /api/plugins/<name> this
// verb renders.
type pluginStatusView struct {
	Name      string               `json:"name"`
	Kind      string               `json:"kind"`
	LastError string               `json:"last_error"`
	Service   *pluginStatusService `json:"service"`
}

// handlePluginStatus dispatches `plugin status <name>`.
func (r *Router) handlePluginStatus(cmd Command) {
	const usage = "usage: plugin status <name>"
	args := strings.Fields(strings.TrimSpace(cmd.Text))

	if len(args) == 0 || strings.ToLower(args[0]) != "status" {
		r.reply("plugin", usage)
		return
	}
	if len(args) < 2 || args[1] == "" {
		r.reply("plugin status", usage)
		return
	}
	name := args[1]

	out, err := r.commGet("/api/plugins/"+url.PathEscape(name), nil)
	if err != nil {
		if strings.Contains(err.Error(), "HTTP 404") {
			r.reply("plugin status", fmt.Sprintf("unknown plugin %q\n%s", name, usage))
			return
		}
		r.reply("plugin status failed", err.Error())
		return
	}
	r.reply("plugin status "+name, renderPluginStatus(out))
}

// renderPluginStatus turns the /api/plugins/<name> JSON into the plain
// text reply. Only the whitelisted fields above ever appear in the
// output.
func renderPluginStatus(body string) string {
	var v pluginStatusView
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		return "unreadable plugin status: " + err.Error()
	}
	if v.Kind != "" && v.Kind != "service" {
		return fmt.Sprintf("plugin %q is kind %q — plugin status covers service-kind plugins", v.Name, v.Kind)
	}
	s := v.Service
	if s == nil {
		return fmt.Sprintf("plugin %q has no service status (kind %q)", v.Name, v.Kind)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "plugin %s — service (%s mode)\n", v.Name, s.Mode)
	if s.State != "" {
		fmt.Fprintf(&b, "  state:      %s\n", s.State)
	}
	if s.Address != "" {
		fmt.Fprintf(&b, "  address:    %s\n", s.Address)
	}
	if s.Version != "" {
		fmt.Fprintf(&b, "  version:    %s\n", s.Version)
	}
	if s.UptimeSeconds != nil && *s.UptimeSeconds >= 0 {
		fmt.Fprintf(&b, "  uptime:     %s\n", (time.Duration(*s.UptimeSeconds) * time.Second).String())
	}
	if s.LastHealthCheck != nil && *s.LastHealthCheck != "" {
		line := "  last check: " + *s.LastHealthCheck
		if s.LastHealthStatus != "" {
			line += fmt.Sprintf(" (probe %s)", s.LastHealthStatus)
		}
		b.WriteString(line + "\n")
	}

	event := strings.TrimSpace(s.LastHealthError)
	if event == "" {
		event = strings.TrimSpace(s.LastError)
	}
	if event == "" {
		event = strings.TrimSpace(v.LastError)
	}
	if event == "" {
		b.WriteString("  open event: none\n")
	} else {
		b.WriteString("  open event: " + event + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
