// BL257 Phase 1 v6.8.0 — comm-channel verbs for the operator identity
// / Telos layer.
//
//	identity                  → show full identity
//	identity show             → alias for above
//	identity get [field]      → print everything (or one field)
//	identity set <f> <value>  → PATCH one field (comma-separated for lists)
//	identity backups          → list the saved copies, newest first
//	identity restore <name>   → bring a saved copy back ("latest" for the newest)
//	identity reset            → points at the CLI, PWA or MCP tool; never clears from chat

package router

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const identityUsage = `Usage:
  identity                            show full identity
  identity show                       alias for above
  identity get [field]                show one field (role|north_star_goals|current_projects|values|current_focus|context_notes)
  identity set <field> <value>        patch one field (comma-separated for list fields)
  identity configure                  open the PWA Identity Wizard or run "datawatch identity configure" locally
  identity backups                    list saved copies of the identity, newest first
  identity restore <name|latest>      bring a saved copy back
  identity reset                      not available here; clear it with "datawatch identity reset", the PWA or the MCP reset_identity tool`

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

	if lower == "backups" {
		r.identityBackups()
		return
	}

	if lower == "restore" || strings.HasPrefix(lower, "restore ") {
		r.identityRestore(strings.TrimSpace(text[len("restore"):]))
		return
	}

	if lower == "reset" {
		// Clearing the identity is deliberately not a chat verb: one
		// mistyped line must not empty it. A backup is kept, so
		// "identity restore latest" undoes a reset made elsewhere.
		r.reply("identity reset",
			"Clearing the identity is not done from chat. Use `datawatch identity reset` on the CLI, the PWA, or the MCP reset_identity tool.\n"+
				"A backup is kept, so `identity restore latest` undoes it.")
		return
	}

	if lower == "configure" {
		// BL257 P2 v6.8.1 — comm channels are stateless / line-oriented;
		// a multi-step interview doesn't fit. Direct the operator at the
		// PWA Identity Wizard (robot icon in header) or the local CLI.
		r.reply("identity configure",
			"Run the wizard via PWA (robot icon in header) or `datawatch identity configure` locally.\n"+
				"To set fields one at a time over comm: `identity set <field> <value>`.\n"+
				identityUsage)
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

// identityBackupList is the GET /api/identity/backups answer.
type identityBackupList struct {
	Backups []struct {
		Name       string    `json:"name"`
		CreatedAt  time.Time `json:"created_at"`
		Role       string    `json:"role"`
		FieldCount int       `json:"field_count"`
	} `json:"backups"`
}

// identityBackups replies with one short line per saved copy, newest first
// as the daemon lists them.
func (r *Router) identityBackups() {
	out, err := r.commGet("/api/identity/backups", nil)
	if err != nil {
		r.reply("identity backups", err.Error())
		return
	}
	var list identityBackupList
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		r.reply("identity backups", err.Error())
		return
	}
	if len(list.Backups) == 0 {
		r.reply("identity backups", "no backups yet")
		return
	}
	lines := make([]string, 0, len(list.Backups))
	for _, b := range list.Backups {
		role := b.Role
		if role == "" {
			role = "(no role)"
		}
		lines = append(lines, fmt.Sprintf("%s  %s  %s  %d fields",
			b.Name, b.CreatedAt.Local().Format("2006-01-02 15:04:05"), role, b.FieldCount))
	}
	r.reply("identity backups", strings.Join(lines, "\n"))
}

// identityRestore asks the daemon to bring a saved copy back and replies
// with the restored identity, or with the daemon's own error text.
func (r *Router) identityRestore(name string) {
	if name == "" {
		r.reply("identity restore", identityUsage)
		return
	}
	body, _ := json.Marshal(map[string]string{"backup": name})
	out, err := r.commJSON("POST", "/api/identity/restore", string(body))
	if err != nil {
		r.reply("identity restore", err.Error())
		return
	}
	r.reply("identity restore "+name, prettyJSON(out))
}
