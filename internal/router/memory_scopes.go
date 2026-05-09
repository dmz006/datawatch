// v7.0.0 alpha.5.x — comm verbs for the scope-hierarchy memory model.
//
//	memory scope                                       → usage
//	memory scope recall persona=X project=/p session=S → walks all layers
//	memory scope borrow scope=project-shared project=/p
//	memory scope seed from-scope=project-shared from-project=/a to-scope=session-local to-project=/b to-session=sess
//	memory scope promote memory-id=42 from-scope=session-local from-project=/p from-session=s to-scope=project-shared to-project=/p

package router

import (
	"encoding/json"
	"strconv"
	"strings"
)

const memoryScopeUsage = `Usage:
  memory scope                                       this help
  memory scope recall [k=v ...]                      walk every layer top-down
  memory scope borrow scope=<s> [k=v ...]            read-only cross-scope query
  memory scope seed from-scope=<s> to-scope=<s> [k=v ...]
  memory scope promote memory-id=<n> from-scope=<s> to-scope=<s> [k=v ...]

scopes: persona-global | persona-in-project | project-shared | session-local
common kv: persona=<x> project=<dir> session=<id> top_k=<n>
seed/promote: from-* + to-* prefix the kv pairs (e.g. from-persona, to-session)
seed extras: role-prefix=<x> content-substring=<x> limit=<n>
promote extras: promoted-by=operator|<persona> persona=<x> run=<x>`

func (r *Router) handleMemoryCmd(cmd Command) {
	text := strings.TrimSpace(cmd.Text)
	if text == "" || strings.EqualFold(text, "help") {
		r.reply("memory", memoryScopeUsage)
		return
	}
	parts := strings.SplitN(text, " ", 2)
	verb := strings.ToLower(parts[0])
	if verb != "scope" {
		r.reply("memory", memoryScopeUsage)
		return
	}
	rest := ""
	if len(parts) > 1 {
		rest = strings.TrimSpace(parts[1])
	}
	bits := strings.SplitN(rest, " ", 2)
	subVerb := strings.ToLower(bits[0])
	tail := ""
	if len(bits) > 1 {
		tail = strings.TrimSpace(bits[1])
	}
	kv := parseMemoryKV(tail)
	switch subVerb {
	case "", "help":
		r.reply("memory scope", memoryScopeUsage)
	case "recall":
		q := buildMemRecallQuery(kv)
		out, err := r.commGet("/api/memory/scopes/recall"+q, nil)
		if err != nil {
			r.reply("memory scope recall", err.Error())
			return
		}
		r.reply("memory scope recall", prettyJSON(out))
	case "borrow":
		if kv["scope"] == "" {
			r.reply("memory scope borrow", "Usage: memory scope borrow scope=<s> [k=v ...]")
			return
		}
		q := "?scope=" + kv["scope"] +
			"&persona=" + kv["persona"] +
			"&project=" + kv["project"] +
			"&session=" + kv["session"] +
			"&top_k=" + kv["top_k"]
		out, err := r.commGet("/api/memory/scopes/borrow"+q, nil)
		if err != nil {
			r.reply("memory scope borrow", err.Error())
			return
		}
		r.reply("memory scope borrow", prettyJSON(out))
	case "seed":
		if kv["from-scope"] == "" || kv["to-scope"] == "" {
			r.reply("memory scope seed", "Usage: memory scope seed from-scope=<s> to-scope=<s> [k=v ...]")
			return
		}
		body := map[string]any{
			"from": map[string]any{
				"scope":      kv["from-scope"],
				"persona":    kv["from-persona"],
				"project":    kv["from-project"],
				"session_id": kv["from-session"],
			},
			"to": map[string]any{
				"scope":      kv["to-scope"],
				"persona":    kv["to-persona"],
				"project":    kv["to-project"],
				"session_id": kv["to-session"],
			},
			"filter": map[string]any{
				"role_prefix":       kv["role-prefix"],
				"content_substring": kv["content-substring"],
			},
			"limit": atoiSafe(kv["limit"], 100),
		}
		bodyJSON, _ := json.Marshal(body)
		out, err := r.commJSON("POST", "/api/memory/scopes/seed", string(bodyJSON))
		if err != nil {
			r.reply("memory scope seed", err.Error())
			return
		}
		r.reply("memory scope seed", prettyJSON(out))
	case "promote":
		if kv["memory-id"] == "" || kv["from-scope"] == "" || kv["to-scope"] == "" {
			r.reply("memory scope promote", "Usage: memory scope promote memory-id=<n> from-scope=<s> to-scope=<s> [k=v ...]")
			return
		}
		memID, _ := strconv.ParseInt(kv["memory-id"], 10, 64)
		body := map[string]any{
			"memory_id": memID,
			"from": map[string]any{
				"scope":      kv["from-scope"],
				"persona":    kv["from-persona"],
				"project":    kv["from-project"],
				"session_id": kv["from-session"],
			},
			"to": map[string]any{
				"scope":      kv["to-scope"],
				"persona":    kv["to-persona"],
				"project":    kv["to-project"],
				"session_id": kv["to-session"],
			},
			"breadcrumb": map[string]any{
				"persona":     kv["persona"],
				"run":         kv["run"],
				"promoted_by": kv["promoted-by"],
			},
		}
		bodyJSON, _ := json.Marshal(body)
		out, err := r.commJSON("POST", "/api/memory/scopes/promote", string(bodyJSON))
		if err != nil {
			r.reply("memory scope promote", err.Error())
			return
		}
		r.reply("memory scope promote", prettyJSON(out))
	default:
		r.reply("memory scope", memoryScopeUsage)
	}
}

func parseMemoryKV(s string) map[string]string {
	out := map[string]string{}
	for _, kv := range strings.Fields(s) {
		eq := strings.IndexRune(kv, '=')
		if eq < 0 {
			continue
		}
		out[strings.ToLower(kv[:eq])] = kv[eq+1:]
	}
	return out
}

func buildMemRecallQuery(kv map[string]string) string {
	q := "?"
	for k, v := range kv {
		if v == "" {
			continue
		}
		q += k + "=" + v + "&"
	}
	return strings.TrimSuffix(q, "&")
}

func atoiSafe(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}
