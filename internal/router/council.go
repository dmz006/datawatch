// BL260 v6.11.0 — comm-channel verbs for Council Mode.
//
//	council                          → list personas
//	council personas                 → list personas (alias)
//	council run <mode> <proposal>    → run (mode = debate|quick)
//	council runs                     → list past runs
//	council get-run <id>             → fetch one run

package router

import (
	"encoding/json"
	"strings"
)

const councilUsage = `Usage:
  council                                list personas
  council personas                       list personas
  council personas get <name>            show one persona's system prompt
  council personas set <name> <prompt>   update a persona's system prompt
  council run <mode> <proposal>          execute (mode = debate|quick)
  council runs                           list past runs
  council get-run <id>                   fetch one run
  council persona-wizard                 (BL297) interactive persona drafter
    persona-wizard start <name> [|<role>]   begin chat-style interview
    persona-wizard answer <text>            answer current question
    persona-wizard refine <instr>           tune the drafted persona
    persona-wizard save                     register the persona
    persona-wizard cancel                   abandon current draft
    persona-wizard list                     list drafts
    persona-wizard purge                    delete ALL drafts
  council config                         (BL297 v6.22.4) runtime config
    config                                  read draft_retention_days etc.
    config set draft-retention-days <N>     update + persist (live)
    config set llm-ref <name>               LLM registry entry for debates
    config set max-parallel <N>             per-round persona concurrency
    config set comm-firehose <true|false>   push persona responses to comm`

func (r *Router) handleCouncilCmd(cmd Command) {
	text := strings.TrimSpace(cmd.Text)
	lower := strings.ToLower(text)

	if text == "" || lower == "personas" {
		out, err := r.commGet("/api/council/personas", nil)
		if err != nil {
			r.reply("council", err.Error())
			return
		}
		r.reply("council", prettyJSON(out))
		return
	}

	// BL296 — "personas get <name>" / "personas set <name> <prompt>"
	if strings.HasPrefix(lower, "personas ") {
		rest := strings.TrimSpace(text[len("personas "):])
		pParts := strings.SplitN(rest, " ", 3)
		pVerb := strings.ToLower(pParts[0])
		switch pVerb {
		case "get":
			if len(pParts) < 2 {
				r.reply("council personas get", "Usage: council personas get <name>")
				return
			}
			out, err := r.commGet("/api/council/personas/"+pParts[1], nil)
			if err != nil {
				r.reply("council personas get", err.Error())
				return
			}
			r.reply("council personas get", prettyJSON(out))
			return
		case "set":
			if len(pParts) < 3 {
				r.reply("council personas set", "Usage: council personas set <name> <system_prompt>")
				return
			}
			name := pParts[1]
			prompt := pParts[2]
			body, _ := json.Marshal(map[string]any{"system_prompt": prompt})
			out, err := r.commJSON("PUT", "/api/council/personas/"+name, string(body))
			if err != nil {
				r.reply("council personas set", err.Error())
				return
			}
			r.reply("council personas set", prettyJSON(out))
			return
		default:
			// fall through to list
			out, err := r.commGet("/api/council/personas", nil)
			if err != nil {
				r.reply("council personas", err.Error())
				return
			}
			r.reply("council personas", prettyJSON(out))
			return
		}
	}

	parts := strings.SplitN(text, " ", 2)
	verb := strings.ToLower(parts[0])

	switch verb {
	case "run":
		// "council run <mode> <proposal...>"
		rest := ""
		if len(parts) > 1 {
			rest = parts[1]
		}
		modeAndProp := strings.SplitN(rest, " ", 2)
		mode := "quick"
		proposal := rest
		if len(modeAndProp) == 2 && (strings.EqualFold(modeAndProp[0], "debate") || strings.EqualFold(modeAndProp[0], "quick")) {
			mode = strings.ToLower(modeAndProp[0])
			proposal = modeAndProp[1]
		}
		if proposal == "" {
			r.reply("council run", councilUsage)
			return
		}
		body, _ := json.Marshal(map[string]any{"proposal": proposal, "mode": mode})
		out, err := r.commJSON("POST", "/api/council/run", string(body))
		if err != nil {
			r.reply("council run", err.Error())
			return
		}
		r.reply("council run", prettyJSON(out))
	case "runs":
		out, err := r.commGet("/api/council/runs", nil)
		if err != nil {
			r.reply("council runs", err.Error())
			return
		}
		r.reply("council runs", prettyJSON(out))
	case "config":
		// BL297 v6.22.4 — comm verb for Council runtime config.
		rest := ""
		if len(parts) > 1 {
			rest = strings.TrimSpace(parts[1])
		}
		if rest == "" {
			out, err := r.commGet("/api/council/config", nil)
			if err != nil {
				r.reply("council config", err.Error())
				return
			}
			r.reply("council config", prettyJSON(out))
			return
		}
		// "set <key> <val>" — currently only draft-retention-days
		bits := strings.Fields(rest)
		if len(bits) >= 3 && strings.EqualFold(bits[0], "set") {
			key := strings.ToLower(bits[1])
			val := bits[2]
			body, _ := json.Marshal(map[string]string{
				strings.ReplaceAll(key, "-", "_"): val,
			})
			out, err := r.commJSON("PATCH", "/api/council/config", string(body))
			if err != nil {
				r.reply("council config set", err.Error())
				return
			}
			r.reply("council config set", prettyJSON(out))
			return
		}
		r.reply("council config", "Usage: council config | council config set draft-retention-days <N>")
		return
	case "persona-wizard":
		// BL297 v6.22.3 — chat-style turn-based persona drafter for
		// bidirectional comm channels. State is keyed on
		// (operator_ref, channel_ref) — see drafts.FindActive.
		rest := ""
		if len(parts) > 1 {
			rest = parts[1]
		}
		r.handleCouncilPersonaWizard(cmd, rest)
		return
	case "cancel":
		// v7.0.0 S3 — comm-channel cancel verb.
		if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
			r.reply("council cancel", "Usage: council cancel <run-id>")
			return
		}
		id := strings.TrimSpace(parts[1])
		out, err := r.commJSON("POST", "/api/council/runs/"+id+"/cancel", "{}")
		if err != nil {
			r.reply("council cancel", err.Error())
			return
		}
		r.reply("council cancel "+id, prettyJSON(out))
		return
	case "get-run":
		if len(parts) < 2 {
			r.reply("council get-run", "Usage: council get-run <id>")
			return
		}
		out, err := r.commGet("/api/council/runs/"+strings.TrimSpace(parts[1]), nil)
		if err != nil {
			r.reply("council get-run", err.Error())
			return
		}
		r.reply("council "+parts[1], prettyJSON(out))
	default:
		r.reply("council", councilUsage)
	}
}
