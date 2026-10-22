// v7.0.0 S2 — comm-channel verbs for the LLM-inference registry.
//
//	llm                                              → list LLMs
//	llm list                                         → list LLMs
//	llm get <name>                                   → fetch one
//	llm add <name> kind=<k> [k=v ...]                → add
//	llm update <name> [k=v ...]                      → update
//	llm delete <name>                                → remove
//	llm test <name> [prompt=<text>]                  → one-shot probe

package router

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/inference"
)

const llmUsage = `Usage:
  llm                                              list LLMs
  llm list                                         list LLMs
  llm get <name>                                   fetch one
  llm add <name> kind=<k> model=<m> [k=v ...]      add (kind=ollama|openwebui|opencode|claude)
  llm update <name> [k=v ...]                      update
  llm delete <name>                                remove
  llm test <name> [prompt=<text>]                  one-shot inference probe
  llm enable <name> [pretest=true]                 enable (optionally probe first)
  llm disable <name>                               disable — dispatcher refuses to route until re-enabled
  llm models list <name>                           list enabled models for an LLM
  llm models add <name> model=<m> [node=<n>]       add an enabled model
  llm models remove <name> model=<m> [node=<n>]    remove an enabled model
  llm in-use <name> [filter=<text>]                list sessions/automata bound to this LLM
  llm refresh-models <name>                        trigger model-list refresh from ComputeNodes

Common kv pairs:
  model=<name>            compute_nodes=<csv>      api_key_ref=<literal-or-${secret:name}>
  timeout_seconds=<N>     tags=<csv>               binary=<path>
  console_cols=<N>        console_rows=<N>         output_mode=terminal|log|chat
  auto_git_init=true      auto_git_commit=true
  skip_permissions=true   channel_enabled=true     auto_accept_disclaimer=true
  permission_mode=plan     default_effort=normal    fallback_chain=<csv>
  plan_tier=pro|max_5x|max_20x|api   (LLM's subscription tier; the daemon's
                                      autonomous.plan_tier_max_inflight table then
                                      caps its inflight unless max_inflight>0 wins)
  max_inflight=<N>        (explicit inflight cap on the LLM; N>=0, 0=unlimited,
                           wins over the plan_tier table and the kind default)

Budget kv pairs (a dollar ceiling over one window; the set is replaced as a whole):
  budget_ceiling_usd=<N>  budget_hours=<N>         rolling window of N trailing hours (1-720)
  budget_weekday=monday..sunday  budget_time=HH:MM  budget_zone=<IANA zone>
                          weekly window; use budget_hours or budget_weekday, not both
  budget_allocation_pct=true   also report an estimated share of the plan-tier allocation
  budget_peer_window=<csv>     federation peers whose spend counts toward the ceiling
  budget=clear                 remove the budget
  (see "budget status [llm]" for spend, window end and rollover)`

func (r *Router) handleLLMCmd(cmd Command) {
	text := strings.TrimSpace(cmd.Text)
	if text == "" || strings.EqualFold(text, "list") {
		out, err := r.commGet("/api/llms", nil)
		if err != nil {
			r.reply("llm", err.Error())
			return
		}
		r.reply("llm", prettyJSON(out))
		return
	}
	if strings.EqualFold(text, "help") {
		r.reply("llm", llmUsage)
		return
	}
	parts := strings.SplitN(text, " ", 2)
	verb := strings.ToLower(parts[0])
	tail := ""
	if len(parts) > 1 {
		tail = strings.TrimSpace(parts[1])
	}
	switch verb {
	case "get":
		if tail == "" {
			r.reply("llm get", "Usage: llm get <name>")
			return
		}
		out, err := r.commGet("/api/llms/"+tail, nil)
		if err != nil {
			r.reply("llm get", err.Error())
			return
		}
		r.reply("llm "+tail, prettyJSON(out))
	case "add", "update":
		nameAndKV := strings.SplitN(tail, " ", 2)
		if len(nameAndKV) < 1 || nameAndKV[0] == "" {
			r.reply("llm "+verb, "Usage: llm "+verb+" <name> kind=<k> model=<m> [k=v ...]")
			return
		}
		name := nameAndKV[0]
		kvText := ""
		if len(nameAndKV) > 1 {
			kvText = nameAndKV[1]
		}
		body := parseLLMKVPairs(kvText)
		body["name"] = name
		budget, budgetSet, berr := parseLLMBudgetKV(kvText)
		if berr != nil {
			r.reply("llm "+verb, berr.Error())
			return
		}
		if budgetSet {
			body["budget"] = budget
		}
		method := "POST"
		path := "/api/llms"
		if verb == "update" {
			method = "PUT"
			path = "/api/llms/" + name
		}
		bodyJSON, _ := json.Marshal(body)
		out, err := r.commJSON(method, path, string(bodyJSON))
		if err != nil {
			r.reply("llm "+verb, err.Error())
			return
		}
		r.reply("llm "+verb, prettyJSON(out))
	case "delete":
		if tail == "" {
			r.reply("llm delete", "Usage: llm delete <name>")
			return
		}
		out, err := r.commJSON("DELETE", "/api/llms/"+tail, "")
		if err != nil {
			r.reply("llm delete", err.Error())
			return
		}
		r.reply("llm delete "+tail, prettyJSON(out))
	case "enable", "disable":
		bits := strings.SplitN(tail, " ", 2)
		if len(bits) < 1 || bits[0] == "" {
			r.reply("llm "+verb, "Usage: llm "+verb+" <name> [pretest=true]")
			return
		}
		body := map[string]any{"enabled": verb == "enable"}
		if len(bits) > 1 {
			kv := parseLLMKVPairs(bits[1])
			if v, ok := kv["pretest"]; ok && verb == "enable" {
				if s, _ := v.(string); strings.EqualFold(s, "true") || s == "1" {
					body["pretest"] = true
				}
			}
		}
		bodyJSON, _ := json.Marshal(body)
		out, err := r.commJSON("PATCH", "/api/llms/"+bits[0]+"/enabled", string(bodyJSON))
		if err != nil {
			r.reply("llm "+verb, err.Error())
			return
		}
		r.reply("llm "+verb+" "+bits[0], prettyJSON(out))
	case "test":
		bits := strings.SplitN(tail, " ", 2)
		if len(bits) < 1 || bits[0] == "" {
			r.reply("llm test", "Usage: llm test <name> [prompt=<text>]")
			return
		}
		body := map[string]any{}
		if len(bits) > 1 {
			kv := parseLLMKVPairs(bits[1])
			if p, ok := kv["prompt"]; ok {
				body["prompt"] = p
			}
		}
		bodyJSON, _ := json.Marshal(body)
		out, err := r.commJSON("POST", "/api/llms/"+bits[0]+"/test", string(bodyJSON))
		if err != nil {
			r.reply("llm test", err.Error())
			return
		}
		r.reply("llm test "+bits[0], prettyJSON(out))

	// v7.0.0-alpha.37 — enabled models + in-use + refresh.
	case "models":
		// llm models <subverb> <llm-name> [k=v ...]
		parts2 := strings.SplitN(tail, " ", 3)
		if len(parts2) < 2 {
			r.reply("llm models", "Usage: llm models list|add|remove <llm-name> [node=<n> model=<m>]")
			return
		}
		subverb := strings.ToLower(parts2[0])
		llmName := parts2[1]
		kvText := ""
		if len(parts2) > 2 {
			kvText = parts2[2]
		}
		switch subverb {
		case "list":
			out, err := r.commGet("/api/llms/"+llmName, nil)
			if err != nil {
				r.reply("llm models list", err.Error())
				return
			}
			// Extract just the models field.
			var parsed map[string]any
			if e := json.Unmarshal([]byte(out), &parsed); e == nil {
				models := parsed["models"]
				if models == nil {
					models = []any{}
				}
				b, _ := json.Marshal(map[string]any{"models": models})
				out = string(b)
			}
			r.reply("llm models "+llmName, prettyJSON(out))
		case "add", "remove":
			kv := parseLLMKVPairs(kvText)
			raw, err := r.commGet("/api/llms/"+llmName, nil)
			if err != nil {
				r.reply("llm models "+subverb, err.Error())
				return
			}
			body := map[string]any{}
			if b, e := json.Marshal(raw); e == nil {
				_ = json.Unmarshal(b, &body)
			}
			if err2 := json.Unmarshal([]byte(raw), &body); err2 != nil {
				r.reply("llm models "+subverb, "parse error: "+err2.Error())
				return
			}
			models, _ := body["models"].([]any)
			node, _ := kv["node"].(string)
			model, _ := kv["model"].(string)
			if model == "" {
				r.reply("llm models "+subverb, "model= required")
				return
			}
			if subverb == "add" {
				models = append(models, map[string]any{"node": node, "model": model})
			} else {
				var kept []any
				for _, m := range models {
					if mm, ok := m.(map[string]any); ok {
						if mm["model"] == model && (node == "" || mm["node"] == node) {
							continue
						}
					}
					kept = append(kept, m)
				}
				models = kept
			}
			body["models"] = models
			bodyJSON, _ := json.Marshal(body)
			out, err := r.commJSON("PUT", "/api/llms/"+llmName, string(bodyJSON))
			if err != nil {
				r.reply("llm models "+subverb, err.Error())
				return
			}
			r.reply("llm models "+subverb+" "+llmName, prettyJSON(out))
		default:
			r.reply("llm models", "Usage: llm models list|add|remove <llm-name> [node=<n> model=<m>]")
		}

	case "in-use":
		if tail == "" {
			r.reply("llm in-use", "Usage: llm in-use <name> [filter=<text>]")
			return
		}
		bits := strings.SplitN(tail, " ", 2)
		params := map[string]string{}
		if len(bits) > 1 {
			for _, kv := range strings.Fields(bits[1]) {
				eq := strings.IndexRune(kv, '=')
				if eq > 0 {
					params[kv[:eq]] = kv[eq+1:]
				}
			}
		}
		// Build query string.
		qparts := ""
		for k, v := range params {
			if qparts != "" {
				qparts += "&"
			}
			qparts += k + "=" + v
		}
		path := "/api/llms/" + bits[0] + "/in_use"
		if qparts != "" {
			path += "?" + qparts
		}
		out, err := r.commGet(path, nil)
		if err != nil {
			r.reply("llm in-use", err.Error())
			return
		}
		r.reply("llm in-use "+bits[0], prettyJSON(out))

	case "refresh-models":
		if tail == "" {
			r.reply("llm refresh-models", "Usage: llm refresh-models <name>")
			return
		}
		out, err := r.commJSON("POST", "/api/llms/"+tail+"/refresh_models", "{}")
		if err != nil {
			r.reply("llm refresh-models", err.Error())
			return
		}
		r.reply("llm refresh-models "+tail, prettyJSON(out))

	default:
		r.reply("llm", llmUsage)
	}
}

func parseLLMKVPairs(s string) map[string]any {
	out := map[string]any{}
	for _, kv := range strings.Fields(s) {
		eq := strings.IndexRune(kv, '=')
		if eq < 0 {
			continue
		}
		k := strings.ToLower(kv[:eq])
		v := kv[eq+1:]
		switch k {
		case "compute_nodes", "tags", "fallback_chain":
			out[k] = strings.Split(v, ",")
		case "timeout_seconds", "console_cols", "console_rows", "max_inflight":
			out[k] = atoiOrZero(v)
		case "budget", "budget_ceiling_usd", "budget_hours", "budget_weekday", "budget_time",
			"budget_zone", "budget_allocation_pct", "budget_peer_window":
			// Assembled into one "budget" object by parseLLMBudgetKV.
			continue
		case "plan_tier":
			v = strings.TrimSpace(strings.ToLower(v))
			if v == "" || v == "unset" || v == "none" {
				continue
			}
			out[k] = v
		case "auto_git_init", "auto_git_commit", "skip_permissions",
			"channel_enabled", "auto_accept_disclaimer", "auto_add_models":
			out[k] = strings.EqualFold(v, "true") || v == "1"
		default:
			out[k] = v
		}
	}
	return out
}

// parseLLMBudgetKV extracts the budget_* key=value pairs of an llm add/update
// line into the "budget" request member. set is false when none were given,
// so an update keeps the stored budget; budget=clear yields a nil value with
// set true, which clears it. An invalid combination is rejected here, before
// any request is sent, with the same BudgetWindow.Validate the daemon uses.
func parseLLMBudgetKV(s string) (budget any, set bool, err error) {
	var bw inference.BudgetWindow
	var given []string
	clear := false
	for _, kv := range strings.Fields(s) {
		eq := strings.IndexRune(kv, '=')
		if eq < 0 {
			continue
		}
		k := strings.ToLower(kv[:eq])
		v := kv[eq+1:]
		switch k {
		case "budget":
			if !strings.EqualFold(v, "clear") && !strings.EqualFold(v, "none") {
				return nil, false, fmt.Errorf("budget=%s: only budget=clear is accepted; set a budget with budget_ceiling_usd and budget_hours or budget_weekday", v)
			}
			clear = true
			continue
		case "budget_ceiling_usd":
			f, perr := strconv.ParseFloat(v, 64)
			if perr != nil {
				return nil, false, fmt.Errorf("budget_ceiling_usd=%q is not a number", v)
			}
			bw.CeilingUSD = f
		case "budget_hours":
			n, perr := strconv.Atoi(v)
			if perr != nil {
				return nil, false, fmt.Errorf("budget_hours=%q is not a whole number", v)
			}
			bw.Hours = n
		case "budget_weekday":
			bw.Weekday = v
		case "budget_time":
			bw.Time = v
		case "budget_zone":
			bw.Zone = v
		case "budget_allocation_pct":
			bw.AllocationPct = strings.EqualFold(v, "true") || v == "1"
		case "budget_peer_window":
			for _, p := range strings.Split(v, ",") {
				bw.PeerWindow = append(bw.PeerWindow, strings.TrimSpace(p))
			}
		default:
			continue
		}
		given = append(given, k)
	}
	if clear {
		if len(given) > 0 {
			return nil, false, fmt.Errorf("budget=clear cannot be combined with %s", strings.Join(given, ", "))
		}
		return nil, true, nil
	}
	if len(given) == 0 {
		return nil, false, nil
	}
	if verr := bw.Validate(); verr != nil {
		return nil, false, verr
	}
	return &bw, true, nil
}
