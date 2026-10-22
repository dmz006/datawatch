// Comm-channel verbs for Automations: "automation activate <id>" starts a
// draft Automation; "automation list" and "automation get <id>" read them;
// pause, resume, cancel, skip, insert, remove, edges and approve are the
// operator controls on a running one, and set-llm and set-verification-llm
// set its LLMs. Sibling of cmd/datawatch/cli_automation.go
// and the automation_* MCP tools; activate calls
// POST /api/automations/{id}/activate, list and get call GET /api/automations
// and GET /api/automations/{id}, and each control calls its own
// POST /api/automations/{id}/{op} with the actor set to comm. Building or
// editing the whole Automation stays off comm (REST, MCP, CLI).

package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	automationUsage = "usage: automation activate <id> | list | get <id> | pause <id> [reason] | resume <id> [reason] | " +
		"cancel <id> in-flight=true|false | skip <id> <member-id> [edge] | insert <id> <prd-id> [urgent] <reason> | " +
		"remove <id> <member-id> | edges <id> <member-id> on_complete=<id> on_blocked=<id> on_failed=<id> | approve <id> <member-id> | " +
		"set-llm <id> [backend=<b>] [model=<m>] [effort=<e>] [planning-backend=<b>] [planning-model=<m>] | " +
		"set-verification-llm <id> [backend=<b>] [model=<m>] [effort=<e>]"
	automationCancelUsage = "usage: automation cancel <id> in-flight=true|false. The choice is required: " +
		"in-flight=true cancels members running now, in-flight=false lets them finish.\n" +
		"examples:\n  automation cancel ab12 in-flight=true\n  automation cancel ab12 in-flight=false"
	automationSkipUsage    = "usage: automation skip <id> <member-id> [on_complete|on_blocked|on_failed]"
	automationInsertUsage  = "usage: automation insert <id> <prd-id> [urgent] <reason...>. A reason is required."
	automationRemoveUsage  = "usage: automation remove <id> <member-id>"
	automationApproveUsage = "usage: automation approve <id> <member-id>"
	automationSetLLMUsage  = "usage: automation set-llm <id> [backend=<b>] [model=<m>] [effort=<e>] [planning-backend=<b>] [planning-model=<m>] " +
		"(name at least one; an empty value such as model= clears the field)"
	automationSetVerificationLLMUsage = "usage: automation set-verification-llm <id> [backend=<b>] [model=<m>] [effort=<e>] " +
		"(name at least one; an empty value such as model= clears the field)"
	automationEdgesUsage = "usage: automation edges <id> <member-id> on_complete=<id> on_blocked=<id> on_failed=<id> " +
		"(name at least one; an edge left out or given empty becomes no next member)"
)

func (r *Router) handleAutomationCmd(cmd Command) {
	args := strings.Fields(cmd.Text)
	if len(args) == 0 {
		r.send(automationUsage)
		return
	}
	switch args[0] {
	case "activate":
		if len(args) < 2 {
			r.send("usage: automation activate <id>")
			return
		}
		resp, err := r.commJSON(http.MethodPost,
			"/api/automations/"+url.PathEscape(args[1])+"/activate", `{"actor":"comm"}`)
		if err != nil {
			r.send("automation activate error: " + err.Error())
			return
		}
		r.send(resp)
	case "list":
		resp, err := r.commJSON(http.MethodGet, "/api/automations", "")
		if err != nil {
			r.send("automation list error: " + err.Error())
			return
		}
		r.send(resp)
	case "get":
		if len(args) < 2 {
			r.send("usage: automation get <id>")
			return
		}
		resp, err := r.commJSON(http.MethodGet, "/api/automations/"+url.PathEscape(args[1]), "")
		if err != nil {
			r.send("automation get error: " + err.Error())
			return
		}
		r.send(resp)
	case "pause":
		r.automationPauseResume(args, "pause", "paused")
	case "resume":
		r.automationPauseResume(args, "resume", "resumed")
	case "cancel":
		if len(args) != 3 {
			r.send(automationCancelUsage)
			return
		}
		key, choice, _ := strings.Cut(args[2], "=")
		if key != "in-flight" || (choice != "true" && choice != "false") {
			r.send(automationCancelUsage)
			return
		}
		r.automationControl("cancel", args[1], "cancel", "cancelled", map[string]any{"cancel_in_flight": choice == "true"})
	case "skip":
		if len(args) < 3 || len(args) > 4 {
			r.send(automationSkipUsage)
			return
		}
		body := map[string]any{"member_id": args[2]}
		if len(args) == 4 {
			if args[3] != "on_complete" && args[3] != "on_blocked" && args[3] != "on_failed" {
				r.send(automationSkipUsage)
				return
			}
			body["edge"] = args[3]
		}
		r.automationControl("skip", args[1], "skip_member", "member skipped", body)
	case "insert":
		if len(args) < 3 {
			r.send(automationInsertUsage)
			return
		}
		rest := args[3:]
		urgent := len(rest) > 0 && rest[0] == "urgent"
		if urgent {
			rest = rest[1:]
		}
		reason := strings.Join(rest, " ")
		if reason == "" {
			r.send(automationInsertUsage)
			return
		}
		r.automationControl("insert", args[1], "insert_member", "member inserted", map[string]any{
			"member": map[string]any{"prd_id": args[2]},
			"urgent": urgent,
			"reason": reason,
		})
	case "remove":
		if len(args) != 3 {
			r.send(automationRemoveUsage)
			return
		}
		r.automationControl("remove", args[1], "remove_member", "member removed", map[string]any{"member_id": args[2]})
	case "edges":
		if len(args) < 4 {
			r.send(automationEdgesUsage)
			return
		}
		body := map[string]any{"member_id": args[2], "on_complete": "", "on_blocked": "", "on_failed": ""}
		for _, tok := range args[3:] {
			k, v, ok := strings.Cut(tok, "=")
			if _, known := body[k]; !ok || !known || k == "member_id" {
				r.send(automationEdgesUsage)
				return
			}
			body[k] = v
		}
		r.automationControl("edges", args[1], "reorder_edges", "edges rewired", body)
	case "approve":
		if len(args) != 3 {
			r.send(automationApproveUsage)
			return
		}
		r.automationControl("approve", args[1], "approve_insert", "member approved", map[string]any{"member_id": args[2]})
	case "set-llm":
		r.automationSetLLM(args, "set-llm", "set_llm", automationSetLLMUsage,
			map[string]string{"backend": "backend", "model": "model", "effort": "effort",
				"planning-backend": "planning_backend", "planning-model": "planning_model"})
	case "set-verification-llm":
		r.automationSetLLM(args, "set-verification-llm", "set_verification_llm", automationSetVerificationLLMUsage,
			map[string]string{"backend": "backend", "model": "model", "effort": "effort"})
	default:
		r.send(fmt.Sprintf("automation: unknown subcommand %q. Try: activate, list, get, pause, resume, cancel, skip, insert, remove, edges, approve, set-llm, set-verification-llm", args[0]))
	}
}

// automationSetLLM serves "set-llm <id> key=value..." and
// "set-verification-llm <id> key=value...". keys maps each accepted token key
// to its REST body key; an unknown key, a token without '=' or no pair at all
// replies with the usage line. An empty value clears the field.
func (r *Router) automationSetLLM(args []string, verb, op, usage string, keys map[string]string) {
	if len(args) < 3 {
		r.send(usage)
		return
	}
	body := map[string]any{}
	for _, tok := range args[2:] {
		k, v, ok := strings.Cut(tok, "=")
		bodyKey, known := keys[k]
		if !ok || !known {
			r.send(usage)
			return
		}
		body[bodyKey] = v
	}
	r.automationControl(verb, args[1], op, "llm updated", body)
}

// automationPauseResume serves "pause <id> [reason...]" and "resume <id>
// [reason...]"; the words after the ID are the reason.
func (r *Router) automationPauseResume(args []string, verb, did string) {
	if len(args) < 2 {
		r.send("usage: automation " + verb + " <id> [reason text...]")
		return
	}
	body := map[string]any{}
	if len(args) > 2 {
		body["reason"] = strings.Join(args[2:], " ")
	}
	r.automationControl(verb, args[1], verb, did, body)
}

// automationControl posts one operator control to the REST API as the comm
// actor and replies with a one-line confirmation. A refusal, including a
// stale expected_version or a state conflict (HTTP 409), is replied with the
// server's own message.
func (r *Router) automationControl(verb, id, op, did string, body map[string]any) {
	body["actor"] = "comm"
	b, err := json.Marshal(body)
	if err != nil {
		r.send("automation " + verb + " error: " + err.Error())
		return
	}
	resp, err := r.commJSON(http.MethodPost, "/api/automations/"+url.PathEscape(id)+"/"+op, string(b))
	if err != nil {
		r.send("automation " + verb + " error: " + err.Error())
		return
	}
	var a struct {
		ID      string `json:"id"`
		Status  string `json:"status"`
		Version int64  `json:"version"`
	}
	if json.Unmarshal([]byte(resp), &a) != nil || a.ID == "" {
		r.send(resp)
		return
	}
	r.send(fmt.Sprintf("automation %s: %s (status %s, version %d)", a.ID, did, a.Status, a.Version))
}
