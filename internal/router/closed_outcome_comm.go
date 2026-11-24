// Closed work over the chat channel: record an outcome class, read or run the
// standing check, record a deploy, and cancel with an outcome. Each verb calls
// the same REST route the CLI and MCP tools use.

package router

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const (
	outcomeUsage       = "usage: autonomous outcome <id> outcome=delivered|superseded|dropped|held [via=<id>] [archive=true] [note=<text, last>]"
	outcomeCheckUsage  = "usage: autonomous outcome-check [run=true]"
	deployRecordUsage  = "usage: autonomous deploy-record release=<release> [version=<v>] [host=<h>] [health=passed|failed|unchecked] [at=<RFC3339>] [detail=<text, last>]"
	cancelOutcomeUsage = "usage: autonomous cancel <prd-id> [outcome=delivered|superseded|dropped|held] [via=<id>] [note=<text, last> | keep|drop <reason, last>]"
)

// ClosedOutcomeCommVerbs lists the comm verbs, for the parity test.
func ClosedOutcomeCommVerbs() []string {
	return []string{"outcome", "outcome-check", "outcome_check", "deploy-record", "deploy_record"}
}

// parseOutcomeTokens turns key=value tokens into a map. A note token takes the
// rest of the line, so a note can hold spaces; it must come last.
func parseOutcomeTokens(tokens []string) (map[string]string, bool) {
	out := map[string]string{}
	for i, tok := range tokens {
		k, v, ok := strings.Cut(tok, "=")
		if !ok || k == "" {
			return nil, false
		}
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "note" || k == "detail" {
			out[k] = strings.TrimSpace(strings.Join(append([]string{v}, tokens[i+1:]...), " "))
			break
		}
		out[k] = strings.TrimSpace(v)
	}
	return out, true
}

func (r *Router) handleOutcomeVerb(args []string) {
	if len(args) < 2 {
		r.reply("autonomous outcome failed", outcomeUsage)
		return
	}
	kv, ok := parseOutcomeTokens(args[2:])
	if !ok || kv["outcome"] == "" {
		r.reply("autonomous outcome failed", outcomeUsage)
		return
	}
	body := map[string]any{"outcome": kv["outcome"]}
	if v := kv["via"]; v != "" {
		body["delivered_via"] = v
	}
	if v := kv["note"]; v != "" {
		body["note"] = v
	}
	action := "outcome"
	if a := strings.ToLower(kv["archive"]); a == "true" || a == "1" || a == "on" {
		action = "archive"
	}
	b, _ := json.Marshal(body)
	out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+url.PathEscape(args[1])+"/"+action, string(b))
	if err != nil {
		r.reply("autonomous outcome failed", err.Error())
		return
	}
	r.reply("autonomous outcome", prettyJSON(out))
}

func (r *Router) handleOutcomeCheckVerb(args []string) {
	kv, ok := parseOutcomeTokens(args[1:])
	if !ok {
		r.reply("autonomous outcome-check failed", outcomeCheckUsage)
		return
	}
	var out string
	var err error
	if v := strings.ToLower(kv["run"]); v == "true" || v == "1" || v == "on" {
		out, err = r.commJSON(http.MethodPost, "/api/autonomous/outcome-check", "")
	} else {
		out, err = r.commGet("/api/autonomous/outcome-check", nil)
	}
	if err != nil {
		r.reply("autonomous outcome-check failed", err.Error())
		return
	}
	rep, perr := autonomous.ParseOutcomeCheckBody([]byte(out))
	if perr != nil {
		r.reply("autonomous outcome-check failed", "unparseable read view: "+perr.Error())
		return
	}
	var b strings.Builder
	autonomous.RenderOutcomeCheck(&b, rep)
	r.reply("autonomous outcome-check", strings.TrimRight(b.String(), "\n"))
}

func (r *Router) handleDeployRecordVerb(args []string) {
	kv, ok := parseOutcomeTokens(args[1:])
	if !ok || kv["release"] == "" {
		r.reply("autonomous deploy-record failed", deployRecordUsage)
		return
	}
	body := map[string]any{}
	for _, k := range []string{"release", "version", "host", "health", "detail", "at"} {
		if v := kv[k]; v != "" {
			body[k] = v
		}
	}
	b, _ := json.Marshal(body)
	out, err := r.commJSON(http.MethodPost, "/api/autonomous/deploy/record", string(b))
	if err != nil {
		r.reply("autonomous deploy-record failed", err.Error())
		return
	}
	r.reply("autonomous deploy-record", prettyJSON(out))
}

// cancelOutcomePath builds the DELETE path of a cancel with its optional
// outcome tokens; ok is false for a malformed token.
func cancelOutcomePath(id string, tokens []string) (string, bool) {
	kv, ok := parseOutcomeTokens(tokens)
	if !ok {
		return "", false
	}
	q := url.Values{}
	for k, name := range map[string]string{"outcome": "outcome", "via": "delivered_via", "note": "note"} {
		if v := kv[k]; v != "" {
			q.Set(name, v)
		}
	}
	path := "/api/autonomous/prds/" + id
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	return path, true
}

// cancelPath builds the DELETE path of a chat cancel: the optional outcome
// tokens, then an optional landing decision, "keep <reason>" or
// "drop <reason>", which takes the rest of the line. A decision word inside a
// note is part of the note.
func cancelPath(id string, tokens []string) (string, bool) {
	head := tokens
	var decision, reason string
	for i, tok := range tokens {
		low := strings.ToLower(tok)
		if strings.HasPrefix(low, "note=") {
			break
		}
		if low == "keep" || low == "drop" {
			decision, reason = low, strings.TrimSpace(strings.Join(tokens[i+1:], " "))
			if reason == "" {
				return "", false
			}
			head = tokens[:i]
			break
		}
	}
	path, ok := cancelOutcomePath(id, head)
	if !ok || decision == "" {
		return path, ok
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	q := url.Values{}
	q.Set("decision", decision)
	q.Set("reason", reason)
	return path + sep + q.Encode(), true
}
