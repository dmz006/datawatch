// Comm-channel verbs for Automations: "automation activate <id>" starts a
// draft Automation; "automation list" and "automation get <id>" read them;
// pause, resume, cancel, skip, insert, remove, edges and approve are the
// operator controls on a running one. Sibling of cmd/datawatch/cli_automation.go
// and the automation_* MCP tools; activate calls
// POST /api/automations/{id}/activate, list and get call GET /api/automations
// and GET /api/automations/{id}, and each control calls its own
// POST /api/automations/{id}/{op} with the actor set to comm. "status <id>"
// reads GET /api/automations/{id} and prints a summary with the budget block,
// and "set-budget <id> key=value..." sets, replaces or clears a budget policy
// through POST /api/automations/{id}/set_budget. Building or editing the whole
// Automation stays off comm (REST, MCP, CLI).

package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const (
	automationUsage = "usage: automation activate <id> | list | get <id> | status <id> | pause <id> [reason] | resume <id> [reason] | " +
		"cancel <id> in-flight=true|false | skip <id> <member-id> [edge] | insert <id> <prd-id> [urgent] <reason> | " +
		"remove <id> <member-id> | edges <id> <member-id> on_complete=<id> on_blocked=<id> on_failed=<id> | approve <id> <member-id> | " +
		"set-budget <id> key=value... (ceiling_type, ceiling_value, window, pacing, on_breach, ...; clear=true removes the policy)"
	automationCancelUsage = "usage: automation cancel <id> in-flight=true|false. The choice is required: " +
		"in-flight=true cancels members running now, in-flight=false lets them finish.\n" +
		"examples:\n  automation cancel ab12 in-flight=true\n  automation cancel ab12 in-flight=false"
	automationSkipUsage      = "usage: automation skip <id> <member-id> [on_complete|on_blocked|on_failed]"
	automationInsertUsage    = "usage: automation insert <id> <prd-id> [urgent] <reason...>. A reason is required."
	automationRemoveUsage    = "usage: automation remove <id> <member-id>"
	automationApproveUsage   = "usage: automation approve <id> <member-id>"
	automationStatusUsage    = "usage: automation status <id>"
	automationSetBudgetUsage = "usage: automation set-budget <id> ceiling_type=dollar|allocation_pct ceiling_value=<n> window=automation|day|week|month " +
		"[pacing=none|even] [on_breach=pause|block] [backend=<llm>] [allocation_estimate=true] [peer_window=<a,b>] " +
		"[reset_time=HH:MM] [reset_weekday=<day>] [reset_day=1..28] [reset_zone=<zone>] [member=<id>] [estimated_cost_usd=<n>] " +
		"[expected_version=<n>] | automation set-budget <id> clear=true [member=<id>] [expected_version=<n>]"
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
	case "status":
		if len(args) != 2 {
			r.send(automationStatusUsage)
			return
		}
		r.automationStatus(args[1])
	case "set-budget":
		r.automationSetBudget(args)
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
	default:
		r.send(fmt.Sprintf("automation: unknown subcommand %q. Try: activate, list, get, status, pause, resume, cancel, skip, insert, remove, edges, approve, set-budget", args[0]))
	}
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

// automationBudgetReply is the part of an Automation REST record that the
// status and set-budget replies print.
type automationBudgetReply struct {
	ID      string                 `json:"id"`
	Name    string                 `json:"name"`
	Status  string                 `json:"status"`
	Version int64                  `json:"version"`
	Members []json.RawMessage      `json:"members"`
	Budget  *autonomous.BudgetView `json:"budget"`
}

// automationSummary is the first line of a status or set-budget reply and,
// when the Automation has a budget view, the budget block under it.
func automationSummary(a automationBudgetReply) string {
	var b strings.Builder
	fmt.Fprintf(&b, "automation %s", a.ID)
	if a.Name != "" {
		fmt.Fprintf(&b, " (%s)", a.Name)
	}
	fmt.Fprintf(&b, ": status %s, version %d, %d members\n", a.Status, a.Version, len(a.Members))
	if a.Budget != nil {
		b.WriteString(autonomous.FormatAutomationBudgetText(*a.Budget))
	} else {
		b.WriteString("budget: none set\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// automationStatus serves "status <id>": the Automation's state and its
// budget block, from GET /api/automations/{id}.
func (r *Router) automationStatus(id string) {
	resp, err := r.commJSON(http.MethodGet, "/api/automations/"+url.PathEscape(id), "")
	if err != nil {
		r.send("automation status error: " + err.Error())
		return
	}
	var a automationBudgetReply
	if json.Unmarshal([]byte(resp), &a) != nil || a.ID == "" {
		r.send(resp)
		return
	}
	r.send(automationSummary(a))
}

// budgetSetPolicyKeys are the set-budget arguments that describe a policy; the
// rest are member, expected_version and estimated_cost_usd.
var budgetSetPolicyKeys = []string{
	"ceiling_type", "ceiling_value", "window", "pacing", "on_breach", "backend",
	"allocation_estimate", "peer_window", "reset_time", "reset_weekday", "reset_day", "reset_zone",
}

// automationSetBudget serves "set-budget <id> key=value...". The keys are the
// REST and MCP argument names; member is accepted for member_id. Input that
// cannot be a valid request is answered with the usage line and never reaches
// the daemon; the daemon stays the judge of everything else.
func (r *Router) automationSetBudget(args []string) {
	if len(args) < 3 {
		r.send(automationSetBudgetUsage)
		return
	}
	body, err := budgetSetBody(args[2:])
	if err != nil {
		r.send(err.Error() + "\n" + automationSetBudgetUsage)
		return
	}
	body["actor"] = "comm"
	b, err := json.Marshal(body)
	if err != nil {
		r.send("automation set-budget error: " + err.Error())
		return
	}
	resp, err := r.commJSON(http.MethodPost, "/api/automations/"+url.PathEscape(args[1])+"/set_budget", string(b))
	if err != nil {
		r.send("automation set-budget error: " + err.Error())
		return
	}
	var a automationBudgetReply
	if json.Unmarshal([]byte(resp), &a) != nil || a.ID == "" {
		r.send(resp)
		return
	}
	r.send(automationSummary(a))
}

// budgetSetBody turns the key=value tokens into the set_budget request body.
func budgetSetBody(tokens []string) (map[string]any, error) {
	body := map[string]any{}
	for _, tok := range tokens {
		k, v, ok := strings.Cut(tok, "=")
		if !ok || v == "" {
			return nil, fmt.Errorf("%q is not key=value", tok)
		}
		if k == "member" {
			k = "member_id"
		}
		if _, dup := body[k]; dup {
			return nil, fmt.Errorf("%s is given more than once", k)
		}
		switch k {
		case "member_id", "ceiling_type", "window", "pacing", "on_breach", "backend", "reset_time", "reset_weekday", "reset_zone":
			body[k] = v
		case "ceiling_value", "estimated_cost_usd":
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return nil, fmt.Errorf("%s must be a number, got %q", k, v)
			}
			body[k] = n
		case "expected_version", "reset_day":
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("%s must be a whole number, got %q", k, v)
			}
			body[k] = n
		case "allocation_estimate", "clear":
			t, err := strconv.ParseBool(v)
			if err != nil {
				return nil, fmt.Errorf("%s must be true or false, got %q", k, v)
			}
			body[k] = t
		case "peer_window":
			peers := strings.Split(v, ",")
			seen := map[string]bool{}
			for i, p := range peers {
				p = strings.TrimSpace(p)
				if p == "" || seen[p] {
					return nil, fmt.Errorf("peer_window must list each peer once, comma separated")
				}
				seen[p], peers[i] = true, p
			}
			body[k] = peers
		default:
			return nil, fmt.Errorf("unknown argument %q", k)
		}
	}
	return body, checkBudgetSetBody(body)
}

// checkBudgetSetBody checks the arguments against each other, the way the
// daemon will, so the common mistakes get the usage line without a request.
func checkBudgetSetBody(body map[string]any) error {
	var policy []string
	for _, k := range budgetSetPolicyKeys {
		if _, ok := body[k]; ok {
			policy = append(policy, k)
		}
	}
	_, hasCost := body["estimated_cost_usd"]
	if clear, _ := body["clear"].(bool); clear {
		if hasCost {
			policy = append(policy, "estimated_cost_usd")
		}
		if len(policy) > 0 {
			return fmt.Errorf("clear removes the policy and cannot be combined with %s", strings.Join(policy, ", "))
		}
		return nil
	}
	delete(body, "clear")
	if hasCost {
		if _, member := body["member_id"]; !member {
			return fmt.Errorf("estimated_cost_usd applies to a member; name it with member=<id>")
		}
		if n, _ := body["estimated_cost_usd"].(float64); n < 0 {
			return fmt.Errorf("estimated_cost_usd must not be negative")
		}
	}
	if len(policy) == 0 {
		if hasCost {
			return nil
		}
		return fmt.Errorf("nothing to set")
	}
	for _, f := range []struct {
		key     string
		allowed []string
	}{
		{"ceiling_type", []string{"dollar", "allocation_pct"}},
		{"window", []string{"automation", "day", "week", "month"}},
		{"pacing", []string{"none", "even"}},
		{"on_breach", []string{"pause", "block"}},
	} {
		v, given := body[f.key].(string)
		if !given && (f.key == "ceiling_type" || f.key == "window") {
			return fmt.Errorf("%s is required with the policy arguments", f.key)
		}
		if given && !containsString(f.allowed, v) {
			return fmt.Errorf("%s must be %s, got %q", f.key, strings.Join(f.allowed, " or "), v)
		}
	}
	if n, _ := body["ceiling_value"].(float64); n <= 0 {
		return fmt.Errorf("ceiling_value is required and must be positive")
	}
	return nil
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
