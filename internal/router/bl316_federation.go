// BL316 S2 — comm-channel verbs for federation peer and group management.
//
//	federation peers                               → list peers
//	federation peer add <name> <url> [token=<t>]   → register peer
//	federation peer delete <name>                  → remove peer
//	federation peer test <name>                    → ping peer /api/health
//	federation groups                              → list builtin + custom groups

package router

import (
	"encoding/json"
	"net/http"
	"strings"
)

const federationUsage = `Usage:
  federation peers                                    list federation peers
  federation peer add <name> <url> [token=<t>]        register a peer
  federation peer delete <name>                       remove a peer
  federation peer test <name>                         ping peer /api/health
  federation groups                                   list groups (builtins + custom)`

func (r *Router) handleFederationCmd(cmd Command) {
	text := strings.TrimSpace(cmd.Text)
	lower := strings.ToLower(text)

	if lower == "help" {
		r.reply("federation", federationUsage)
		return
	}

	// "federation peers" or "federation peer list"
	if lower == "peers" || lower == "peer list" || lower == "peer" {
		out, err := r.commGet("/api/federation/peers", nil)
		if err != nil {
			r.reply("federation peers failed", err.Error())
			return
		}
		r.reply("federation peers", prettyJSON(out))
		return
	}

	// "federation groups"
	if lower == "groups" || lower == "group list" || lower == "group" {
		out, err := r.commGet("/api/federation/groups", nil)
		if err != nil {
			r.reply("federation groups failed", err.Error())
			return
		}
		r.reply("federation groups", prettyJSON(out))
		return
	}

	// "federation peer add <name> <url> [token=<t>]"
	if strings.HasPrefix(lower, "peer add ") {
		parts := strings.Fields(text[len("peer add "):])
		if len(parts) < 2 {
			r.reply("federation peer add", "Usage: federation peer add <name> <url> [token=<t>]")
			return
		}
		name, url := parts[0], parts[1]
		body := map[string]any{"name": name, "url": url, "enabled": true}
		for _, kv := range parts[2:] {
			if strings.HasPrefix(strings.ToLower(kv), "token=") {
				body["token"] = kv[len("token="):]
			}
		}
		data, _ := json.Marshal(body)
		out, err := r.commJSON(http.MethodPost, "/api/federation/peers", string(data))
		if err != nil {
			r.reply("federation peer add failed", err.Error())
			return
		}
		r.reply("federation peer add", prettyJSON(out))
		return
	}

	// "federation peer delete <name>"
	if strings.HasPrefix(lower, "peer delete ") || strings.HasPrefix(lower, "peer del ") {
		var name string
		if strings.HasPrefix(lower, "peer delete ") {
			name = strings.TrimSpace(text[len("peer delete "):])
		} else {
			name = strings.TrimSpace(text[len("peer del "):])
		}
		out, err := r.commJSON(http.MethodDelete, "/api/federation/peers/"+name, "")
		if err != nil {
			r.reply("federation peer delete failed", err.Error())
			return
		}
		r.reply("federation peer delete", prettyJSON(out))
		return
	}

	// "federation peer test <name>"
	if strings.HasPrefix(lower, "peer test ") {
		name := strings.TrimSpace(text[len("peer test "):])
		out, err := r.commJSON(http.MethodPost, "/api/federation/peers/"+name+"/test", "")
		if err != nil {
			r.reply("federation peer test failed", err.Error())
			return
		}
		r.reply("federation peer test: "+name, prettyJSON(out))
		return
	}

	r.reply("federation", federationUsage)
}
