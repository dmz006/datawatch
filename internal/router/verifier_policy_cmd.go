// Comm-channel verbs for verifier routing, under "autonomous":
//
//	autonomous verifier-policy                                     show the global policy
//	autonomous verifier-policy-set key=value...                    set fields of the global policy
//	autonomous set-verifier-policy <prd> [mode=X] [backends=a,b]   override one Automaton
//	autonomous set-task-verifier-policy <prd> <task> [mode=X] [backends=a,b]
//
// The global policy is autonomous.verifier_policy and is written through
// PUT /api/autonomous/config; the overrides call the presence-aware
// set_verifier_policy and set_task_verifier_policy actions. Sibling of the
// CLI commands in cmd/datawatch/cli_autonomous.go and the MCP tools in
// internal/mcp/verifier_policy_tools.go.

package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	verifierPolicyModes = "fixed|prefer-free|balance|gpu-only|claude-only"

	verifierPolicyShowUsage = "usage: autonomous verifier-policy"
	verifierPolicySetUsage  = "usage: autonomous verifier-policy-set mode=" + verifierPolicyModes + " backends=<a,b> " +
		"claude_session_cap=<n> claude_cap_window_minutes=<n> force_stronger_on_retry=true|false " +
		"final_gate_stronger=true|false stronger_backend=<b> stronger_model=<m> " +
		"(name at least one; an empty value clears the field; backends is a comma list)"
	verifierPolicyPRDUsage = "usage: autonomous set-verifier-policy <prd-id> [mode=" + verifierPolicyModes + "] [backends=<a,b>] " +
		"(name at least one; an empty value clears; precedence task > Automaton > global verifier_policy)"
	verifierPolicyTaskUsage = "usage: autonomous set-task-verifier-policy <prd-id> <task-id> [mode=" + verifierPolicyModes + "] [backends=<a,b>] " +
		"(name at least one; an empty value clears; precedence task > Automaton > global verifier_policy)"
)

// verifierPolicySetKeys maps each key of verifier-policy-set to its kind.
var verifierPolicySetKeys = map[string]string{
	"mode":                      "string",
	"backends":                  "list",
	"claude_session_cap":        "int",
	"claude_cap_window_minutes": "int",
	"force_stronger_on_retry":   "bool",
	"final_gate_stronger":       "bool",
	"stronger_backend":          "string",
	"stronger_model":            "string",
}

// splitVerifierPolicyBackends splits a comma list. An empty string is an empty
// list (clear). Empty entries are kept so the daemon rejects them instead of
// a stray comma quietly shrinking the list.
func splitVerifierPolicyBackends(v string) []string {
	if strings.TrimSpace(v) == "" {
		return []string{}
	}
	parts := strings.Split(v, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// verifierPolicyConfigBody turns key=value tokens into the verifier_policy
// object of a config body. A key that is unknown, a token without '=' or a
// value that cannot parse is an error.
func verifierPolicyConfigBody(tokens []string) (map[string]any, error) {
	vp := map[string]any{}
	for _, tok := range tokens {
		k, v, ok := strings.Cut(tok, "=")
		if !ok {
			return nil, fmt.Errorf("%q is not key=value", tok)
		}
		k = strings.ReplaceAll(strings.TrimSpace(k), "-", "_")
		v = strings.TrimSpace(v)
		switch verifierPolicySetKeys[k] {
		case "string":
			vp[k] = v
		case "list":
			vp[k] = splitVerifierPolicyBackends(v)
		case "int":
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("%s must be a whole number >= 0, got %q", k, v)
			}
			vp[k] = n
		case "bool":
			b, err := strconv.ParseBool(v)
			if err != nil {
				return nil, fmt.Errorf("%s must be true or false, got %q", k, v)
			}
			vp[k] = b
		default:
			return nil, fmt.Errorf("unknown key %q", k)
		}
	}
	return vp, nil
}

// verifierPolicyOverrideBody turns mode= and backends= tokens into a
// set_verifier_policy body. Only the keys given are sent, so an absent key
// leaves the field unchanged and an empty value clears it.
func verifierPolicyOverrideBody(tokens []string) (map[string]any, error) {
	body := map[string]any{"actor": "operator"}
	for _, tok := range tokens {
		k, v, ok := strings.Cut(tok, "=")
		if !ok {
			return nil, fmt.Errorf("%q is not key=value", tok)
		}
		switch strings.TrimSpace(k) {
		case "mode":
			body["mode"] = strings.TrimSpace(v)
		case "backends":
			body["backends"] = splitVerifierPolicyBackends(v)
		default:
			return nil, fmt.Errorf("unknown key %q", k)
		}
	}
	if len(body) == 1 {
		return nil, fmt.Errorf("no key given")
	}
	return body, nil
}

// autonomousVerifierPolicy serves the four verbs. args[0] is the verb.
func (r *Router) autonomousVerifierPolicy(args []string) {
	verb := strings.ReplaceAll(strings.ToLower(args[0]), "_", "-")
	title := "autonomous " + verb
	switch verb {
	case "verifier-policy":
		if len(args) != 1 {
			r.reply(title+" failed", verifierPolicyShowUsage)
			return
		}
		out, err := r.commGet("/api/autonomous/config", nil)
		if err != nil {
			r.reply(title+" failed", err.Error())
			return
		}
		var cfg map[string]json.RawMessage
		if json.Unmarshal([]byte(out), &cfg) == nil {
			if vp, ok := cfg["verifier_policy"]; ok {
				out = string(vp)
			}
		}
		r.reply(title, prettyJSON(out))
	case "verifier-policy-set":
		vp, err := verifierPolicyConfigBody(args[1:])
		if err != nil || len(vp) == 0 {
			msg := verifierPolicySetUsage
			if err != nil {
				msg = err.Error() + "; " + msg
			}
			r.reply(title+" failed", msg)
			return
		}
		raw, _ := json.Marshal(map[string]any{"verifier_policy": vp})
		out, err := r.commJSON(http.MethodPut, "/api/autonomous/config", string(raw))
		if err != nil {
			r.reply(title+" failed", err.Error())
			return
		}
		r.reply(title, prettyJSON(out))
	case "set-verifier-policy", "set-task-verifier-policy":
		task := verb == "set-task-verifier-policy"
		need, usage := 2, verifierPolicyPRDUsage
		if task {
			need, usage = 3, verifierPolicyTaskUsage
		}
		if len(args) < need {
			r.reply(title+" failed", usage)
			return
		}
		body, err := verifierPolicyOverrideBody(args[need:])
		if err != nil {
			r.reply(title+" failed", err.Error()+"; "+usage)
			return
		}
		route := "set_verifier_policy"
		if task {
			route = "set_task_verifier_policy"
			body["task_id"] = args[2]
		}
		raw, _ := json.Marshal(body)
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+url.PathEscape(args[1])+"/"+route, string(raw))
		if err != nil {
			r.reply(title+" failed", err.Error())
			return
		}
		r.reply(title, prettyJSON(out))
	}
}
