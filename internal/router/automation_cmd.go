// Comm-channel verbs for Automations: "automation activate <id>" starts a
// draft Automation; "automation return-to-draft <id>" returns a blocked
// Automation (no member started) to the draft the operator can still edit;
// "automation list" and "automation get <id>" read them;
// pause, resume, cancel, skip, insert, remove, edges and approve are the
// operator controls on a running one, and set-llm and set-verification-llm
// set its LLMs, and set-max-parallel and set-stage-gate set its run-shape
// limits. Sibling of cmd/datawatch/cli_automation.go
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
	automationUsage = "usage: automation activate <id> | return-to-draft <id> | list | get <id> | status <id> | shipped <id> commit=<c> [release=<r>] [source=merge_step|backfill] [merged_at=<RFC3339>] | pause <id> [reason] | resume <id> [reason] | " +
		"cancel <id> in-flight=true|false | skip <id> <member-id> [edge] | insert <id> <automaton-id|pending_item=<item-id>> [urgent] [stage=<stage>] <reason> | " +
		"insert <id> decision-wait [urgent] [stage=<stage>] <question> | <reason> | " +
		"remove <id> <member-id> | edges <id> <member-id> on_complete=<id> on_blocked=<id> on_failed=<id> | approve <id> <member-id> | " +
		"rename-stage <id> <from-stage> <to-stage> | " +
		"set-llm <id> [backend=<b>] [model=<m>] [effort=<e>] [planning-backend=<b>] [planning-model=<m>] | " +
		"set-verification-llm <id> [backend=<b>] [model=<m>] [effort=<e>] | " +
		"set-decision-assistant <id> backend=<b> model=<m> [mode=shadow|live] [hooks=<a,b>] [threshold.<hook>=<n>] [member=<id>] | " +
		"set-decision-assistant <id> clear=true [member=<id>] | " +
		"set-max-parallel <id> <n> [member=<fork-id>] (most members that run at once, 0 = no limit) | " +
		"set-stage-gate <id> on|off (hold a staged member until every earlier stage has finished) | " +
		"set-budget <id> key=value... (ceiling_type, ceiling_value, window, pacing, on_breach, ...; clear=true removes the policy) | " +
		"set-effort-ceiling <id> kind=low|medium|high|cheaper_backend ... | set-effort-ceiling <id> clear=true | " +
		"import-graph <graph-id> [dry-run] (turn an orchestrator graph into a draft Automation; dry-run previews it)"
	automationCancelUsage = "usage: automation cancel <id> in-flight=true|false. The choice is required: " +
		"in-flight=true cancels members running now, in-flight=false lets them finish.\n" +
		"examples:\n  automation cancel ab12 in-flight=true\n  automation cancel ab12 in-flight=false"
	automationSkipUsage        = "usage: automation skip <id> <member-id> [on_complete|on_blocked|on_failed]"
	automationRenameStageUsage = "usage: automation rename-stage <id> <from-stage> <to-stage>"
	automationInsertUsage      = "usage: automation insert <id> <automaton-id|pending_item=<item-id>> [urgent] [stage=<stage>] <reason...>. A reason is required. pending_item=<id> inserts a pending item (work with no Automaton yet) that holds the Automation until one is attached. " +
		"automation insert <id> decision-wait [urgent] [stage=<stage>] <question...> | <reason...> inserts a member that asks the operator the question and holds only its own branch until it is answered; the question and the reason are both required, split by |."
	automationRemoveUsage  = "usage: automation remove <id> <member-id>"
	automationApproveUsage = "usage: automation approve <id> <member-id>"
	automationSetLLMUsage  = "usage: automation set-llm <id> [backend=<b>] [model=<m>] [effort=<e>] [planning-backend=<b>] [planning-model=<m>] " +
		"(name at least one; an empty value such as model= clears the field)"
	automationSetVerificationLLMUsage = "usage: automation set-verification-llm <id> [backend=<b>] [model=<m>] [effort=<e>] " +
		"(name at least one; an empty value such as model= clears the field)"
	automationStatusUsage    = "usage: automation status <id>"
	automationSetBudgetUsage = "usage: automation set-budget <id> ceiling_type=dollar|allocation_pct ceiling_value=<n> window=automation|day|week|month " +
		"[pacing=none|even] [on_breach=pause|block] [backend=<llm>] [allocation_estimate=true] [peer_window=<a,b>] " +
		"[reset_time=HH:MM] [reset_weekday=<day>] [reset_day=1..28] [reset_zone=<zone>] [member=<id>] [estimated_cost_usd=<n>] " +
		"[expected_version=<n>] | automation set-budget <id> clear=true [member=<id>] [expected_version=<n>]"
	automationSetDecisionAssistantUsage = "usage: automation set-decision-assistant <id> backend=<b> model=<m> [mode=shadow|live] " +
		"[hooks=empty_edge,skip_member,insert_member,budget_breach] [threshold.<hook>=<0..1>] [member=<id>] [expected_version=<n>] | " +
		"automation set-decision-assistant <id> clear=true [member=<id>]. Off by default; backend= (empty) or clear=true turns it off."
	automationSetMaxParallelUsage = "usage: automation set-max-parallel <id> <n> [member=<fork-id>] [expected_version=<n>]. " +
		"n is the most members that run at once; 0 clears the limit. member=<fork-id> limits that fork's branches instead of the whole Automation."
	automationSetStageGateUsage = "usage: automation set-stage-gate <id> on|off [expected_version=<n>]. " +
		"on holds a staged member until every earlier stage has finished; off makes a stage only a label."
	automationImportGraphUsage = "usage: automation import-graph <graph-id> [dry-run]. Turns an orchestrator graph into a draft " +
		"Automation; dry-run shows what would be created and stores nothing."
	automationEdgesUsage = "usage: automation edges <id> <member-id> on_complete=<id> on_blocked=<id> on_failed=<id> " +
		"(name at least one; an edge left out or given empty becomes no next member)"
)

// AutomationCommVerbs lists the verbs handled by the switch in
// handleAutomationCmd. Keep it in step with that switch.
func AutomationCommVerbs() []string {
	return []string{"activate", "return-to-draft", "shipped", "list", "get", "status", "set-budget", "set-effort-ceiling", "pause", "resume", "cancel",
		"skip", "insert", "remove", "edges", "approve", "rename-stage", "set-llm", "set-verification-llm", "set-decision-assistant", "set-max-parallel", "set-stage-gate", "import-graph"}
}

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
	case "return-to-draft":
		if len(args) < 2 {
			r.send("usage: automation return-to-draft <id>")
			return
		}
		resp, err := r.commJSON(http.MethodPost,
			"/api/automations/"+url.PathEscape(args[1])+"/return_to_draft", `{"actor":"comm"}`)
		if err != nil {
			r.send("automation return-to-draft error: " + err.Error())
			return
		}
		r.send(resp)
	case "shipped":
		r.automationShipped(args)
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
	case "set-effort-ceiling":
		r.automationSetEffortCeiling(args)
	case "import-graph":
		if len(args) < 2 || len(args) > 3 || (len(args) == 3 && args[2] != "dry-run") {
			r.send(automationImportGraphUsage)
			return
		}
		r.automationImportGraph(args[1], len(args) == 3)
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
		urgent, stage, reasons := false, "", []string{}
		for _, tok := range rest {
			switch tok {
			case "urgent":
				urgent = true
			case "stage":
				r.send(automationInsertUsage)
				return
			default:
				if v, ok := strings.CutPrefix(tok, "stage="); ok {
					if v == "" {
						r.send(automationInsertUsage)
						return
					}
					stage = v
					continue
				}
				reasons = append(reasons, tok)
			}
		}
		reason := strings.Join(reasons, " ")
		if reason == "" {
			r.send(automationInsertUsage)
			return
		}
		member := map[string]any{"prd_id": args[2]}
		if item, ok := strings.CutPrefix(args[2], "pending_item="); ok {
			if strings.TrimSpace(item) == "" {
				r.send(automationInsertUsage)
				return
			}
			member = map[string]any{"pending_item_id": item}
		}
		if args[2] == "decision-wait" || args[2] == "decision_wait" {
			question, why, ok := strings.Cut(reason, "|")
			question, reason = strings.TrimSpace(question), strings.TrimSpace(why)
			if !ok || question == "" || reason == "" {
				r.send(automationInsertUsage)
				return
			}
			member = map[string]any{"kind": "decision_wait", "question": question}
		}
		if stage != "" {
			member["stage"] = stage
		}
		r.automationControl("insert", args[1], "insert_member", "member inserted", map[string]any{
			"member": member,
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
	case "rename-stage":
		if len(args) != 4 {
			r.send(automationRenameStageUsage)
			return
		}
		r.automationControl("rename-stage", args[1], "stages/rename", "stage renamed", map[string]any{"from": args[2], "to": args[3]})
	case "approve":
		if len(args) != 3 {
			r.send(automationApproveUsage)
			return
		}
		r.automationControl("approve", args[1], "approve_insert", "member approved", map[string]any{"member_id": args[2]})
	case "set-llm":
		r.automationSetLLM(args, "set-llm", "set_llm", automationSetLLMUsage,
			map[string]string{"backend": "backend", "model": "model", "effort": "effort",
				"planning-backend": "planning_backend", "planning-model": "planning_model",
				"planning_backend": "planning_backend", "planning_model": "planning_model"})
	case "set-decision-assistant":
		r.automationSetDecisionAssistant(args)
	case "set-max-parallel":
		r.automationSetMaxParallel(args)
	case "set-stage-gate":
		r.automationSetStageGate(args)
	case "set-verification-llm":
		r.automationSetLLM(args, "set-verification-llm", "set_verification_llm", automationSetVerificationLLMUsage,
			map[string]string{"backend": "backend", "model": "model", "effort": "effort"})
	default:
		r.send(fmt.Sprintf("automation: unknown subcommand %q. Try: activate, return-to-draft, list, get, status, pause, resume, cancel, skip, insert, remove, edges, approve, rename-stage, set-llm, set-verification-llm, set-decision-assistant, set-max-parallel, set-stage-gate, set-budget, import-graph", args[0]))
	}
}

// automationImportGraph serves "import-graph <graph-id> [dry-run]" through
// POST /api/automations/import-graph/{graph_id} and replies with a short
// summary: member, fork and join counts and the warnings, or the reason the
// graph was refused (HTTP 422).
func (r *Router) automationImportGraph(graphID string, dryRun bool) {
	b, _ := json.Marshal(map[string]any{"dry_run": dryRun})
	resp, err := r.commJSON(http.MethodPost, "/api/automations/import-graph/"+url.PathEscape(graphID), string(b))
	if err != nil {
		if reason, ok := strings.CutPrefix(err.Error(), "HTTP 422: "); ok {
			r.send(fmt.Sprintf("graph %s cannot be imported: %s", graphID, strings.TrimSpace(reason)))
			return
		}
		r.send("automation import-graph error: " + err.Error())
		return
	}
	type member struct {
		Kind string `json:"kind"`
	}
	var out struct {
		ID         string   `json:"id"`
		Members    []member `json:"members"`
		Warnings   []string `json:"warnings"`
		Automation *struct {
			ID      string   `json:"id"`
			Members []member `json:"members"`
		} `json:"automation"`
		Input *struct {
			Members []member `json:"members"`
		} `json:"input"`
	}
	if json.Unmarshal([]byte(resp), &out) != nil {
		r.send(resp)
		return
	}
	id, members := out.ID, out.Members
	switch {
	case out.Input != nil:
		members = out.Input.Members
	case out.Automation != nil:
		id, members = out.Automation.ID, out.Automation.Members
	}
	forks, joins := 0, 0
	for _, m := range members {
		switch m.Kind {
		case "fork":
			forks++
		case "join":
			joins++
		}
	}
	var sb strings.Builder
	if dryRun {
		fmt.Fprintf(&sb, "graph %s dry run: would create a draft Automation\n", graphID)
	} else {
		fmt.Fprintf(&sb, "graph %s imported as Automation %s (draft)\n", graphID, id)
	}
	fmt.Fprintf(&sb, "members: %d, forks: %d, joins: %d\n", len(members), forks, joins)
	if len(out.Warnings) == 0 {
		sb.WriteString("warnings: none")
	} else {
		fmt.Fprintf(&sb, "warnings: %d", len(out.Warnings))
		for _, w := range out.Warnings {
			sb.WriteString("\n- " + w)
		}
	}
	r.send(sb.String())
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

// decisionAssistantBody turns the key=value tokens of set-decision-assistant
// into a REST body. hooks is a comma list, threshold.<hook> sets one hook's
// confidence threshold, clear is a boolean and member names a member. An unknown
// key, a token without '=' or a value that cannot parse is an error.
func decisionAssistantBody(tokens []string) (map[string]any, error) {
	body := map[string]any{}
	for _, tok := range tokens {
		k, v, ok := strings.Cut(tok, "=")
		if !ok {
			return nil, fmt.Errorf("%q is not key=value", tok)
		}
		switch {
		case k == "backend" || k == "model" || k == "mode":
			body[k] = v
		case k == "member" || k == "member_id":
			body["member_id"] = v
		case k == "hooks":
			var hooks []string
			for _, h := range strings.Split(v, ",") {
				if h = strings.TrimSpace(h); h != "" {
					hooks = append(hooks, h)
				}
			}
			body["hooks"] = hooks
		case k == "clear":
			b, err := strconv.ParseBool(v)
			if err != nil {
				return nil, fmt.Errorf("clear must be true or false, got %q", v)
			}
			if b {
				body["clear"] = true
			}
		case k == "expected_version" || k == "expected-version":
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("expected_version must be a number, got %q", v)
			}
			body["expected_version"] = n
		case strings.HasPrefix(k, "threshold."):
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return nil, fmt.Errorf("%s must be a number, got %q", k, v)
			}
			th, _ := body["thresholds"].(map[string]float64)
			if th == nil {
				th = map[string]float64{}
				body["thresholds"] = th
			}
			th[strings.TrimPrefix(k, "threshold.")] = n
		default:
			return nil, fmt.Errorf("unknown key %q", k)
		}
	}
	return body, nil
}

// automationSetDecisionAssistant serves "set-decision-assistant <id>
// key=value..." through POST /api/automations/{id}/set_decision_assistant. A
// body that cannot be valid (no key, clear mixed with settings, a backend
// without a model) is answered with the usage line and never reaches the daemon.
func (r *Router) automationSetDecisionAssistant(args []string) {
	if len(args) < 3 {
		r.send(automationSetDecisionAssistantUsage)
		return
	}
	body, err := decisionAssistantBody(args[2:])
	if err != nil {
		r.send(err.Error() + "\n" + automationSetDecisionAssistantUsage)
		return
	}
	_, clear := body["clear"]
	backend, hasBackend := body["backend"].(string)
	_, hasModel := body["model"]
	_, hasMode := body["mode"]
	_, hasHooks := body["hooks"]
	_, hasThresholds := body["thresholds"]
	setting := hasBackend || hasModel || hasMode || hasHooks || hasThresholds
	switch {
	case clear && setting:
		r.send("clear cannot be combined with backend, model, mode, hooks or thresholds\n" + automationSetDecisionAssistantUsage)
		return
	case !clear && !setting:
		r.send(automationSetDecisionAssistantUsage)
		return
	case !clear && (!hasBackend || backend != "") && (!hasBackend || backend == "" || !hasModel || body["model"] == ""):
		r.send("backend and model are required together\n" + automationSetDecisionAssistantUsage)
		return
	}
	r.automationControl("set-decision-assistant", args[1], "set_decision_assistant", "decision assistant updated", body)
}

// controlOptions splits the trailing tokens of a set-* verb into the value
// (a bare token, or the one named by valueKeys) and the other key=value
// options (member, expected_version). ok is false for a token that is neither,
// for a repeated value and for an unknown key.
func controlOptions(tokens []string, valueKeys ...string) (value string, opts map[string]string, ok bool) {
	opts = map[string]string{}
	have := false
	for _, tok := range tokens {
		k, v, isPair := strings.Cut(tok, "=")
		switch {
		case !isPair:
			if have {
				return "", nil, false
			}
			value, have = tok, true
		case containsString(valueKeys, k):
			if have {
				return "", nil, false
			}
			value, have = v, true
		case k == "member" || k == "member_id":
			opts["member_id"] = v
		case k == "expected_version" || k == "expected-version":
			opts["expected_version"] = v
		default:
			return "", nil, false
		}
	}
	return value, opts, have
}

// withControlOptions copies the member and expected_version options into body;
// a non-numeric expected_version is an error.
func withControlOptions(body map[string]any, opts map[string]string) error {
	if m := opts["member_id"]; m != "" {
		body["member_id"] = m
	}
	if v, present := opts["expected_version"]; present {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return fmt.Errorf("expected_version must be a whole number, got %q", v)
		}
		body["expected_version"] = n
	}
	return nil
}

// automationSetMaxParallel serves "set-max-parallel <id> <n> [member=<fork>]":
// the most members that run at once (0 clears the limit), for the Automation or
// for one fork's branches, through POST /api/automations/{id}/set_max_parallel.
func (r *Router) automationSetMaxParallel(args []string) {
	if len(args) < 3 {
		r.send(automationSetMaxParallelUsage)
		return
	}
	value, opts, ok := controlOptions(args[2:], "max_parallel", "max-parallel")
	n, err := strconv.Atoi(value)
	if !ok || err != nil || n < 0 {
		r.send(automationSetMaxParallelUsage)
		return
	}
	body := map[string]any{"max_parallel": n}
	if err := withControlOptions(body, opts); err != nil {
		r.send(err.Error() + "\n" + automationSetMaxParallelUsage)
		return
	}
	did := fmt.Sprintf("max parallel set to %d", n)
	if n == 0 {
		did = "max parallel cleared (no limit)"
	}
	if m := opts["member_id"]; m != "" {
		did = "fork " + m + " " + did
	}
	r.automationControl("set-max-parallel", args[1], "set_max_parallel", did, body)
}

// automationSetStageGate serves "set-stage-gate <id> on|off": whether a staged
// member waits for every earlier stage, through
// POST /api/automations/{id}/set_stage_gate.
func (r *Router) automationSetStageGate(args []string) {
	if len(args) < 3 {
		r.send(automationSetStageGateUsage)
		return
	}
	value, opts, ok := controlOptions(args[2:], "stage_gate", "stage-gate")
	on, known := map[string]bool{"on": true, "true": true, "off": false, "false": false}[strings.ToLower(value)]
	if !ok || !known || opts["member_id"] != "" {
		r.send(automationSetStageGateUsage)
		return
	}
	body := map[string]any{"stage_gate": on}
	if err := withControlOptions(body, opts); err != nil {
		r.send(err.Error() + "\n" + automationSetStageGateUsage)
		return
	}
	did := "stage gate turned off"
	if on {
		did = "stage gate turned on"
	}
	r.automationControl("set-stage-gate", args[1], "set_stage_gate", did, body)
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
	line := fmt.Sprintf("automation %s: %s (status %s, version %d)", a.ID, did, a.Status, a.Version)
	if extra := planningRequeueReplyLine(resp); extra != "" {
		line += "; " + extra
	}
	r.send(line)
}

// planningRequeueReplyLine renders the top-level `planning` outcome of a
// set-llm response (if any) as a compact comm-channel line, so an operator
// sees which pool a queued member's decomposition was retargeted to and
// whether it ran immediately or deferred. Empty when the response carries no
// planning change (true for every non-LLM op), keeping automationControl
// otherwise unchanged.
func planningRequeueReplyLine(resp string) string {
	var wrapper struct {
		Planning *autonomous.PlanningRequeueResult `json:"planning"`
	}
	if err := json.Unmarshal([]byte(resp), &wrapper); err != nil || wrapper.Planning == nil || wrapper.Planning.Outcome == "" {
		return ""
	}
	pr := wrapper.Planning
	reason := ""
	if pr.Reason != "" {
		reason = ": " + pr.Reason
	}
	switch pr.Outcome {
	case autonomous.PlanningUnchanged:
		return "planning pool unchanged" + reason
	case autonomous.PlanningRequeued:
		return fmt.Sprintf("planning requeued %s/%s → %s/%s%s", pr.OldBackend, pr.OldModel, pr.NewBackend, pr.NewModel, reason)
	case autonomous.PlanningDeferred:
		return "planning deferred to next run" + reason
	default:
		return "planning " + string(pr.Outcome) + reason
	}
}

// automationShippedUsage is the usage line for "automation shipped".
const automationShippedUsage = "usage: automation shipped <id> commit=<merge-commit> [release=<release>] [source=merge_step|backfill] [merged_at=<RFC3339>]"

// automationShipped serves "automation shipped <id> commit=...": it records the
// Automation's shipped state (release, merge commit, merge time, source) through
// POST /api/automations/{id}/shipped. Recording never changes the Automation's
// status and is idempotent per merge commit.
func (r *Router) automationShipped(args []string) {
	if len(args) < 2 {
		r.send(automationShippedUsage)
		return
	}
	id := args[1]
	body := map[string]any{"actor": "comm", "source": "backfill"}
	for _, kv := range args[2:] {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			r.send(automationShippedUsage)
			return
		}
		k := strings.ToLower(strings.TrimSpace(kv[:i]))
		v := strings.TrimSpace(kv[i+1:])
		switch k {
		case "commit", "merge_commit":
			body["merge_commit"] = v
		case "release", "merged_in_release":
			body["merged_in_release"] = v
		case "source":
			body["source"] = v
		case "merged_at", "at":
			body["merged_at"] = v
		default:
			r.send(automationShippedUsage)
			return
		}
	}
	b, err := json.Marshal(body)
	if err != nil {
		r.send("automation shipped error: " + err.Error())
		return
	}
	resp, err := r.commJSON(http.MethodPost, "/api/automations/"+url.PathEscape(id)+"/shipped", string(b))
	if err != nil {
		r.send("automation shipped error: " + err.Error())
		return
	}
	var a struct {
		ID          string `json:"id"`
		Status      string `json:"status"`
		Version     int64  `json:"version"`
		MergeCommit string `json:"merge_commit"`
	}
	if json.Unmarshal([]byte(resp), &a) != nil || a.ID == "" {
		r.send(resp)
		return
	}
	r.send(fmt.Sprintf("automation %s shipped record saved: merge_commit %s (status %s, version %d)", a.ID, a.MergeCommit, a.Status, a.Version))
}

// automationBudgetReply is the part of an Automation REST record that the
// status and set-budget replies print.
type automationBudgetReply struct {
	ID      string                  `json:"id"`
	Name    string                  `json:"name"`
	Status  string                  `json:"status"`
	Version int64                   `json:"version"`
	Members []automationReplyMember `json:"members"`
	Budget  *autonomous.BudgetView  `json:"budget"`
	// MaxParallel is the Automation's limit on members running at once (0 or
	// absent: none) and StageGate whether a member's stage holds it back.
	MaxParallel int  `json:"max_parallel"`
	StageGate   bool `json:"stage_gate"`
}

// automationReplyMember is the part of a member the status reply reads: the
// flags a member sets while it holds its cursor without running.
type automationReplyMember struct {
	ID                 string `json:"id"`
	Kind               string `json:"kind"`
	MaxParallel        int    `json:"max_parallel"`
	WaitingForSlot     bool   `json:"waiting_for_slot"`
	WaitingForStage    string `json:"waiting_for_stage"`
	WaitingForOperator bool   `json:"waiting_for_operator"`
}

// automationRunShapeLine is the "run shape" line of a status reply: the limit,
// the gate and the members waiting under them. It is empty when the Automation
// sets none of them and nothing waits, so a plain chain reads as before.
func automationRunShapeLine(a automationBudgetReply) string {
	var parts []string
	if a.MaxParallel > 0 {
		parts = append(parts, fmt.Sprintf("max %d at once", a.MaxParallel))
	}
	var forks []string
	var slot, stage, operator int
	for _, m := range a.Members {
		if m.Kind == autonomous.MemberKindFork && m.MaxParallel > 0 {
			forks = append(forks, fmt.Sprintf("fork %s max %d", m.ID, m.MaxParallel))
		}
		if m.WaitingForSlot {
			slot++
		}
		if m.WaitingForStage != "" {
			stage++
		}
		if m.WaitingForOperator {
			operator++
		}
	}
	parts = append(parts, forks...)
	if a.StageGate {
		parts = append(parts, "stage gate on")
	}
	var waiting []string
	if slot > 0 {
		waiting = append(waiting, fmt.Sprintf("%d for a free slot", slot))
	}
	if stage > 0 {
		waiting = append(waiting, fmt.Sprintf("%d for an earlier stage", stage))
	}
	if operator > 0 {
		waiting = append(waiting, fmt.Sprintf("%d for you", operator))
	}
	if len(waiting) > 0 {
		parts = append(parts, "waiting: "+strings.Join(waiting, ", "))
	}
	if len(parts) == 0 {
		return ""
	}
	return "run shape: " + strings.Join(parts, "; ")
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
	if line := automationRunShapeLine(a); line != "" {
		b.WriteString(line + "\n")
	}
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
