// BL220 — Configuration Accessibility Rule gap closure (2026-05-02).
//
// Nine new comm-channel commands surface features that were previously
// only reachable via REST passthrough (rest GET /api/...).  Each
// handler follows the same pattern as sx2_parity.go: proxy to the
// in-process loopback, pretty-print the response.
//
// G4  orchestrator  — /api/orchestrator/*
// G5  plugins       — /api/plugins/*
// G8  templates     — /api/templates/*
// G9  device-alias  — /api/device-aliases/*
// G11 detection     — /api/diagnose
// G14 analytics     — /api/analytics
// G16 observer      — /api/observer/* (beyond peers)
// G17 routing       — /api/routing-rules/*
// G24 splash        — /api/splash/info

package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// handleOrchestrator — BL220-G4.
//
//	orchestrator                        → list graphs
//	orchestrator config                 → orchestrator config
//	orchestrator verdicts               → verdicts list
//	orchestrator get <id>               → graph detail
//	orchestrator create <title> <proj>  → POST /api/orchestrator/graphs
//	orchestrator plan <id>              → POST .../plan
//	orchestrator run <id>               → POST .../run
//	orchestrator delete <id>            → DELETE .../graphs/<id>
func (r *Router) handleOrchestrator(cmd Command) {
	const help = "usage: orchestrator [config|verdicts|get <id>|create <title> <project>|plan <id>|run <id>|delete <id>]"
	args := strings.Fields(strings.TrimSpace(cmd.Text))

	if len(args) == 0 {
		out, err := r.commGet("/api/orchestrator/graphs", nil)
		if err != nil {
			r.reply("orchestrator failed", err.Error())
			return
		}
		r.reply("orchestrator", prettyJSON(out))
		return
	}

	verb := strings.ToLower(args[0])
	switch verb {
	case "config":
		out, err := r.commGet("/api/orchestrator/config", nil)
		if err != nil {
			r.reply("orchestrator config failed", err.Error())
			return
		}
		r.reply("orchestrator config", prettyJSON(out))
	case "verdicts":
		out, err := r.commGet("/api/orchestrator/verdicts", nil)
		if err != nil {
			r.reply("orchestrator verdicts failed", err.Error())
			return
		}
		r.reply("orchestrator verdicts", prettyJSON(out))
	case "get", "show":
		if len(args) < 2 {
			r.reply("orchestrator get failed", "usage: orchestrator get <id>")
			return
		}
		out, err := r.commGet("/api/orchestrator/graphs/"+args[1], nil)
		if err != nil {
			r.reply("orchestrator get failed", err.Error())
			return
		}
		r.reply("orchestrator get", prettyJSON(out))
	case "create":
		if len(args) < 3 {
			r.reply("orchestrator create failed", "usage: orchestrator create <title> <project-dir>")
			return
		}
		title := args[1]
		projectDir := strings.Join(args[2:], " ")
		body, _ := json.Marshal(map[string]string{"title": title, "project_dir": projectDir})
		out, err := r.commJSON(http.MethodPost, "/api/orchestrator/graphs", string(body))
		if err != nil {
			r.reply("orchestrator create failed", err.Error())
			return
		}
		r.reply("orchestrator create", prettyJSON(out))
	case "plan":
		if len(args) < 2 {
			r.reply("orchestrator plan failed", "usage: orchestrator plan <id>")
			return
		}
		out, err := r.commJSON(http.MethodPost, "/api/orchestrator/graphs/"+args[1]+"/plan", "")
		if err != nil {
			r.reply("orchestrator plan failed", err.Error())
			return
		}
		r.reply("orchestrator plan", prettyJSON(out))
	case "run":
		if len(args) < 2 {
			r.reply("orchestrator run failed", "usage: orchestrator run <id>")
			return
		}
		out, err := r.commJSON(http.MethodPost, "/api/orchestrator/graphs/"+args[1]+"/run", "")
		if err != nil {
			r.reply("orchestrator run failed", err.Error())
			return
		}
		r.reply("orchestrator run", prettyJSON(out))
	case "delete":
		if len(args) < 2 {
			r.reply("orchestrator delete failed", "usage: orchestrator delete <id>")
			return
		}
		out, err := r.commJSON(http.MethodDelete, "/api/orchestrator/graphs/"+args[1], "")
		if err != nil {
			r.reply("orchestrator delete failed", err.Error())
			return
		}
		r.reply("orchestrator delete", prettyJSON(out))
	default:
		r.reply("orchestrator", "unknown verb "+verb+"\n"+help)
	}
}

// handlePlugins — BL220-G5.
//
//	plugins                                → list
//	plugins get <name>                     → detail
//	plugins enable <name>                  → enable
//	plugins disable <name>                 → disable
//	plugins reload                         → rescan dir
//	plugins test <name> <hook> [payload]   → invoke hook
func (r *Router) handlePlugins(cmd Command) {
	const help = "usage: plugins [get <name>|enable <name>|disable <name>|reload|test <name> <hook> [payload]]"
	args := strings.Fields(strings.TrimSpace(cmd.Text))

	if len(args) == 0 {
		out, err := r.commGet("/api/plugins", nil)
		if err != nil {
			r.reply("plugins failed", err.Error())
			return
		}
		r.reply("plugins", prettyJSON(out))
		return
	}

	verb := strings.ToLower(args[0])
	switch verb {
	case "get", "show":
		if len(args) < 2 {
			r.reply("plugins get failed", "usage: plugins get <name>")
			return
		}
		out, err := r.commGet("/api/plugins/"+args[1], nil)
		if err != nil {
			r.reply("plugins get failed", err.Error())
			return
		}
		r.reply("plugins get", prettyJSON(out))
	case "enable":
		if len(args) < 2 {
			r.reply("plugins enable failed", "usage: plugins enable <name>")
			return
		}
		out, err := r.commJSON(http.MethodPost, "/api/plugins/"+args[1]+"/enable", "")
		if err != nil {
			r.reply("plugins enable failed", err.Error())
			return
		}
		r.reply("plugins enable", prettyJSON(out))
	case "disable":
		if len(args) < 2 {
			r.reply("plugins disable failed", "usage: plugins disable <name>")
			return
		}
		out, err := r.commJSON(http.MethodPost, "/api/plugins/"+args[1]+"/disable", "")
		if err != nil {
			r.reply("plugins disable failed", err.Error())
			return
		}
		r.reply("plugins disable", prettyJSON(out))
	case "reload":
		out, err := r.commJSON(http.MethodPost, "/api/plugins/reload", "")
		if err != nil {
			r.reply("plugins reload failed", err.Error())
			return
		}
		r.reply("plugins reload", prettyJSON(out))
	case "test":
		if len(args) < 3 {
			r.reply("plugins test failed", "usage: plugins test <name> <hook> [payload]")
			return
		}
		payload := ""
		if len(args) >= 4 {
			payload = strings.Join(args[3:], " ")
		}
		body, _ := json.Marshal(map[string]string{"hook": args[2], "payload": payload})
		out, err := r.commJSON(http.MethodPost, "/api/plugins/"+args[1]+"/test", string(body))
		if err != nil {
			r.reply("plugins test failed", err.Error())
			return
		}
		r.reply("plugins test", prettyJSON(out))
	default:
		r.reply("plugins", "unknown verb "+verb+"\n"+help)
	}
}

// handleTemplates — BL220-G8.
//
//	templates             → list
//	templates get <name>  → fetch one
//	templates delete <name> → delete
func (r *Router) handleTemplates(cmd Command) {
	const help = "usage: templates [get <name>|delete <name>]"
	args := strings.Fields(strings.TrimSpace(cmd.Text))

	if len(args) == 0 {
		out, err := r.commGet("/api/templates", nil)
		if err != nil {
			r.reply("templates failed", err.Error())
			return
		}
		r.reply("templates", prettyJSON(out))
		return
	}

	verb := strings.ToLower(args[0])
	switch verb {
	case "get", "show":
		if len(args) < 2 {
			r.reply("templates get failed", "usage: templates get <name>")
			return
		}
		out, err := r.commGet("/api/templates/"+args[1], nil)
		if err != nil {
			r.reply("templates get failed", err.Error())
			return
		}
		r.reply("templates get", prettyJSON(out))
	case "delete":
		if len(args) < 2 {
			r.reply("templates delete failed", "usage: templates delete <name>")
			return
		}
		out, err := r.commJSON(http.MethodDelete, "/api/templates/"+args[1], "")
		if err != nil {
			r.reply("templates delete failed", err.Error())
			return
		}
		r.reply("templates delete", prettyJSON(out))
	default:
		r.reply("templates", "unknown verb "+verb+"\n"+help)
	}
}

// handleRouting — BL220-G17.
//
//	routing           → list routing rules
//	routing test <task> → POST /api/routing-rules/test
func (r *Router) handleRouting(cmd Command) {
	args := strings.Fields(strings.TrimSpace(cmd.Text))

	if len(args) == 0 {
		out, err := r.commGet("/api/routing-rules", nil)
		if err != nil {
			r.reply("routing failed", err.Error())
			return
		}
		r.reply("routing", prettyJSON(out))
		return
	}

	verb := strings.ToLower(args[0])
	switch verb {
	case "test":
		task := strings.TrimSpace(strings.Join(args[1:], " "))
		if task == "" {
			r.reply("routing test failed", "usage: routing test <task-description>")
			return
		}
		body, _ := json.Marshal(map[string]string{"task": task})
		out, err := r.commJSON(http.MethodPost, "/api/routing-rules/test", string(body))
		if err != nil {
			r.reply("routing test failed", err.Error())
			return
		}
		r.reply("routing test", prettyJSON(out))
	default:
		r.reply("routing", "unknown verb "+verb+"\nusage: routing [test <task>]")
	}
}

// handleDeviceAlias — BL220-G9.
//
//	device-alias                     → list aliases
//	device-alias add <alias> <server> → POST /api/device-aliases
//	device-alias delete <alias>       → DELETE /api/device-aliases/<alias>
func (r *Router) handleDeviceAlias(cmd Command) {
	const help = "usage: device-alias [add <alias> <server>|delete <alias>]"
	args := strings.Fields(strings.TrimSpace(cmd.Text))

	if len(args) == 0 {
		out, err := r.commGet("/api/device-aliases", nil)
		if err != nil {
			r.reply("device-alias failed", err.Error())
			return
		}
		r.reply("device-alias", prettyJSON(out))
		return
	}

	verb := strings.ToLower(args[0])
	switch verb {
	case "add":
		if len(args) < 3 {
			r.reply("device-alias add failed", "usage: device-alias add <alias> <server>")
			return
		}
		body, _ := json.Marshal(map[string]string{"alias": args[1], "server": args[2]})
		out, err := r.commJSON(http.MethodPost, "/api/device-aliases", string(body))
		if err != nil {
			r.reply("device-alias add failed", err.Error())
			return
		}
		r.reply("device-alias add", prettyJSON(out))
	case "delete", "remove":
		if len(args) < 2 {
			r.reply("device-alias delete failed", "usage: device-alias delete <alias>")
			return
		}
		out, err := r.commJSON(http.MethodDelete, "/api/device-aliases/"+args[1], "")
		if err != nil {
			r.reply("device-alias delete failed", err.Error())
			return
		}
		r.reply("device-alias delete", prettyJSON(out))
	default:
		r.reply("device-alias", "unknown verb "+verb+"\n"+help)
	}
}

// handleSplash — BL220-G24.
//
//	splash → GET /api/splash/info
func (r *Router) handleSplash(_ Command) {
	out, err := r.commGet("/api/splash/info", nil)
	if err != nil {
		r.reply("splash failed", err.Error())
		return
	}
	r.reply("splash", prettyJSON(out))
}

// handleDetection — BL220-G11.
//
//	detection → GET /api/diagnose (full health snapshot including eBPF)
func (r *Router) handleDetection(_ Command) {
	out, err := r.commGet("/api/diagnose", nil)
	if err != nil {
		r.reply("detection failed", err.Error())
		return
	}
	r.reply("detection", prettyJSON(out))
}

// handleObserver — BL220-G16.  Full observer surface beyond `peers`.
//
//	observer                           → stats summary
//	observer stats                     → GET /api/observer/stats
//	observer config                    → GET /api/observer/config
//	observer envelopes                 → GET /api/observer/envelopes
//	observer envelopes all-peers       → GET /api/observer/envelopes/all-peers
//	observer envelope <id>             → GET /api/observer/envelope/<id>
func (r *Router) handleObserver(cmd Command) {
	const help = "usage: observer [stats|config|envelopes [all-peers]|envelope <id>]"
	args := strings.Fields(strings.TrimSpace(cmd.Text))

	if len(args) == 0 {
		out, err := r.commGet("/api/observer/stats", nil)
		if err != nil {
			r.reply("observer failed", err.Error())
			return
		}
		r.reply("observer", prettyJSON(out))
		return
	}

	verb := strings.ToLower(args[0])
	switch verb {
	case "stats":
		out, err := r.commGet("/api/observer/stats", nil)
		if err != nil {
			r.reply("observer stats failed", err.Error())
			return
		}
		r.reply("observer stats", prettyJSON(out))
	case "config":
		out, err := r.commGet("/api/observer/config", nil)
		if err != nil {
			r.reply("observer config failed", err.Error())
			return
		}
		r.reply("observer config", prettyJSON(out))
	case "envelopes":
		path := "/api/observer/envelopes"
		label := "observer envelopes"
		if len(args) >= 2 && strings.ToLower(args[1]) == "all-peers" {
			path = "/api/observer/envelopes/all-peers"
			label = "observer envelopes all-peers"
		}
		out, err := r.commGet(path, nil)
		if err != nil {
			r.reply(label+" failed", err.Error())
			return
		}
		r.reply(label, prettyJSON(out))
	case "envelope":
		if len(args) < 2 {
			r.reply("observer envelope failed", "usage: observer envelope <id>")
			return
		}
		q := url.Values{"id": []string{args[1]}}
		out, err := r.commGet("/api/observer/envelope", q)
		if err != nil {
			r.reply("observer envelope failed", err.Error())
			return
		}
		r.reply("observer envelope", prettyJSON(out))
	default:
		r.reply("observer", "unknown verb "+verb+"\n"+help)
	}
}

// handleAnalytics — BL220-G14.
//
//	analytics          → GET /api/analytics (default range)
//	analytics <range>  → GET /api/analytics?range=<range>
func (r *Router) handleAnalytics(cmd Command) {
	q := url.Values{}
	if rng := strings.TrimSpace(cmd.Text); rng != "" {
		q.Set("range", rng)
	}
	out, err := r.commGet("/api/analytics", q)
	if err != nil {
		r.reply("analytics failed", err.Error())
		return
	}
	r.reply("analytics", prettyJSON(out))
}

// handleTooling — BL219.
//
//	tooling                      → status for all backends (default project dir)
//	tooling status [backend]     → GET /api/tooling/status
//	tooling gitignore <backend>  → POST /api/tooling/gitignore
//	tooling cleanup <backend>    → POST /api/tooling/cleanup
func (r *Router) handleTooling(cmd Command) {
	text := strings.TrimSpace(cmd.Text)
	lower := strings.ToLower(text)

	// Default: "tooling" with no subcommand → status for all backends.
	if text == "" || strings.HasPrefix(lower, "status") {
		backend := ""
		if strings.HasPrefix(lower, "status ") {
			backend = strings.TrimSpace(text[len("status "):])
		}
		q := url.Values{}
		if backend != "" {
			q.Set("backend", backend)
		}
		out, err := r.commGet("/api/tooling/status", q)
		if err != nil {
			r.reply("tooling failed", err.Error())
			return
		}
		r.reply("tooling status", prettyJSON(out))
		return
	}

	// tooling gitignore <backend>
	if strings.HasPrefix(lower, "gitignore ") {
		backend := strings.TrimSpace(text[len("gitignore "):])
		body := fmt.Sprintf(`{"backend":%q}`, backend)
		out, err := r.commJSON("POST", "/api/tooling/gitignore", body)
		if err != nil {
			r.reply("tooling gitignore failed", err.Error())
			return
		}
		r.reply("tooling gitignore", prettyJSON(out))
		return
	}

	// tooling cleanup <backend>
	if strings.HasPrefix(lower, "cleanup ") {
		backend := strings.TrimSpace(text[len("cleanup "):])
		body := fmt.Sprintf(`{"backend":%q}`, backend)
		out, err := r.commJSON("POST", "/api/tooling/cleanup", body)
		if err != nil {
			r.reply("tooling cleanup failed", err.Error())
			return
		}
		r.reply("tooling cleanup", prettyJSON(out))
		return
	}

	r.reply("tooling", "Usage: tooling [status [backend]] | gitignore <backend> | cleanup <backend>")
}
