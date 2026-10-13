// BL410 Phase 1 remediation — comm-channel verbs for retry profiles +
// per-Automaton retry-policy overrides. Mirrors guardrails.go's
// "guardrail profile"/"guardrail automaton set" verbs exactly: this is
// the comm-channel sibling of cli_retry.go, both reusing the same
// REST endpoints registered for the PWA/MCP/REST surfaces.
//
// Supported verbs (all proxied through the in-process REST loopback):
//
//	"retry profile list"                               → list profiles
//	"retry profile create <name> [k=v,k=v...]"         → create profile (budgets CSV)
//	"retry profile get <id>"                           → get profile
//	"retry profile delete <id>"                        → delete profile
//	"retry automaton set <id> [...]"                   → per-Automaton override
package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func (r *Router) handleRetryCmd(cmd Command) {
	args := strings.Fields(cmd.Text)
	if len(args) == 0 {
		r.send("usage: retry profile | retry automaton")
		return
	}
	switch args[0] {
	case "profile":
		r.retryProfile(args[1:])
	case "automaton":
		r.retryAutomaton(args[1:])
	default:
		r.send(fmt.Sprintf("retry: unknown subcommand %q. Try: profile | automaton", args[0]))
	}
}

func (r *Router) retryProfile(args []string) {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "list":
		body, err := r.commGet("/api/autonomous/retry_profiles", nil)
		if err != nil {
			r.send("retry profile list error: " + err.Error())
			return
		}
		r.send(body)

	case "get":
		if len(args) < 2 {
			r.send("usage: retry profile get <id>")
			return
		}
		body, err := r.commGet("/api/autonomous/retry_profiles/"+args[1], nil)
		if err != nil {
			r.send("retry profile get error: " + err.Error())
			return
		}
		r.send(body)

	case "create":
		if len(args) < 2 {
			r.send("usage: retry profile create <name> [infra=4,content=1,...] [ceiling=<n>]")
			return
		}
		name := args[1]
		body := map[string]any{"name": name}
		for _, flag := range args[2:] {
			if strings.HasPrefix(flag, "ceiling=") {
				n, err := strconv.Atoi(strings.TrimPrefix(flag, "ceiling="))
				if err != nil {
					r.send("retry profile create error: bad ceiling: " + err.Error())
					return
				}
				body["ceiling_total"] = n
				continue
			}
			budgets, err := csvToIntMap(flag)
			if err != nil {
				r.send("retry profile create error: bad budgets: " + err.Error())
				return
			}
			body["budgets"] = budgets
		}
		payload, _ := json.Marshal(body)
		resp, err := r.commJSON(http.MethodPost, "/api/autonomous/retry_profiles", string(payload))
		if err != nil {
			r.send("retry profile create error: " + err.Error())
			return
		}
		r.send(resp)

	case "delete":
		if len(args) < 2 {
			r.send("usage: retry profile delete <id>")
			return
		}
		_, err := r.commJSON(http.MethodDelete, "/api/autonomous/retry_profiles/"+args[1], "")
		if err != nil {
			r.send("retry profile delete error: " + err.Error())
			return
		}
		r.send("deleted")

	default:
		r.send(fmt.Sprintf("retry profile: unknown subcommand %q. Try: list | get | create | delete", sub))
	}
}

func (r *Router) retryAutomaton(args []string) {
	if len(args) < 2 || args[0] != "set" {
		r.send("usage: retry automaton set <id> [profile=<name>] [policy=k=v,k=v] [ceiling=<n>]")
		return
	}
	id := args[1]
	body := map[string]any{}
	for _, flag := range args[2:] {
		switch {
		case strings.HasPrefix(flag, "profile="):
			body["retry_profile"] = strings.TrimPrefix(flag, "profile=")
		case strings.HasPrefix(flag, "policy="):
			policy, err := csvToIntMap(strings.TrimPrefix(flag, "policy="))
			if err != nil {
				r.send("retry automaton set error: bad policy: " + err.Error())
				return
			}
			body["retry_policy"] = policy
		case strings.HasPrefix(flag, "ceiling="):
			n, err := strconv.Atoi(strings.TrimPrefix(flag, "ceiling="))
			if err != nil {
				r.send("retry automaton set error: bad ceiling: " + err.Error())
				return
			}
			body["retry_ceiling_total"] = n
		}
	}
	payload, _ := json.Marshal(body)
	resp, err := r.commJSON(http.MethodPut, "/api/autonomous/prds/"+id+"/set_retry_policy", string(payload))
	if err != nil {
		r.send("retry automaton set error: " + err.Error())
		return
	}
	r.send(resp)
}

// csvToIntMap parses "k=v,k=v" into map[string]int — the comm-channel
// sibling of cli_retry.go's identical helper (duplicated, not shared,
// since cmd/datawatch and internal/router are separate packages with
// no existing cross-import for CLI-only helpers).
func csvToIntMap(s string) (map[string]int, error) {
	out := map[string]int{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return nil, err
		}
		out[strings.TrimSpace(k)] = n
	}
	return out, nil
}
