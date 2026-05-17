// BL312 S1 — comm-channel verbs for the multi-server registry.
//
//	server                    → list servers
//	server list               → list servers
//	server add <name> <url> [token=<t>]
//	server delete <name>
//	server test <name>

package router

import (
	"encoding/json"
	"net/http"
	"strings"
)

const serverUsage = `Usage:
  server                                          list servers
  server list                                     list servers
  server add <name> <url> [token=<t>]             add runtime entry
  server delete <name>                            remove runtime entry
  server test <name>                              ping server /api/health`

func (r *Router) handleServerCmd(cmd Command) {
	text := strings.TrimSpace(cmd.Text)
	lower := strings.ToLower(text)

	if text == "" || lower == "list" {
		out, err := r.commGet("/api/servers", nil)
		if err != nil {
			r.reply("server list failed", err.Error())
			return
		}
		r.reply("servers", prettyJSON(out))
		return
	}

	if lower == "help" {
		r.reply("server", serverUsage)
		return
	}

	if strings.HasPrefix(lower, "list") {
		out, err := r.commGet("/api/servers", nil)
		if err != nil {
			r.reply("server list failed", err.Error())
			return
		}
		r.reply("servers", prettyJSON(out))
		return
	}

	if strings.HasPrefix(lower, "add ") {
		parts := strings.Fields(text[4:])
		if len(parts) < 2 {
			r.reply("server add", "Usage: server add <name> <url> [token=<t>]")
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
		out, err := r.commJSON(http.MethodPost, "/api/servers", string(data))
		if err != nil {
			r.reply("server add failed", err.Error())
			return
		}
		r.reply("server add", prettyJSON(out))
		return
	}

	if strings.HasPrefix(lower, "delete ") || strings.HasPrefix(lower, "del ") {
		var name string
		if strings.HasPrefix(lower, "delete ") {
			name = strings.TrimSpace(text[7:])
		} else {
			name = strings.TrimSpace(text[4:])
		}
		out, err := r.commJSON(http.MethodDelete, "/api/servers/"+name, "")
		if err != nil {
			r.reply("server delete failed", err.Error())
			return
		}
		r.reply("server delete", prettyJSON(out))
		return
	}

	if strings.HasPrefix(lower, "test ") {
		name := strings.TrimSpace(text[5:])
		out, err := r.commJSON(http.MethodPost, "/api/servers/"+name+"/test", "")
		if err != nil {
			r.reply("server test failed", err.Error())
			return
		}
		r.reply("server test: "+name, prettyJSON(out))
		return
	}

	r.reply("server", serverUsage)
}
