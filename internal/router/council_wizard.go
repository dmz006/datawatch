// BL297 (v6.22.3) — comm-channel verbs for the Council "Add Persona"
// wizard.
//
// Comm channels (Slack/Mattermost/etc.) are line-oriented + per-message.
// We don't carry per-operator state here, so the operator pastes the
// draft ID back on each turn:
//
//	council persona-wizard start <name>[|role]            → returns draft id
//	council persona-wizard answer <id> <field> <text>     → patch
//	council persona-wizard refine <id> [<instruction>]    → re-LLM
//	council persona-wizard save <id>                       → register
//	council persona-wizard cancel <id>                     → abandon
//	council persona-wizard list                            → list drafts
//	council persona-wizard delete <id>                     → drop one
//	council persona-wizard purge                           → drop all
//
// PWA + CLI surfaces present a richer flow; this is the line-oriented
// fallback per BL297 Q9 design.

package router

import (
	"encoding/json"
	"strings"
)

const councilWizardUsage = `Usage:
  council persona-wizard start <name>[|<role>]
  council persona-wizard answer <id> <field> <text>
    field = focus | stance | tone | anti_patterns | examples
  council persona-wizard refine <id> [<instruction>]
  council persona-wizard save <id>
  council persona-wizard cancel <id>
  council persona-wizard list
  council persona-wizard delete <id>
  council persona-wizard purge`

func (r *Router) handleCouncilPersonaWizard(cmd Command, rest string) {
	rest = strings.TrimSpace(rest)
	if rest == "" || strings.EqualFold(rest, "help") {
		r.reply("council persona-wizard", councilWizardUsage)
		return
	}
	parts := strings.SplitN(rest, " ", 2)
	verb := strings.ToLower(parts[0])
	tail := ""
	if len(parts) > 1 {
		tail = strings.TrimSpace(parts[1])
	}
	switch verb {
	case "start":
		name := tail
		role := ""
		if i := strings.Index(tail, "|"); i >= 0 {
			name = strings.TrimSpace(tail[:i])
			role = strings.TrimSpace(tail[i+1:])
		}
		if name == "" {
			r.reply("council persona-wizard start", "name required: start <name>[|<role>]")
			return
		}
		body, _ := json.Marshal(map[string]any{
			"name": name, "role": role,
			"channel_ref":  "comm",
			"operator_ref": "comm",
		})
		out, err := r.commJSON("POST", "/api/council/personas/draft", string(body))
		if err != nil {
			r.reply("council persona-wizard start", err.Error())
			return
		}
		r.reply("council persona-wizard start", prettyJSON(out)+
			"\n\nReply with: council persona-wizard answer <id> focus <text>")
	case "answer":
		// answer <id> <field> <text>
		bits := strings.SplitN(tail, " ", 3)
		if len(bits) < 3 {
			r.reply("council persona-wizard answer", "Usage: answer <id> <field> <text>")
			return
		}
		id, field, text := bits[0], strings.ToLower(bits[1]), bits[2]
		body, _ := json.Marshal(map[string]string{field: text})
		out, err := r.commJSON("PATCH", "/api/council/personas/draft/"+id, string(body))
		if err != nil {
			r.reply("council persona-wizard answer", err.Error())
			return
		}
		r.reply("council persona-wizard answer", prettyJSON(out))
	case "refine":
		bits := strings.SplitN(tail, " ", 2)
		if len(bits) < 1 || bits[0] == "" {
			r.reply("council persona-wizard refine", "Usage: refine <id> [<instruction>]")
			return
		}
		id := bits[0]
		instr := ""
		if len(bits) > 1 {
			instr = bits[1]
		}
		body, _ := json.Marshal(map[string]string{"instruction": instr})
		out, err := r.commJSON("POST", "/api/council/personas/draft/"+id+"/refine", string(body))
		if err != nil {
			r.reply("council persona-wizard refine", err.Error())
			return
		}
		r.reply("council persona-wizard refine", prettyJSON(out))
	case "save":
		if tail == "" {
			r.reply("council persona-wizard save", "Usage: save <id>")
			return
		}
		out, err := r.commJSON("POST", "/api/council/personas/draft/"+tail+"/save", "{}")
		if err != nil {
			r.reply("council persona-wizard save", err.Error())
			return
		}
		r.reply("council persona-wizard save", prettyJSON(out))
	case "cancel":
		if tail == "" {
			r.reply("council persona-wizard cancel", "Usage: cancel <id>")
			return
		}
		out, err := r.commJSON("POST", "/api/council/personas/draft/"+tail+"/abandon", "{}")
		if err != nil {
			r.reply("council persona-wizard cancel", err.Error())
			return
		}
		r.reply("council persona-wizard cancel", prettyJSON(out))
	case "list":
		out, err := r.commGet("/api/council/personas/drafts", nil)
		if err != nil {
			r.reply("council persona-wizard list", err.Error())
			return
		}
		r.reply("council persona-wizard list", prettyJSON(out))
	case "delete":
		if tail == "" {
			r.reply("council persona-wizard delete", "Usage: delete <id>")
			return
		}
		out, err := r.commJSON("DELETE", "/api/council/personas/drafts/"+tail, "")
		if err != nil {
			r.reply("council persona-wizard delete", err.Error())
			return
		}
		r.reply("council persona-wizard delete", prettyJSON(out))
	case "purge":
		out, err := r.commJSON("DELETE", "/api/council/personas/drafts", "")
		if err != nil {
			r.reply("council persona-wizard purge", err.Error())
			return
		}
		r.reply("council persona-wizard purge", prettyJSON(out))
	default:
		r.reply("council persona-wizard", councilWizardUsage)
	}
	_ = cmd
}
