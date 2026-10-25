// Comm-channel verbs for the judge routing settings, under "autonomous":
//
//	autonomous judge-policy                                   show the judge backends and time limit
//	autonomous judge-policy-set key=value...                  set judge_policy and/or judge_timeout_seconds
//
// The settings are autonomous.judge_policy and autonomous.judge_timeout_seconds
// and are written through PUT /api/autonomous/config, like the verifier policy
// verbs in verifier_policy_cmd.go. An empty judge_policy follows the verifier
// routing policy; a zero time limit means the 1800-second default.

package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const (
	judgePolicyShowUsage = "usage: autonomous judge-policy"
	judgePolicySetUsage  = "usage: autonomous judge-policy-set judge_policy=<a,b> judge_timeout_seconds=<n> " +
		"(name at least one; judge_policy is an ordered backend list, empty clears it so the judges follow the verifier routing policy; " +
		"judge_timeout_seconds is a whole number of seconds, 0 = 1800)"
)

// judgePolicyConfigBody turns key=value tokens into the top-level judge keys
// of a config body. judge_policy is a comma list (empty clears it) and
// judge_timeout_seconds a whole number. An unknown key, a token without '=' or
// a value that cannot parse is an error.
func judgePolicyConfigBody(tokens []string) (map[string]any, error) {
	body := map[string]any{}
	for _, tok := range tokens {
		k, v, ok := strings.Cut(tok, "=")
		if !ok {
			return nil, fmt.Errorf("%q is not key=value", tok)
		}
		k = strings.ReplaceAll(strings.TrimSpace(k), "-", "_")
		v = strings.TrimSpace(v)
		switch k {
		case "judge_policy":
			body[k] = splitVerifierPolicyBackends(v)
		case "judge_timeout_seconds":
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("%s must be a whole number >= 0, got %q", k, v)
			}
			body[k] = n
		default:
			return nil, fmt.Errorf("unknown key %q", k)
		}
	}
	return body, nil
}

// autonomousJudgePolicy serves the two judge verbs. args[0] is the verb.
func (r *Router) autonomousJudgePolicy(args []string) {
	verb := strings.ReplaceAll(strings.ToLower(args[0]), "_", "-")
	title := "autonomous " + verb
	switch verb {
	case "judge-policy":
		if len(args) != 1 {
			r.reply(title+" failed", judgePolicyShowUsage)
			return
		}
		out, err := r.commGet("/api/autonomous/config", nil)
		if err != nil {
			r.reply(title+" failed", err.Error())
			return
		}
		var cfg map[string]json.RawMessage
		if json.Unmarshal([]byte(out), &cfg) == nil {
			judge := map[string]json.RawMessage{
				"judge_policy":          cfg["judge_policy"],
				"judge_timeout_seconds": cfg["judge_timeout_seconds"],
			}
			if raw, err := json.Marshal(judge); err == nil {
				out = string(raw)
			}
		}
		r.reply(title, prettyJSON(out))
	case "judge-policy-set":
		body, err := judgePolicyConfigBody(args[1:])
		if err != nil || len(body) == 0 {
			msg := judgePolicySetUsage
			if err != nil {
				msg = err.Error() + "; " + msg
			}
			r.reply(title+" failed", msg)
			return
		}
		raw, _ := json.Marshal(body)
		out, err := r.commJSON(http.MethodPut, "/api/autonomous/config", string(raw))
		if err != nil {
			r.reply(title+" failed", err.Error())
			return
		}
		r.reply(title, prettyJSON(out))
	}
}
