// BL257 Phase 1 v6.8.0 — comm-channel verbs for the operator identity
// / Telos layer.
//
//	identity                  → show full identity
//	identity show             → alias for above
//	identity get [field]      → print everything (or one field)
//	identity set <f> <value>  → PATCH one field (comma-separated for lists)

package router

import (
	"encoding/json"
	"strings"
)

const identityUsage = `Usage:
  identity                            show full identity
  identity show                       alias for above
  identity get [field]                show one field (role|north_star_goals|current_projects|values|current_focus|context_notes)
  identity set <field> <value>        patch one field (comma-separated for list fields)`

func (r *Router) handleIdentityCmd(cmd Command) {
	text := strings.TrimSpace(cmd.Text)
	lower := strings.ToLower(text)

	if text == "" || lower == "show" {
		out, err := r.commGet("/api/identity", nil)
		if err != nil {
			r.reply("identity", err.Error())
			return
		}
		r.reply("identity", prettyJSON(out))
		return
	}

	if strings.HasPrefix(lower, "get") {
		field := strings.TrimSpace(text[len("get"):])
		out, err := r.commGet("/api/identity", nil)
		if err != nil {
			r.reply("identity", err.Error())
			return
		}
		if field == "" {
			r.reply("identity", prettyJSON(out))
			return
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(out), &m); err != nil {
			r.reply("identity", err.Error())
			return
		}
		v := m[field]
		if v == nil {
			r.reply("identity "+field, "not set")
			return
		}
		b, _ := json.MarshalIndent(v, "", "  ")
		r.reply("identity "+field, string(b))
		return
	}

	if strings.HasPrefix(lower, "set ") {
		rest := strings.TrimSpace(text[len("set "):])
		// Tokenize "<field> <value...>"
		idx := strings.IndexAny(rest, " \t")
		if idx < 0 {
			r.reply("identity set", identityUsage)
			return
		}
		field := strings.TrimSpace(rest[:idx])
		value := strings.TrimSpace(rest[idx+1:])
		patch := identityFieldPatch(field, value)
		if patch == nil {
			r.reply("identity set", "unknown field "+field)
			return
		}
		body, _ := json.Marshal(patch)
		out, err := r.commJSON("PATCH", "/api/identity", string(body))
		if err != nil {
			r.reply("identity set", err.Error())
			return
		}
		r.reply("identity set "+field, prettyJSON(out))
		return
	}

	r.reply("identity", identityUsage)
}

// identityFieldPatch builds the PATCH body for one field. Returns nil
// if the field is unknown.
func identityFieldPatch(field, value string) map[string]any {
	field = strings.ToLower(strings.TrimSpace(field))
	listOf := func(s string) []string {
		parts := strings.Split(s, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	switch field {
	case "role":
		return map[string]any{"role": value}
	case "north_star_goals", "goals":
		return map[string]any{"north_star_goals": listOf(value)}
	case "current_projects", "projects":
		return map[string]any{"current_projects": listOf(value)}
	case "values":
		return map[string]any{"values": listOf(value)}
	case "current_focus", "focus":
		return map[string]any{"current_focus": value}
	case "context_notes", "notes":
		return map[string]any{"context_notes": value}
	}
	return nil
}
