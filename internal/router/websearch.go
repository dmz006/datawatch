// BL391 — comm-channel verbs for the multi-provider web search registry.
// Deliberately read-only + enable/disable/test — full provider add/update/
// delete is chat-unfriendly (same scope decision as council's "backends"
// list, council.go:36) and stays on REST/CLI/MCP/PWA.
//
//	websearch                   → usage stats summary
//	websearch providers         → list providers
//	websearch stats             → usage summary + daily series
//	websearch history           → recent search events
//	websearch enable <name>     → enable a provider
//	websearch disable <name>    → disable a provider
//	websearch test <name>       → one-shot connectivity test

package router

import "strings"

const webSearchUsage = `Usage:
  websearch                   usage stats summary
  websearch providers         list providers
  websearch stats             usage summary + daily series
  websearch history           recent search events
  websearch enable <name>     enable a provider
  websearch disable <name>    disable a provider
  websearch test <name>       one-shot connectivity test
Add/update/delete providers via REST, CLI ('datawatch websearch add'), MCP, or the PWA Settings page.`

func (r *Router) handleWebSearchCmd(cmd Command) {
	text := strings.TrimSpace(cmd.Text)
	lower := strings.ToLower(text)

	if text == "" || lower == "stats" {
		out, err := r.commGet("/api/websearch/stats", nil)
		if err != nil {
			r.reply("websearch", err.Error())
			return
		}
		r.reply("websearch", prettyJSON(out))
		return
	}
	if lower == "help" {
		r.reply("websearch", webSearchUsage)
		return
	}
	if lower == "providers" {
		out, err := r.commGet("/api/websearch/providers", nil)
		if err != nil {
			r.reply("websearch providers", err.Error())
			return
		}
		r.reply("websearch providers", prettyJSON(out))
		return
	}
	if lower == "history" {
		out, err := r.commGet("/api/websearch/history", nil)
		if err != nil {
			r.reply("websearch history", err.Error())
			return
		}
		r.reply("websearch history", prettyJSON(out))
		return
	}

	parts := strings.SplitN(text, " ", 2)
	verb := strings.ToLower(parts[0])
	tail := ""
	if len(parts) > 1 {
		tail = strings.TrimSpace(parts[1])
	}
	switch verb {
	case "enable", "disable":
		if tail == "" {
			r.reply("websearch "+verb, "Usage: websearch "+verb+" <name>")
			return
		}
		out, err := r.commJSON("POST", "/api/websearch/providers/"+tail+"/"+verb, "")
		if err != nil {
			r.reply("websearch "+verb, err.Error())
			return
		}
		r.reply("websearch "+verb+" "+tail, prettyJSON(out))
	case "test":
		if tail == "" {
			r.reply("websearch test", "Usage: websearch test <name>")
			return
		}
		out, err := r.commJSON("POST", "/api/websearch/providers/"+tail+"/test", "")
		if err != nil {
			r.reply("websearch test", err.Error())
			return
		}
		r.reply("websearch test "+tail, prettyJSON(out))
	default:
		r.reply("websearch", webSearchUsage)
	}
}
