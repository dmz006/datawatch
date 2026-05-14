// BL303 S2 — comm-channel verbs for the guardrail library + profiles.
//
// Supported verbs (all proxied through the in-process REST loopback):
//
//	"guardrail library"                                → list library
//	"guardrail profile list"                           → list profiles
//	"guardrail profile create <name> [guardrail,...]"  → create profile
//	"guardrail profile get <id>"                       → get profile
//	"guardrail profile delete <id>"                    → delete profile
//	"guardrail automaton set <id> [...]"               → per-Automaton override

package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func (r *Router) handleGuardrailCmd(cmd Command) {
	args := strings.Fields(cmd.Text)
	if len(args) == 0 || args[0] == "library" {
		r.guardrailLibrary()
		return
	}
	switch args[0] {
	case "profile":
		r.guardrailProfile(args[1:])
	case "automaton":
		r.guardrailAutomaton(args[1:])
	default:
		r.send(fmt.Sprintf("guardrail: unknown subcommand %q. Try: library | profile | automaton", args[0]))
	}
}

func (r *Router) guardrailLibrary() {
	body, err := r.commGet("/api/autonomous/guardrails", nil)
	if err != nil {
		r.send("guardrail library error: " + err.Error())
		return
	}
	var entries []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Type        string `json:"type"`
	}
	if err := json.Unmarshal([]byte(body), &entries); err != nil {
		r.send(body)
		return
	}
	if len(entries) == 0 {
		r.send("Guardrail library: (empty)")
		return
	}
	var sb strings.Builder
	sb.WriteString("Guardrail library:\n")
	for _, e := range entries {
		sb.WriteString(fmt.Sprintf("  %-22s [%s] %s\n", e.Name, e.Type, e.Description))
	}
	r.send(strings.TrimRight(sb.String(), "\n"))
}

func (r *Router) guardrailProfile(args []string) {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "list":
		body, err := r.commGet("/api/autonomous/guardrail_profiles", nil)
		if err != nil {
			r.send("guardrail profile list error: " + err.Error())
			return
		}
		r.send(body)

	case "get":
		if len(args) < 2 {
			r.send("usage: guardrail profile get <id>")
			return
		}
		body, err := r.commGet("/api/autonomous/guardrail_profiles/"+args[1], nil)
		if err != nil {
			r.send("guardrail profile get error: " + err.Error())
			return
		}
		r.send(body)

	case "create":
		if len(args) < 2 {
			r.send("usage: guardrail profile create <name> [guardrail1,guardrail2,...]")
			return
		}
		name := args[1]
		var guardrails []string
		if len(args) > 2 {
			for _, g := range strings.Split(args[2], ",") {
				if g = strings.TrimSpace(g); g != "" {
					guardrails = append(guardrails, g)
				}
			}
		}
		payload, _ := json.Marshal(map[string]any{"name": name, "guardrails": guardrails})
		body, err := r.commJSON(http.MethodPost, "/api/autonomous/guardrail_profiles", string(payload))
		if err != nil {
			r.send("guardrail profile create error: " + err.Error())
			return
		}
		r.send(body)

	case "delete":
		if len(args) < 2 {
			r.send("usage: guardrail profile delete <id>")
			return
		}
		_, err := r.commJSON(http.MethodDelete, "/api/autonomous/guardrail_profiles/"+args[1], "")
		if err != nil {
			r.send("guardrail profile delete error: " + err.Error())
			return
		}
		r.send("deleted")

	default:
		r.send(fmt.Sprintf("guardrail profile: unknown subcommand %q. Try: list | get | create | delete", sub))
	}
}

func (r *Router) guardrailAutomaton(args []string) {
	if len(args) < 2 || args[0] != "set" {
		r.send("usage: guardrail automaton set <id> [profile=<name>] [per-task=g1,g2] [per-story=g1,g2]")
		return
	}
	id := args[1]
	body := map[string]any{}
	for _, flag := range args[2:] {
		if strings.HasPrefix(flag, "profile=") {
			body["guardrail_profile"] = strings.TrimPrefix(flag, "profile=")
		} else if strings.HasPrefix(flag, "per-task=") {
			body["per_task_guardrails"] = splitCSV(strings.TrimPrefix(flag, "per-task="))
		} else if strings.HasPrefix(flag, "per-story=") {
			body["per_story_guardrails"] = splitCSV(strings.TrimPrefix(flag, "per-story="))
		}
	}
	payload, _ := json.Marshal(body)
	resp, err := r.commJSON(http.MethodPut, "/api/autonomous/prds/"+id+"/guardrails", string(payload))
	if err != nil {
		r.send("guardrail automaton set error: " + err.Error())
		return
	}
	r.send(resp)
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
