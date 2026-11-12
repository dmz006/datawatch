// Sprint Sx2 (v3.7.3) — comm-channel parity for v3.5–v3.7 endpoints.
//
// Each handler proxies the corresponding REST endpoint via the
// in-process HTTP loopback so the full surface is reachable from
// chat. The router gets webPort via SetWebPort at startup; when 0
// the handler reports a clear "loopback not configured" error.

package router

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// commHTTP is the shared HTTP client for in-process REST calls.
//
// The loopback targets 127.0.0.1 on this process's own listeners, and under
// tls_enabled the plain port 302-redirects to the daemon's own (often
// auto-generated, self-signed) TLS port — so certificate verification cannot
// succeed there even in a fully isolated sandbox. The call stays on the host
// and reaches our own process, so skipping verification is the same trust
// posture the smoke/CLI checks already take (curl -sk on the local daemon);
// it changes nothing for a non-TLS loopback.
var commHTTP = &http.Client{
	Timeout: 60 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // 127.0.0.1 loopback to this process's own listener
	},
}

// commTruncationRefusal is the comm channel's share of the spec-write
// truncation refusal. The daemon 400s every spec and description write whose
// text carries the capped-view truncation marker (a copy from a capped read,
// even one edited after the cut), and every comm verb that writes such text
// checks it locally with the daemon's own message before sending, so the
// refusal never depends on the round trip. It checks the title positions
// too: the daemon's 400 covers the spec/description fields a capped view
// cuts, and a title pasted from capped text is the same error, so the comm
// surface is one step stricter than the daemon's field list.
func commTruncationRefusal(verb, field, text string) (reply string, refuse bool) {
	msg, refuse := autonomous.TruncationRefusal(field, text)
	if !refuse || text == "" {
		return "", false
	}
	return "autonomous " + verb + " failed: " + msg, true
}

// commGet calls GET <local>/<path> and returns the body.
func (r *Router) commGet(path string, q url.Values) (string, error) {
	if r.webPort == 0 {
		return "", fmt.Errorf("REST loopback not configured")
	}
	u := fmt.Sprintf("http://127.0.0.1:%d%s", r.webPort, path)
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	return r.commDo(req)
}

// commJSON sends method+jsonBody to the loopback.
func (r *Router) commJSON(method, path, jsonBody string) (string, error) {
	if r.webPort == 0 {
		return "", fmt.Errorf("REST loopback not configured")
	}
	u := fmt.Sprintf("http://127.0.0.1:%d%s", r.webPort, path)
	var rdr io.Reader
	if jsonBody != "" {
		rdr = bytes.NewReader([]byte(jsonBody))
	}
	req, err := http.NewRequest(method, u, rdr)
	if err != nil {
		return "", err
	}
	if jsonBody != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return r.commDo(req)
}

// commDo sends one loopback request and returns the body of a 2xx reply.
// It presents the daemon's bearer token (SetWebToken) on every call: a
// daemon with server.token set answers 401 to a request without it, so
// every comm verb would fail on a token-protected daemon.
func (r *Router) commDo(req *http.Request) (string, error) {
	if r.webToken != "" {
		req.Header.Set("Authorization", "Bearer "+r.webToken)
	}
	resp, err := commHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return string(body), nil
}

// extractGuardrailLLMTokens (BL416) pulls guardrail_backend=<v> /
// guardrail_model=<v> tokens out of set-llm / set-task-llm args so the
// remaining positional parsing is unchanged. Only tokens that appear are
// returned, so "guardrail_model=" clears and an absent token leaves the
// field unchanged.
func extractGuardrailLLMTokens(args []string) ([]string, map[string]string) {
	rest := make([]string, 0, len(args))
	guardrail := map[string]string{}
	for _, a := range args {
		k, v, ok := strings.Cut(a, "=")
		if ok && (k == "guardrail_backend" || k == "guardrail_model") {
			guardrail[k] = v
			continue
		}
		rest = append(rest, a)
	}
	return rest, guardrail
}

// extractLegacySetLLMTokens pulls backend=<v> / effort=<v> / model=<v> /
// decomposition_profile=<v> tokens out of set-llm args so the remaining
// positional parsing is unchanged. Only tokens that appear are returned,
// so "model=" clears and an absent token stays absent.
func extractLegacySetLLMTokens(args []string) ([]string, map[string]string) {
	rest := make([]string, 0, len(args))
	legacy := map[string]string{}
	for _, a := range args {
		k, v, ok := strings.Cut(a, "=")
		if ok && (k == "backend" || k == "effort" || k == "model" || k == "decomposition_profile") {
			legacy[k] = v
			continue
		}
		rest = append(rest, a)
	}
	return rest, legacy
}

// reply sends a tagged response back through the messaging backend.
func (r *Router) reply(label, body string) {
	if body == "" {
		body = "(empty)"
	}
	r.send(fmt.Sprintf("[%s] %s\n%s", r.hostname, label, body))
}

// ----- handlers ------------------------------------------------------------

func (r *Router) handleRest(cmd Command) {
	body, err := r.commJSON(cmd.RestMethod, cmd.RestPath, cmd.RestBody)
	if err != nil {
		r.reply("rest "+cmd.RestMethod+" "+cmd.RestPath+" failed", err.Error())
		return
	}
	r.reply("rest "+cmd.RestMethod+" "+cmd.RestPath, body)
}

func (r *Router) handleCostCmd(cmd Command) {
	q := url.Values{}
	if id := strings.TrimSpace(cmd.Text); id != "" {
		q.Set("session", id)
	}
	body, err := r.commGet("/api/cost", q)
	if err != nil {
		r.reply("cost failed", err.Error())
		return
	}
	r.reply("cost", body)
}

// handleCostReportCmd serves "cost report [group_by=<g>] [since=<t>] [until=<t>]"
// by GET /api/cost/report — the same engine endpoint as the
// `datawatch cost report` CLI and the /api/cost/report REST surface, so the
// report logic lives in exactly one place; the handler only translates the
// chat tokens into query parameters and relays the JSON back.
func (r *Router) handleCostReportCmd(cmd Command) {
	const usage = "usage: cost report [group_by=<automaton|automation|backend|release|week>] [since=<RFC3339|YYYY-MM-DD>] [until=<RFC3339|YYYY-MM-DD>]"
	q := url.Values{}
	for _, tok := range strings.Fields(cmd.Text) {
		k, v, ok := strings.Cut(tok, "=")
		if !ok {
			r.reply("cost report failed", usage)
			return
		}
		switch k {
		case "group_by":
			q.Set("group_by", v)
		case "since":
			q.Set("since", v)
		case "until":
			q.Set("until", v)
		default:
			r.reply("cost report failed", usage)
			return
		}
	}
	body, err := r.commGet("/api/cost/report", q)
	if err != nil {
		r.reply("cost report failed", err.Error())
		return
	}
	if txt := formatCostReportText(body); txt != "" {
		body = txt
	}
	r.reply("cost report", body)
}

func (r *Router) handleCooldown(cmd Command) {
	switch cmd.CooldownVerb {
	case "status", "":
		body, err := r.commGet("/api/cooldown", nil)
		if err != nil {
			r.reply("cooldown status failed", err.Error())
			return
		}
		r.reply("cooldown status", body)
	case "set":
		if cmd.CooldownSeconds <= 0 {
			r.reply("cooldown set failed", "usage: cooldown set <seconds> [reason]")
			return
		}
		until := time.Now().Add(time.Duration(cmd.CooldownSeconds) * time.Second).UnixMilli()
		body := fmt.Sprintf(`{"until_unix_ms":%d,"reason":%q}`, until, cmd.CooldownReason)
		out, err := r.commJSON(http.MethodPost, "/api/cooldown", body)
		if err != nil {
			r.reply("cooldown set failed", err.Error())
			return
		}
		r.reply("cooldown set", out)
	case "clear":
		_, err := r.commJSON(http.MethodDelete, "/api/cooldown", "")
		if err != nil {
			r.reply("cooldown clear failed", err.Error())
			return
		}
		r.reply("cooldown", "cleared")
	default:
		r.reply("cooldown failed", "usage: cooldown [status|set <seconds> [reason]|clear]")
	}
}

// handleUsageBackoff serves "usage_backoff status|set|clear" through the
// /api/usage-backoff loopback, mirroring the cooldown verbs.
func (r *Router) handleUsageBackoff(cmd Command) {
	const usage = "usage: usage_backoff [status | set <backend> <minutes> [reason] | clear [backend|all] | recheck]"
	switch cmd.UsageBackoffVerb {
	case "status", "":
		body, err := r.commGet("/api/usage-backoff", nil)
		if err != nil {
			r.reply("usage_backoff status failed", err.Error())
			return
		}
		r.reply("usage_backoff status", body)
	case "set":
		if cmd.UsageBackoffBackend == "" || (cmd.UsageBackoffMinutes < 1 && cmd.UsageBackoffUntil == "") {
			r.reply("usage_backoff set failed", usage)
			return
		}
		req := map[string]any{"backend": cmd.UsageBackoffBackend}
		if cmd.UsageBackoffUntil != "" {
			req["until"] = cmd.UsageBackoffUntil
		} else {
			req["minutes"] = cmd.UsageBackoffMinutes
		}
		if cmd.UsageBackoffReason != "" {
			req["reason"] = cmd.UsageBackoffReason
		}
		payload, _ := json.Marshal(req)
		out, err := r.commJSON(http.MethodPost, "/api/usage-backoff", string(payload))
		if err != nil {
			r.reply("usage_backoff set failed", err.Error())
			return
		}
		r.reply("usage_backoff set", out)
	case "clear":
		payload := "{}"
		if cmd.UsageBackoffBackend != "" {
			b, _ := json.Marshal(map[string]any{"backend": cmd.UsageBackoffBackend})
			payload = string(b)
		}
		out, err := r.commJSON(http.MethodDelete, "/api/usage-backoff", payload)
		if err != nil {
			r.reply("usage_backoff clear failed", err.Error())
			return
		}
		r.reply("usage_backoff clear", out)
	case "recheck":
		out, err := r.commJSON(http.MethodPost, "/api/usage-backoff/recheck", "{}")
		if err != nil {
			r.reply("usage_backoff recheck failed", err.Error())
			return
		}
		r.reply("usage_backoff recheck", out)
	default:
		r.reply("usage_backoff failed", usage)
	}
}

// handleBudget serves "budget [status] [llm]" through the /api/budget
// loopback and replies with the same text the CLI prints.
func (r *Router) handleBudget(cmd Command) {
	const usage = "usage: budget [status] [<llm>]"
	if cmd.BudgetVerb == "invalid" {
		r.reply("budget failed", usage)
		return
	}
	var q url.Values
	if cmd.BudgetLLM != "" {
		q = url.Values{"llm": {cmd.BudgetLLM}}
	}
	body, err := r.commGet("/api/budget", q)
	if err != nil {
		r.reply("budget status failed", err.Error())
		return
	}
	text, err := autonomous.FormatBudgetText([]byte(body))
	if err != nil {
		r.reply("budget status failed", err.Error())
		return
	}
	r.reply("budget status", strings.TrimRight(text, "\n"))
}

func (r *Router) handleStale(cmd Command) {
	q := url.Values{}
	if s := strings.TrimSpace(cmd.Text); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			q.Set("seconds", strconv.Itoa(n))
		}
	}
	body, err := r.commGet("/api/sessions/stale", q)
	if err != nil {
		r.reply("stale failed", err.Error())
		return
	}
	r.reply("stale", body)
}

// auditCommKeys are the key=value words "audit" accepts — the shared
// filter set every audit surface exposes.
var auditCommKeys = audit.QueryParamNames

// AuditCommKeys returns the key=value words the "audit" command accepts.
func AuditCommKeys() []string { return slices.Clone(auditCommKeys) }

func (r *Router) handleAudit(cmd Command) {
	// "audit actor=<x> action=<y|prefix*> subject=<z> session_id=<s> since=<t> until=<t> limit=<n>"
	// "audit self-config [limit=<n>]" — BL412 self-config audit view
	words := strings.Fields(strings.TrimSpace(cmd.Text))
	if len(words) > 0 && words[0] == "self-config" {
		q := url.Values{}
		for _, kv := range words[1:] {
			if i := strings.IndexByte(kv, '='); i > 0 && kv[:i] == "limit" {
				q.Set("limit", kv[i+1:])
			}
		}
		body, err := r.commGet("/api/audit/self-config", q)
		if err != nil {
			r.reply("self-config audit failed", err.Error())
			return
		}
		r.reply("self-config audit", prettyJSON(body))
		return
	}
	q := url.Values{}
	for _, kv := range words {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			continue
		}
		k, v := kv[:i], kv[i+1:]
		if slices.Contains(auditCommKeys, k) {
			q.Set(k, v)
		}
	}
	if _, err := audit.ParseQuery(q.Get); err != nil {
		r.reply("audit failed", err.Error())
		return
	}
	body, err := r.commGet("/api/audit", q)
	if err != nil {
		// Audit-not-enabled returns 503; tell the operator clearly.
		r.reply("audit failed", err.Error())
		return
	}
	// Pretty-print short output; long JSON gets sent raw.
	if len(body) > 0 {
		var pretty interface{}
		if json.Unmarshal([]byte(body), &pretty) == nil {
			if buf, e := json.MarshalIndent(pretty, "", "  "); e == nil {
				r.reply("audit", string(buf))
				return
			}
		}
	}
	r.reply("audit", body)
}

// handlePeers — BL172 (S11) chat parity for /api/observer/peers/*.
//
//	"peers"                       → list
//	"peers <name>"                → detail
//	"peers <name> stats"          → last snapshot
//	"peers register <name> [shape] [version]"
//	"peers delete <name>"
func (r *Router) handlePeers(cmd Command) {
	args := strings.Fields(strings.TrimSpace(cmd.Text))
	if len(args) == 0 {
		body, err := r.commGet("/api/observer/peers", nil)
		if err != nil {
			r.reply("peers failed", err.Error())
			return
		}
		r.reply("peers", prettyJSON(body))
		return
	}
	switch args[0] {
	case "register":
		if len(args) < 2 {
			r.reply("peers failed", "usage: peers register <name> [shape] [version]")
			return
		}
		body := map[string]any{"name": args[1]}
		if len(args) >= 3 {
			body["shape"] = args[2]
		}
		if len(args) >= 4 {
			body["version"] = args[3]
		}
		raw, _ := json.Marshal(body)
		out, err := r.commJSON(http.MethodPost, "/api/observer/peers", string(raw))
		if err != nil {
			r.reply("peers register failed", err.Error())
			return
		}
		r.reply("peers register", prettyJSON(out))
	case "delete":
		if len(args) < 2 {
			r.reply("peers failed", "usage: peers delete <name>")
			return
		}
		out, err := r.commJSON(http.MethodDelete, "/api/observer/peers/"+args[1], "")
		if err != nil {
			r.reply("peers delete failed", err.Error())
			return
		}
		r.reply("peers delete", prettyJSON(out))
	default:
		// "peers <name>" or "peers <name> stats"
		path := "/api/observer/peers/" + args[0]
		if len(args) >= 2 && args[1] == "stats" {
			path += "/stats"
		}
		out, err := r.commGet(path, nil)
		if err != nil {
			r.reply("peers failed", err.Error())
			return
		}
		r.reply("peers", prettyJSON(out))
	}
}

// prettyJSON pretty-prints body when valid JSON; returns the raw
// string otherwise. Local helper to keep handlePeers terse.
func prettyJSON(body string) string {
	if body == "" {
		return body
	}
	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		return body
	}
	buf, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return body
	}
	return string(buf)
}

// BL197 (v4.9.2) — autonomous PRD lifecycle over chat. Mirrors the
// /api/autonomous/prds/* REST surface so the operator can run PRDs
// from Signal / Telegram without curl. Verbs:
//
//	autonomous status
//	autonomous list
//	autonomous get <prd-id>
//	autonomous decompose <prd-id>
//	autonomous run <prd-id>
//	autonomous cancel <prd-id>
//	autonomous learnings
//	autonomous create <spec…>
//
// `prd` is accepted as a shorter alias for `autonomous`.
// shippedRecordUsage is the comm-channel usage line for recording shipped state.
const shippedRecordUsage = "usage: autonomous shipped-record <id> commit=<commit> [release=<release>] [source=merge_step|backfill] [merged_at=<RFC3339>] [automation=true]"

// shippedBackfillUsage is the comm-channel usage line for the shipped backfill.
const shippedBackfillUsage = "usage: autonomous shipped-backfill [dry_run=true]   (one shipped-state backfill pass from the base repository's main-branch history)"

// autonomousListUsage is the comm-channel usage line for the narrowable PRD
// list.
const autonomousListUsage = "usage: autonomous list [shipped=true|false] [status=<s1,s2,...>]   (no filter = the full list; shipped may appear in status)"

// handleAutonomousShippedVerb serves the shipped-record comm verb for both
// surfaces: it records the shipped state of a PRD (default) or an Automation
// (when automation=true) through their own POST /.../shipped endpoints. Status
// is never touched.
func (r *Router) handleAutonomousShippedVerb(args []string) {
	if len(args) < 2 {
		r.reply("autonomous shipped-record failed", shippedRecordUsage)
		return
	}
	id := args[1]
	body := map[string]any{"source": "backfill"}
	automation := false
	for _, kv := range args[2:] {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			r.reply("autonomous shipped-record failed", shippedRecordUsage)
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
		case "automation":
			automation = strings.EqualFold(v, "true") || v == "1" || strings.EqualFold(v, "on")
		default:
			r.reply("autonomous shipped-record failed", shippedRecordUsage)
			return
		}
	}
	path := "/api/autonomous/prds/" + url.PathEscape(id) + "/shipped"
	if automation {
		path = "/api/automations/" + url.PathEscape(id) + "/shipped"
	}
	b, _ := json.Marshal(body)
	out, err := r.commJSON(http.MethodPost, path, string(b))
	if err != nil {
		r.reply("autonomous shipped-record failed", err.Error())
		return
	}
	r.reply("autonomous shipped-record", prettyJSON(out))
}

// handleAutonomousShippedBackfillVerb serves the shipped-backfill comm
// verb: one shipped-state backfill pass over the base repository's
// main-branch history via POST /api/autonomous/shipped/backfill, replying
// with the report. dry_run=true scans and computes the report without
// writing any shipped record; absent (the default, like every other
// surface) the pass writes.
func (r *Router) handleAutonomousShippedBackfillVerb(args []string) {
	path := "/api/autonomous/shipped/backfill"
	for _, tok := range args[1:] {
		i := strings.IndexByte(tok, '=')
		if i <= 0 {
			r.reply("autonomous shipped-backfill failed", shippedBackfillUsage)
			return
		}
		k := strings.ToLower(strings.TrimSpace(tok[:i]))
		v := strings.TrimSpace(tok[i+1:])
		if k != "dry_run" {
			r.reply("autonomous shipped-backfill failed", shippedBackfillUsage)
			return
		}
		if strings.EqualFold(v, "true") || v == "1" || strings.EqualFold(v, "on") {
			path += "?dry_run=true"
		}
		// false / 0 / off = the default: the pass writes.
	}
	out, err := r.commJSON(http.MethodPost, path, "")
	if err != nil {
		r.reply("autonomous shipped-backfill failed", err.Error())
		return
	}
	r.reply("autonomous shipped-backfill", prettyJSON(out))
}

// handleAutonomousShippedCountsVerb serves the shipped-counts comm verb:
// GET /api/autonomous/shipped/counts — the per-status PRD and Automation
// count buckets computed under the in-force autonomous.shipped settings
// (shipped separated from cancelled while enabled + count_separately hold),
// with the effective settings echoed. Read-only.
func (r *Router) handleAutonomousShippedCountsVerb(args []string) {
	out, err := r.commGet("/api/autonomous/shipped/counts", nil)
	if err != nil {
		r.reply("autonomous shipped-counts failed", err.Error())
		return
	}
	r.reply("autonomous shipped-counts", prettyJSON(out))
}

// handleAutonomousListVerb serves "autonomous list" with optional filters:
// shipped=true|false keeps only PRDs with / without shipped state (a
// non-empty merge commit); status takes comma-separated statuses and may
// include shipped. No filter = the full list (full=1, the original shape).
// Shipped is status-like and orthogonal: it selects on the record's merge
// commit and never changes the stored status.
func (r *Router) handleAutonomousListVerb(args []string) {
	q := url.Values{}
	sawFilter := false
	for _, tok := range args[1:] {
		i := strings.IndexByte(tok, '=')
		if i <= 0 {
			r.reply("autonomous list failed", autonomousListUsage)
			return
		}
		k := strings.ToLower(strings.TrimSpace(tok[:i]))
		v := strings.TrimSpace(tok[i+1:])
		switch k {
		case "shipped":
			if !strings.EqualFold(v, "true") && !strings.EqualFold(v, "false") {
				r.reply("autonomous list failed", autonomousListUsage)
				return
			}
			q.Set("shipped", strings.ToLower(v))
			sawFilter = true
		case "status":
			q.Set("status", v)
			sawFilter = true
		default:
			r.reply("autonomous list failed", autonomousListUsage)
			return
		}
	}
	if !sawFilter {
		q.Set("full", "1")
	}
	out, err := r.commGet("/api/autonomous/prds", q)
	if err != nil {
		r.reply("autonomous list failed", err.Error())
		return
	}
	r.reply("autonomous list", prettyJSON(out))
}

func (r *Router) handleAutonomous(cmd Command) {
	args := strings.Fields(strings.TrimSpace(cmd.Text))
	help := "usage: autonomous {status|list|event-log [kind=K] [project=P] [q=TEXT] [limit=1..10] [before=CURSOR]|config-get|config-set key=value [k2=v2 ...]|judge-policy|judge-policy-set key=value|get <id>|decompose <id>|approve <id>|plan-review <id> [note]|plan-coverage <id>|generated-artefacts [set <key> <on|off>]|change-record <id>|regenerate <id> <kind>|reject <id> [reason]|request-revision <id> [note]|edit-task <prd> <task> [read_only=true|false] [<new-spec>]|set-llm <prd> [backend=X] [effort=Y] [model=Z] [decomposition_profile=P] [<backend> [effort] [model]] [guardrail_backend=X] [guardrail_model=Y]|set-notify-events <prd> <kinds|inherit>|set-task-llm <prd> <task> <backend> [effort] [model] [guardrail_backend=X] [guardrail_model=Y]|set-task-project-dir <prd> <task> [dir]|set-task-node <automaton> <task> <node|clear>|set-story-node <automaton> <story> <node|clear>|set-task-verification-llm <prd> <task> [backend=X] [model=Y] [effort=Z]|set-story-verification-llm <prd> <story> [backend=X] [model=Y] [effort=Z]|set-verification-llm <prd> [backend=X] [model=Y] [effort=Z]|verifier-policy|verifier-policy-set key=value [k2=v2 ...]|set-verifier-policy <prd> [mode=X] [backends=a,b]|set-task-verifier-policy <prd> <task> [mode=X] [backends=a,b]|instantiate <template> [k=v,k=v]|run <id>|cancel <id>|cancel-story <prd> <story> [reason]|cancel-task <prd> <task> [reason]|review-task <automaton> <task> retry|cancel [backend=X] [model=Y] [effort=Z] [files=a,b] [spec=...] [note=...]|set-failure-escalation <automaton> [task] clear|[enabled=bool] [max_failures=N] [steps=backend:model@N,...]|reset-to-draft <id>|resume <id> [note]|set-quality-gates <prd> [enabled=bool] [test_command=cmd] [timeout=N] [block_on_regression=bool] [scope=full|touched-plus-smoke|touched] [smoke_packages=a,b]|scan-results <id>|learnings|children <id>|create <spec>|scan <id>|scan-fix <id>|scan-rules <id>|scan-config [get|set k=v]|types|type-register <id> <label> [description=...] [color=#hex]|set-type <id> <type>|set-kind <id> <feature|bug|other>|lane-fill [limit]|set-lane-preference <prd-id> <gpu|claude|any>|infra-retries [automaton-id] [limit]|onboarding-report [text|json]|merge-shadow|merge-pipeline [show|set-record|clear-record|validate]|triage [automation-id]|checks [automaton-id]|ship [automaton-id]|rules [automaton-id]|rules set <key> <value> [project-dir]|restart-interruptions|decisions [automaton-id]|planning-routing [show|test|scorecard]|decision-records [automaton-id]|decision-record <id>|decision-record create <json>|decision-record withdraw <id> [reason]|repo-ci-watch|supervisor-watch|supervisor jobs|supervisor runs [job=X] [outcome=Y] [limit=N]|supervisor run job=NAME [input=X] [candidates=A,B]|merge|safe-to-delete|cleanup|lint config|lint config-set key=value|lint dry-run <id>|lint findings <id>|uncommitted-work [automaton-id]|unlanded-work [keep|archive-branch|delete-worktree <branch> [confirm]]|protected-paths [automaton-id]|authorize-paths <automaton-id> <path> [<path>...]|authorize-paths <automaton-id> clear|request-watch [session <id>|automaton <id>]|host-load|merged-members [automation-id]|overlap <text>|security-failure [run-id]|tags [counts]|tags sweep [dry-run]|tag-create <name> [parent=<id>] [color=<hex>] [description=<text>]|tag-rename <id> <new-name>|tag-move <id> <parent-id|root>|tag-color <id> <color>|tag-delete <id>|set-tags <prd-id> [tag-id1,tag-id2]|set-primary <prd-id> <tag-id|none>|session-tags <session-id> [tag-id1,tag-id2]|session-tags list|attention|tag-registry|tag-registry-dismiss <note-id>|guided-mode <id> on|off|set-skills <id> <skill1,skill2>|templates|template-get <id>|template-create <title> <spec>|template-update <id> <title> <spec>|template-delete <id>|template-instantiate <id> [dir] [k=v,k=v]|template-clone <prd-id> [desc]|events [queue=<name>] [since=<cursor>]|subscribe <kinds|short-names> [prd=<id>] [task=<id>] [sink=comm|webhook|peer] [target=<t>] [secret_ref=<ref>]|unsubscribe <id>|subscription-update <id> key=value...|subscription-test <id>|subscriptions|requirements-report [text|json]|model-pull-consent-get|model-pull-consent-set model=<name> [deny=<name> ...] [publisher=<p> ...]|branch-refresh <id> enabled|disabled|inherit|branch-refresh-stats|node-versions <name>|standard-tasks}"
	if len(args) == 0 {
		r.reply("autonomous", help)
		return
	}
	verb := strings.ToLower(args[0])
	switch verb {
	case "status":
		out, err := r.commGet("/api/autonomous/status", nil)
		if err != nil {
			r.reply("autonomous status failed", err.Error())
			return
		}
		r.reply("autonomous status", prettyJSON(out))
		r.replyHeldForReview([]byte(out))
	// v8.36.9 — config-get/config-set: the autonomous config surface
	// (verification_backends among others) had REST + MCP but no comm-
	// channel parity at all. SetConfig replaces the whole object, so
	// config-set reads the current config first and merges the
	// requested keys onto it rather than zeroing out every other
	// setting the operator didn't mention.
	case "config-get", "config":
		out, err := r.commGet("/api/autonomous/config", nil)
		if err != nil {
			r.reply("autonomous config-get failed", err.Error())
			return
		}
		r.reply("autonomous config-get", prettyJSON(out))
	case "config-set":
		if len(args) < 2 {
			r.reply("autonomous config-set failed", "usage: autonomous config-set key=value [key2=value2 ...] (list values: key=a,b,c)")
			return
		}
		current, err := r.commGet("/api/autonomous/config", nil)
		if err != nil {
			r.reply("autonomous config-set failed", "reading current config: "+err.Error())
			return
		}
		var cfgMap map[string]any
		if err := json.Unmarshal([]byte(current), &cfgMap); err != nil {
			r.reply("autonomous config-set failed", "parsing current config: "+err.Error())
			return
		}
		listKeys := map[string]bool{
			"verification_backends": true, "per_task_guardrails": true, "per_story_guardrails": true,
			"per_prd_guardrails": true, // BL418
			"judge_policy":       true,
		}
		for _, kv := range args[1:] {
			i := strings.IndexByte(kv, '=')
			if i <= 0 {
				continue
			}
			k := strings.TrimSpace(kv[:i])
			v := strings.TrimSpace(kv[i+1:])
			// scope_lint takes off|warn|enforce. Accept the configure-style
			// autonomous.scope_lint spelling too, and refuse anything else
			// here rather than sending a value the daemon would not store.
			if k == "autonomous.scope_lint" {
				k = "scope_lint"
			}
			if k == "autonomous.priority_by_kind" {
				k = "priority_by_kind"
			}
			if k == "autonomous.defect_intake_automation" {
				k = "defect_intake_automation"
			}
			if k == "scope_lint" {
				if err := config.ValidateScopeLint(v); err != nil {
					r.reply("autonomous config-set failed", err.Error())
					return
				}
			}
			// priority_by_kind — the kind → capacity-priority table (higher
			// values line up closer to the front). Validated with the same shared
			// parser every surface uses: integer values (a non-number, a number
			// with a fraction part and a negative value are all refused with a
			// clear message) and non-empty kind keys; the shipped kind names and
			// an installation's own are all accepted. Accepted as kind=value pairs
			// (e.g. priority_by_kind=bug=90,maintenance=50) or a quoted JSON
			// object; an empty value clears the stored table back to the daemon's
			// defaults; omitting the key leaves it alone. The daemon repeats the
			// same check over the wire object.
			if k == "priority_by_kind" {
				table, perr := autonomous.ParsePriorityByKindValue(v)
				if perr != nil {
					r.reply("autonomous config-set failed", perr.Error())
					return
				}
				cfgMap["priority_by_kind"] = table
				continue
			}
			// defect_intake_automation — the Automation name new kind:bug
			// filings are recorded under. Any string is accepted (each
			// installation chooses its own name); the empty string clears it
			// — the filing is then recorded, not filed into an Automation.
			// The daemon repeats the string check over the wire object.
			if k == "defect_intake_automation" {
				cfgMap[k] = strings.TrimSpace(v)
				continue
			}

			switch {
			case v == "true":
				cfgMap[k] = true
			case v == "false":
				cfgMap[k] = false
			case listKeys[k]:
				cfgMap[k] = strings.Split(v, ",")
			default:
				if n, nerr := strconv.Atoi(v); nerr == nil {
					cfgMap[k] = n
				} else {
					cfgMap[k] = v
				}
			}
		}
		raw, _ := json.Marshal(cfgMap)
		out, err := r.commJSON(http.MethodPut, "/api/autonomous/config", string(raw))
		if err != nil {
			r.reply("autonomous config-set failed", err.Error())
			return
		}
		r.reply("autonomous config-set", prettyJSON(out))
	case "list":
		r.handleAutonomousListVerb(args)
	case "get", "show":
		if len(args) < 2 {
			r.reply("autonomous get failed", "usage: autonomous get <prd-id>")
			return
		}
		out, err := r.commGet("/api/autonomous/prds/"+args[1], nil)
		if err != nil {
			r.reply("autonomous get failed", err.Error())
			return
		}
		reply := prettyJSON(out)
		if lines := rulesReviewOutcomeLines([]byte(out)); len(lines) > 0 {
			reply += "\n" + strings.Join(lines, "\n")
		}
		if line := parallelMetricsStatusLine(out); line != "" {
			reply += "\n" + line
		}
		if lines := taskMergeStatusLines(out); len(lines) > 0 {
			reply += "\n" + strings.Join(lines, "\n")
		}
		if line := reviewStateStatusLine(out); line != "" {
			reply += "\n" + line
		}
		if lines := automatonCostStatusLines(out); len(lines) > 0 {
			reply += "\n" + strings.Join(lines, "\n")
		}
		// Counts-first line per task with a non-empty skipped_snapshot_paths
		// ("left unstaged: <task> N (reason=count, ...)"); the detailed
		// path list stays in the raw JSON above.
		if lines := autonomous.RenderSkippedSnapshotLines([]byte(out)); len(lines) > 0 {
			reply += "\n" + strings.Join(lines, "\n")
		}
		r.reply("autonomous get", reply)
	case "decompose":
		if len(args) < 2 {
			r.reply("autonomous decompose failed", "usage: autonomous decompose <prd-id>")
			return
		}
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/decompose", "")
		if err != nil {
			r.reply("autonomous decompose failed", err.Error())
			return
		}
		r.reply("autonomous decompose", autonomousOutcomeReply(out))
	case "run":
		if len(args) < 2 {
			r.reply("autonomous run failed", "usage: autonomous run <prd-id>")
			return
		}
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/run", "")
		if err != nil {
			r.reply("autonomous run failed", err.Error())
			return
		}
		r.reply("autonomous run", prettyJSON(out))
	case "cancel", "delete":
		if len(args) < 2 {
			r.reply("autonomous cancel failed", "usage: autonomous cancel <prd-id>")
			return
		}
		out, err := r.commJSON(http.MethodDelete, "/api/autonomous/prds/"+args[1], "")
		if err != nil {
			r.reply("autonomous cancel failed", err.Error())
			return
		}
		r.reply("autonomous cancel", prettyJSON(out))
	// BL191 (v5.2.0) — review/approve/reject/edit-task/instantiate.
	case "approve":
		if len(args) < 2 {
			r.reply("autonomous approve failed", "usage: autonomous approve <prd-id> [note]")
			return
		}
		note := strings.TrimSpace(strings.Join(args[2:], " "))
		body, _ := json.Marshal(map[string]string{"actor": "operator", "note": note})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/approve", string(body))
		if err != nil {
			r.reply("autonomous approve failed", err.Error())
			return
		}
		r.reply("autonomous approve", prettyJSON(out))
	// Plan review gate: re-run the pre-approval reviewer judge for a stored
	// PRD (the new verdict replaces the stored one).
	case "plan-review", "plan_review", "run-plan-review":
		if len(args) < 2 {
			r.reply("autonomous plan-review failed", "usage: autonomous plan-review <prd-id> [note]")
			return
		}
		note := strings.TrimSpace(strings.Join(args[2:], " "))
		body, _ := json.Marshal(map[string]string{"actor": "operator", "note": note})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/plan-review", string(body))
		if err != nil {
			r.reply("autonomous plan-review failed", err.Error())
			return
		}
		r.reply("autonomous plan-review", prettyJSON(out))
	// Plan-coverage read: the stored coverage record for one Automaton
	// (mode, min ratio, covered/missing/weakened, findings count, the
	// suppressed created-file and requirement counts, and the
	// acknowledgement) as a compact text summary — the raw JSON stays
	// on the REST/MCP surfaces.
	case "plan-coverage", "plan_coverage", "coverage":
		if len(args) < 2 {
			r.reply("autonomous plan-coverage failed", "usage: autonomous plan-coverage <automaton-id>")
			return
		}
		out, err := r.commJSON(http.MethodGet, "/api/autonomous/prds/"+args[1]+"/plan_coverage", "")
		if err != nil {
			r.reply("autonomous plan-coverage failed", err.Error())
			return
		}
		var envelope struct {
			PlanCoverage *struct {
				Mode     string  `json:"mode"`
				MinRatio float64 `json:"min_ratio"`
				Report   struct {
					Covered  int `json:"covered"`
					Missing  int `json:"missing"`
					Weakened int `json:"weakened"`
				} `json:"report"`
				Findings   []any `json:"findings"`
				Suppressed struct {
					CreatedFiles int `json:"created_files"`
					Requirements int `json:"requirements"`
				} `json:"suppressed"`
				Ack *struct {
					By string `json:"by"`
				} `json:"ack"`
			} `json:"plan_coverage"`
		}
		if err := json.Unmarshal([]byte(out), &envelope); err != nil {
			r.reply("autonomous plan-coverage failed", "parsing the coverage record: "+err.Error())
			return
		}
		if envelope.PlanCoverage == nil {
			r.reply("autonomous plan-coverage", "no plan coverage record is stored yet for this Automaton")
			return
		}
		rec := envelope.PlanCoverage
		lines := []string{
			fmt.Sprintf("mode %s (minimum ratio %g)", rec.Mode, rec.MinRatio),
			fmt.Sprintf("covered %d, missing %d, weakened %d", rec.Report.Covered, rec.Report.Missing, rec.Report.Weakened),
			fmt.Sprintf("findings %d", len(rec.Findings)),
			fmt.Sprintf("suppressed: created files %d, requirements %d", rec.Suppressed.CreatedFiles, rec.Suppressed.Requirements),
		}
		if rec.Ack != nil {
			who := strings.TrimSpace(rec.Ack.By)
			if who == "" {
				who = "the operator"
			}
			lines = append(lines, "acknowledged by "+who)
		} else {
			lines = append(lines, "no acknowledgement yet")
		}
		r.reply("autonomous plan-coverage", strings.Join(lines, "; "))
	case "reject":
		if len(args) < 2 {
			r.reply("autonomous reject failed", "usage: autonomous reject <prd-id> [reason]")
			return
		}
		reason := strings.TrimSpace(strings.Join(args[2:], " "))
		body, _ := json.Marshal(map[string]string{"actor": "operator", "reason": reason})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/reject", string(body))
		if err != nil {
			r.reply("autonomous reject failed", err.Error())
			return
		}
		r.reply("autonomous reject", prettyJSON(out))
	case "request-revision", "request_revision", "revise":
		if len(args) < 2 {
			r.reply("autonomous request-revision failed", "usage: autonomous request-revision <prd-id> [note]")
			return
		}
		note := strings.TrimSpace(strings.Join(args[2:], " "))
		body, _ := json.Marshal(map[string]string{"actor": "operator", "note": note})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/request_revision", string(body))
		if err != nil {
			r.reply("autonomous request-revision failed", err.Error())
			return
		}
		r.reply("autonomous request-revision", prettyJSON(out))
	case "edit-task", "edit_task":
		const editTaskUsage = "usage: autonomous edit-task <prd-id> <task-id> [read_only=true|false] [report_path=<path>] [<new-spec…>]"
		if len(args) < 4 {
			r.reply("autonomous edit-task failed", editTaskUsage)
			return
		}
		rest, readOnly, reportPath, tokErr := takeTaskTokens(args[3:])
		if tokErr != nil {
			r.reply("autonomous edit-task failed", tokErr.Error())
			return
		}
		newSpec := strings.TrimSpace(strings.Join(rest, " "))
		if newSpec == "" && readOnly == nil && reportPath == "" {
			r.reply("autonomous edit-task failed", editTaskUsage)
			return
		}
		if msg, refuse := commTruncationRefusal("edit-task", "new_spec", newSpec); refuse {
			r.reply("autonomous edit-task failed", msg)
			return
		}
		editBody := map[string]any{"task_id": args[2], "actor": "operator"}
		if newSpec != "" {
			editBody["new_spec"] = newSpec
		}
		if readOnly != nil {
			editBody["read_only"] = *readOnly
		}
		if reportPath != "" {
			editBody["report_path"] = reportPath
		}
		body, _ := json.Marshal(editBody)
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/edit_task", string(body))
		if err != nil {
			r.reply("autonomous edit-task failed", err.Error())
			return
		}
		r.reply("autonomous edit-task", prettyJSON(out))
	case "reset-task", "reset_task":
		if len(args) < 3 {
			r.reply("autonomous reset-task failed", "usage: autonomous reset-task <prd-id> <task-id> [clean-restart]")
			return
		}
		// Optional trailing "clean-restart": the next attempt starts from the
		// pre-task tree instead of restoring the preserved attempt work.
		cleanRestart := len(args) > 3 && (args[3] == "clean-restart" || args[3] == "clean_restart")
		body, _ := json.Marshal(map[string]any{"task_id": args[2], "actor": "operator", "clean_restart": cleanRestart})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/reset_task", string(body))
		if err != nil {
			r.reply("autonomous reset-task failed", err.Error())
			return
		}
		r.reply("autonomous reset-task", prettyJSON(out))
	case "edit-story", "edit_story":
		if len(args) < 4 {
			r.reply("autonomous edit-story failed", "usage: autonomous edit-story <prd-id> <story-id> <new-title-or-desc…>")
			return
		}
		rest := strings.TrimSpace(strings.Join(args[3:], " "))
		if msg, refuse := commTruncationRefusal("edit-story", "new_title", rest); refuse {
			r.reply("autonomous edit-story failed", msg)
			return
		}
		body, _ := json.Marshal(map[string]string{"story_id": args[2], "new_title": rest, "actor": "operator"})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/edit_story", string(body))
		if err != nil {
			r.reply("autonomous edit-story failed", err.Error())
			return
		}
		r.reply("autonomous edit-story", prettyJSON(out))
	case "add-story", "add_story":
		if len(args) < 3 {
			r.reply("autonomous add-story failed", "usage: autonomous add-story <prd-id> <title…>")
			return
		}
		title := strings.TrimSpace(strings.Join(args[2:], " "))
		if msg, refuse := commTruncationRefusal("add-story", "title", title); refuse {
			r.reply("autonomous add-story failed", msg)
			return
		}
		body, _ := json.Marshal(map[string]string{"title": title, "actor": "operator"})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/add_story", string(body))
		if err != nil {
			r.reply("autonomous add-story failed", err.Error())
			return
		}
		r.reply("autonomous add-story", prettyJSON(out))
	case "remove-story", "remove_story":
		if len(args) < 3 {
			r.reply("autonomous remove-story failed", "usage: autonomous remove-story <prd-id> <story-id>")
			return
		}
		body, _ := json.Marshal(map[string]string{"story_id": args[2], "actor": "operator"})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/remove_story", string(body))
		if err != nil {
			r.reply("autonomous remove-story failed", err.Error())
			return
		}
		r.reply("autonomous remove-story", prettyJSON(out))
	case "add-task", "add_task":
		const addTaskUsage = "usage: autonomous add-task <prd-id> <story-id> [read_only=true|false] [report_path=<path>] <title…>"
		if len(args) < 4 {
			r.reply("autonomous add-task failed", addTaskUsage)
			return
		}
		rest, readOnly, reportPath, tokErr := takeTaskTokens(args[3:])
		if tokErr != nil {
			r.reply("autonomous add-task failed", tokErr.Error())
			return
		}
		title := strings.TrimSpace(strings.Join(rest, " "))
		if title == "" {
			r.reply("autonomous add-task failed", addTaskUsage)
			return
		}
		if msg, refuse := commTruncationRefusal("add-task", "title", title); refuse {
			r.reply("autonomous add-task failed", msg)
			return
		}
		addBody := map[string]any{"story_id": args[2], "title": title, "actor": "operator"}
		if readOnly != nil && *readOnly {
			addBody["read_only"] = true
		}
		if reportPath != "" {
			addBody["report_path"] = reportPath
		}
		body, _ := json.Marshal(addBody)
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/add_task", string(body))
		if err != nil {
			r.reply("autonomous add-task failed", err.Error())
			return
		}
		r.reply("autonomous add-task", prettyJSON(out))
	case "remove-task", "remove_task":
		if len(args) < 4 {
			r.reply("autonomous remove-task failed", "usage: autonomous remove-task <prd-id> <story-id> <task-id>")
			return
		}
		body, _ := json.Marshal(map[string]string{"story_id": args[2], "task_id": args[3], "actor": "operator"})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/remove_task", string(body))
		if err != nil {
			r.reply("autonomous remove-task failed", err.Error())
			return
		}
		r.reply("autonomous remove-task", prettyJSON(out))
	case "set-llm", "set_llm":
		args, legacy := extractLegacySetLLMTokens(args)
		args, guardrail := extractGuardrailLLMTokens(args)
		if len(args) < 2 || (len(args) < 3 && len(legacy) == 0 && len(guardrail) == 0) {
			r.reply("autonomous set-llm failed", "usage: autonomous set-llm <prd-id> [backend=X] [effort=Y] [model=Z] [decomposition_profile=P] [<backend> [effort] [model]] [guardrail_backend=X] [guardrail_model=Y]")
			return
		}
		body := map[string]string{"actor": "operator"}
		for k, v := range guardrail {
			body[k] = v
		}
		setLegacy := map[string]string{}
		if v, ok := legacy["backend"]; ok {
			setLegacy["backend"] = v
		}
		if v, ok := legacy["effort"]; ok {
			setLegacy["effort"] = v
		}
		if v, ok := legacy["model"]; ok {
			setLegacy["model"] = v
		}
		// Positional args count as explicitly provided and take precedence
		// over the same-named token.
		if len(args) >= 3 {
			setLegacy["backend"] = args[2]
		}
		if len(args) >= 4 {
			setLegacy["effort"] = args[3]
		}
		if len(args) >= 5 {
			setLegacy["model"] = strings.Join(args[4:], " ")
		}
		for k, v := range setLegacy {
			body[k] = v
		}
		// decomposition_profile is token-only — no positional mapping.
		if v, ok := legacy["decomposition_profile"]; ok {
			body["decomposition_profile"] = v
		}
		raw, _ := json.Marshal(body)
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/set_llm", string(raw))
		if err != nil {
			r.reply("autonomous set-llm failed", err.Error())
			return
		}
		r.reply("autonomous set-llm", autonomousOutcomeReply(out))
	case "set-notify-events", "set_notify_events":
		// Per-Automaton notify_events list: which lifecycle event kinds
		// produce notifications. inherit (empty list) falls back to the
		// guardrail profile, then the global default set.
		if len(args) < 3 {
			// The kind lists are derived from internal/autonomous/notify_events.go
			// (the single source of truth) so the usage text can't drift from
			// ValidateNotifyEvents when new kinds land.
			validKinds := strings.Join(autonomous.ValidNotifyEventKinds(), " ")
			optInKinds := strings.Join(autonomous.OptInNotifyEvents(), ", ")
			defSet := strings.Join(autonomous.DefaultNotifyEvents(), ", ")
			usage := "usage: autonomous set-notify-events <prd-id> <kind,kind,...|inherit>"
			usage += "\nvalid kinds: " + validKinds
			usage += "\nopt-in kinds (off by default — inherit does NOT include them): " + optInKinds
			usage += "\ndefault set (used when the Automaton and its guardrail profile both inherit): " + defSet
			r.reply("autonomous set-notify-events failed", usage)
			return
		}
		events := []string{}
		if v := strings.TrimSpace(args[2]); v != "" && !strings.EqualFold(v, "inherit") {
			for _, k := range strings.Split(v, ",") {
				if k = strings.TrimSpace(k); k != "" {
					events = append(events, k)
				}
			}
		}
		raw, _ := json.Marshal(map[string]any{"notify_events": events, "actor": "operator"})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/set_notify_events", string(raw))
		if err != nil {
			r.reply("autonomous set-notify-events failed", err.Error())
			return
		}
		r.reply("autonomous set-notify-events", prettyJSON(out))
	case "set-story-llm", "set_story_llm":
		if len(args) < 4 {
			r.reply("autonomous set-story-llm failed", "usage: autonomous set-story-llm <prd-id> <story-id> <backend> [effort] [model]")
			return
		}
		body := map[string]string{"story_id": args[2], "backend": args[3], "actor": "operator"}
		if len(args) >= 5 {
			body["effort"] = args[4]
		}
		if len(args) >= 6 {
			body["model"] = strings.Join(args[5:], " ")
		}
		raw, _ := json.Marshal(body)
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/set_story_llm", string(raw))
		if err != nil {
			r.reply("autonomous set-story-llm failed", err.Error())
			return
		}
		r.reply("autonomous set-story-llm", autonomousOutcomeReply(out))
	case "set-task-llm", "set_task_llm":
		args, guardrail := extractGuardrailLLMTokens(args)
		if len(args) < 3 || (len(args) < 4 && len(guardrail) == 0) {
			r.reply("autonomous set-task-llm failed", "usage: autonomous set-task-llm <prd-id> <task-id> <backend> [effort] [model] [guardrail_backend=X] [guardrail_model=Y]  |  autonomous set-task-llm <prd-id> <task-id> guardrail_backend=X [guardrail_model=Y]")
			return
		}
		body := map[string]string{"task_id": args[2], "actor": "operator"}
		for k, v := range guardrail {
			body[k] = v
		}
		if len(args) >= 4 {
			body["backend"] = args[3]
		}
		if len(args) >= 5 {
			body["effort"] = args[4]
		}
		if len(args) >= 6 {
			body["model"] = strings.Join(args[5:], " ")
		}
		raw, _ := json.Marshal(body)
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/set_task_llm", string(raw))
		if err != nil {
			r.reply("autonomous set-task-llm failed", err.Error())
			return
		}
		r.reply("autonomous set-task-llm", autonomousOutcomeReply(out))
	case "set-task-node", "set_task_node":
		r.replySetNodePin(args, false)
	case "set-story-node", "set_story_node":
		r.replySetNodePin(args, true)
	case "set-task-project-dir", "set_task_project_dir":
		if len(args) < 3 {
			r.reply("autonomous set-task-project-dir failed", "usage: autonomous set-task-project-dir <prd-id> <task-id> [dir]  (omit dir to clear)")
			return
		}
		body := map[string]string{"task_id": args[2], "project_dir": "", "actor": "operator"}
		if len(args) >= 4 {
			body["project_dir"] = strings.Join(args[3:], " ")
		}
		raw, _ := json.Marshal(body)
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/set_task_project_dir", string(raw))
		if err != nil {
			r.reply("autonomous set-task-project-dir failed", err.Error())
			return
		}
		r.reply("autonomous set-task-project-dir", prettyJSON(out))
	case "set-task-verification-llm", "set_task_verification_llm",
		"set-story-verification-llm", "set_story_verification_llm",
		"set-verification-llm", "set_verification_llm":
		verb := strings.ReplaceAll(args[0], "_", "-")
		route := strings.ReplaceAll(verb, "-", "_")
		idKey, idName := "", ""
		switch verb {
		case "set-task-verification-llm":
			idKey, idName = "task_id", "<task-id> "
		case "set-story-verification-llm":
			idKey, idName = "story_id", "<story-id> "
		}
		usage := "usage: autonomous " + verb + " <prd-id> " + idName + "[backend=X] [model=Y] [effort=Z]  (omitted keys unchanged, key= clears; precedence task > story > Automaton > global verification_backend)"
		need := 2
		if idKey != "" {
			need = 3
		}
		if len(args) < need {
			r.reply("autonomous "+verb+" failed", usage)
			return
		}
		body := map[string]string{"actor": "operator"}
		if idKey != "" {
			body[idKey] = args[2]
		}
		set := 0
		for _, a := range args[need:] {
			k, v, ok := strings.Cut(a, "=")
			if !ok || (k != "backend" && k != "model" && k != "effort") {
				r.reply("autonomous "+verb+" failed", "unrecognized argument "+strconv.Quote(a)+"; "+usage)
				return
			}
			body["verification_"+k] = v
			set++
		}
		if set == 0 {
			r.reply("autonomous "+verb+" failed", usage)
			return
		}
		raw, _ := json.Marshal(body)
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/"+route, string(raw))
		if err != nil {
			r.reply("autonomous "+verb+" failed", err.Error())
			return
		}
		r.reply("autonomous "+verb, prettyJSON(out))
	case "verifier-policy", "verifier_policy", "verifier-policy-set", "verifier_policy_set",
		"set-verifier-policy", "set_verifier_policy", "set-task-verifier-policy", "set_task_verifier_policy":
		r.autonomousVerifierPolicy(args)
	case "judge-policy", "judge_policy", "judge-policy-set", "judge_policy_set":
		r.autonomousJudgePolicy(args)
	case "instantiate":
		if len(args) < 2 {
			r.reply("autonomous instantiate failed", "usage: autonomous instantiate <template-id> [k=v,k=v]")
			return
		}
		vars := map[string]string{}
		if len(args) >= 3 {
			for _, kv := range strings.Split(strings.Join(args[2:], " "), ",") {
				if i := strings.IndexByte(kv, '='); i > 0 {
					vars[strings.TrimSpace(kv[:i])] = strings.TrimSpace(kv[i+1:])
				}
			}
		}
		body, _ := json.Marshal(map[string]any{"vars": vars, "actor": "operator"})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/instantiate", string(body))
		if err != nil {
			r.reply("autonomous instantiate failed", err.Error())
			return
		}
		r.reply("autonomous instantiate", prettyJSON(out))
	case "learnings":
		out, err := r.commGet("/api/autonomous/learnings", nil)
		if err != nil {
			r.reply("autonomous learnings failed", err.Error())
			return
		}
		r.reply("autonomous learnings", prettyJSON(out))
	case "children":
		// BL191 Q4 (v5.9.0) — list child PRDs spawned via SpawnPRD tasks.
		if len(args) < 2 {
			r.reply("autonomous children failed", "usage: autonomous children <prd-id>")
			return
		}
		out, err := r.commGet("/api/autonomous/prds/"+args[1]+"/children", nil)
		if err != nil {
			r.reply("autonomous children failed", err.Error())
			return
		}
		r.reply("autonomous children", prettyJSON(out))
	case "create":
		if len(args) < 2 {
			r.reply("autonomous create failed", "usage: autonomous create <spec>")
			return
		}
		spec := strings.TrimSpace(strings.Join(args[1:], " "))
		if msg, refuse := commTruncationRefusal("create", "spec", spec); refuse {
			r.reply("autonomous create failed", msg)
			return
		}
		body := map[string]any{"spec": spec}
		raw, _ := json.Marshal(body)
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds", string(raw))
		if err != nil {
			r.reply("autonomous create failed", err.Error())
			return
		}
		r.reply("autonomous create", prettyJSON(out))
	// BL221 (v6.2.0) Phase 3 — scan commands
	case "scan":
		if len(args) < 2 {
			r.reply("autonomous scan failed", "usage: autonomous scan <prd-id>")
			return
		}
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/scan", "")
		if err != nil {
			r.reply("autonomous scan failed", err.Error())
			return
		}
		r.reply("autonomous scan", prettyJSON(out))
	case "scan-fix", "scan_fix":
		if len(args) < 2 {
			r.reply("autonomous scan-fix failed", "usage: autonomous scan-fix <prd-id>")
			return
		}
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/scan/fix", "")
		if err != nil {
			r.reply("autonomous scan-fix failed", err.Error())
			return
		}
		r.reply("autonomous scan-fix", prettyJSON(out))
	case "scan-rules", "scan_rules":
		if len(args) < 2 {
			r.reply("autonomous scan-rules failed", "usage: autonomous scan-rules <prd-id>")
			return
		}
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/scan/rules", "")
		if err != nil {
			r.reply("autonomous scan-rules failed", err.Error())
			return
		}
		r.reply("autonomous scan-rules", prettyJSON(out))
	case "scan-config", "scan_config":
		verb2 := ""
		if len(args) >= 2 {
			verb2 = strings.ToLower(args[1])
		}
		if verb2 == "set" && len(args) >= 3 {
			kvBody := map[string]any{}
			for _, kv := range strings.Split(strings.Join(args[2:], " "), ",") {
				if i := strings.IndexByte(kv, '='); i > 0 {
					k := strings.TrimSpace(kv[:i])
					v := strings.TrimSpace(kv[i+1:])
					switch v {
					case "true":
						kvBody[k] = true
					case "false":
						kvBody[k] = false
					default:
						kvBody[k] = v
					}
				}
			}
			raw, _ := json.Marshal(kvBody)
			out, err := r.commJSON(http.MethodPut, "/api/autonomous/scan/config", string(raw))
			if err != nil {
				r.reply("autonomous scan-config set failed", err.Error())
				return
			}
			r.reply("autonomous scan-config set", prettyJSON(out))
		} else {
			out, err := r.commGet("/api/autonomous/scan/config", nil)
			if err != nil {
				r.reply("autonomous scan-config get failed", err.Error())
				return
			}
			r.reply("autonomous scan-config", prettyJSON(out))
		}
	// BL221 (v6.2.0) Phase 4 — type registry, guided mode, skills
	case "types", "type-list", "type_list":
		out, err := r.commGet("/api/autonomous/types", nil)
		if err != nil {
			r.reply("autonomous types failed", err.Error())
			return
		}
		r.reply("autonomous types", prettyJSON(out))
	case "type-register", "type_register":
		// usage: autonomous type-register <id> <label> [description=...] [color=#hex]
		if len(args) < 3 {
			r.reply("autonomous type-register failed", "usage: autonomous type-register <id> <label> [description=...] [color=#hex]")
			return
		}
		payload := map[string]string{"id": args[1], "label": args[2]}
		for _, extra := range args[3:] {
			if kv := strings.SplitN(extra, "=", 2); len(kv) == 2 {
				switch strings.ToLower(kv[0]) {
				case "description", "desc":
					payload["description"] = kv[1]
				case "color":
					payload["color"] = kv[1]
				}
			}
		}
		body, _ := json.Marshal(payload)
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/types", string(body))
		if err != nil {
			r.reply("autonomous type-register failed", err.Error())
			return
		}
		r.reply("autonomous type-register", prettyJSON(out))
	case "set-type", "set_type":
		if len(args) < 3 {
			r.reply("autonomous set-type failed", "usage: autonomous set-type <prd-id> <type>")
			return
		}
		body, _ := json.Marshal(map[string]string{"type": args[2]})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/set_type", string(body))
		if err != nil {
			r.reply("autonomous set-type failed", err.Error())
			return
		}
		r.reply("autonomous set-type", prettyJSON(out))
	case "set-kind", "set_kind":
		const kindUsage = "usage: autonomous set-kind <prd-id> <feature|bug|other>"
		if len(args) < 3 {
			r.reply("autonomous set-kind failed", kindUsage)
			return
		}
		kind, ok := autonomous.NormalizePRDKind(args[2])
		if !ok || kind == "" {
			r.reply("autonomous set-kind failed", "unknown Automaton kind "+strconv.Quote(args[2])+"; "+kindUsage)
			return
		}
		body, _ := json.Marshal(map[string]string{"kind": kind})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/set_kind", string(body))
		if err != nil {
			r.reply("autonomous set-kind failed", err.Error())
			return
		}
		r.reply("autonomous set-kind", prettyJSON(out))
	case "set-release-size", "set_release_size":
		const sizeUsage = "usage: autonomous set-release-size <automaton-id> <patch|minor|major|none|clear>"
		if len(args) < 3 {
			r.reply("autonomous set-release-size failed", sizeUsage)
			return
		}
		size := ""
		if !strings.EqualFold(strings.TrimSpace(args[2]), "clear") {
			v, ok := autonomous.NormalizeReleaseSize(args[2])
			if !ok {
				r.reply("autonomous set-release-size failed", "unknown release size "+strconv.Quote(args[2])+"; "+sizeUsage)
				return
			}
			size = v
		}
		body, _ := json.Marshal(map[string]string{"release_size": size})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/set_release_size", string(body))
		if err != nil {
			r.reply("autonomous set-release-size failed", err.Error())
			return
		}
		r.reply("autonomous set-release-size", prettyJSON(out))
	case "set-breaking-change", "set_breaking_change":
		const breakUsage = "usage: autonomous set-breaking-change <automaton-id> <true|false>"
		if len(args) < 3 {
			r.reply("autonomous set-breaking-change failed", breakUsage)
			return
		}
		breaking, err := strconv.ParseBool(args[2])
		if err != nil {
			r.reply("autonomous set-breaking-change failed", "breaking change must be true or false; "+breakUsage)
			return
		}
		body, _ := json.Marshal(map[string]bool{"breaking_change": breaking})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/set_breaking_change", string(body))
		if err != nil {
			r.reply("autonomous set-breaking-change failed", err.Error())
			return
		}
		r.reply("autonomous set-breaking-change", prettyJSON(out))
	case "shipped", "shipped-record", "shipped_record", "record-shipped", "record_shipped":
		r.handleAutonomousShippedVerb(args)
	case "defect-intake", "defect_intake", "defect-batches", "defect_batches", "defect-split", "defect_split":
		r.handleDefectQueueVerb(verb, args)
	case "shipped-counts", "shipped_counts":
		r.handleAutonomousShippedCountsVerb(args)
	case "shipped-backfill", "shipped_backfill":
		r.handleAutonomousShippedBackfillVerb(args)
	case "lane-fill", "lane_fill", "set-lane-preference", "set_lane_preference":
		r.handleLaneFillVerb(verb, args)
	case "infra-retries", "infra_retries":
		r.replyInfraRetries(args)
	case "onboarding-report", "onboarding_report":
		r.replyOnboardingReport(args)
	case "requirements-report", "requirements_report":
		r.replyRequirementsReport(args)
	case "model-pull-consent-get", "model_pull_consent_get":
		r.replyModelPullConsentGet(args)
	case "model-pull-consent-set", "model_pull_consent_set":
		r.replyModelPullConsentSet(args)
	case "node-versions", "node_versions":
		r.replyNodeBackendVersions(args)
	case "merge-pipeline", "merge_pipeline":
		r.replyMergePipeline(args)
	case "merge-shadow", "merge_shadow":
		r.replyMergeShadow(args)
	case "triage":
		r.handleTriageVerb(verb, args)
	case "restart-interruptions":
		r.handleRestartInterruptionsVerb(args)
	case "checks", "ship":
		r.handleChecksShipVerb(verb, args)
	case "rules":
		r.handleRulesContextVerb(args)
	case "decisions":
		r.handleDecisionsVerb(verb, args)
	case "planning-routing", "planning_routing":
		r.handlePlanningRoutingVerb(args)

	case "decision-records", "decision_records", "decision-record", "decision_record":
		r.handleDecisionRecordsVerb(verb, args)
	case "repo-ci-watch", "repo_ci_watch":
		r.handleRepoCIWatchVerb(verb, args)
	case "supervisor-watch", "supervisor_watch":
		r.handleSupervisorWatchVerb(verb, args)
	case "supervisor", "supervisor-loop", "supervisor_loop":
		r.handleSupervisorLoopVerb(verb, args)
	case "merge", "safe-to-delete", "safe_to_delete":
		r.handleMergeVerb(verb, args)
	case "cleanup":
		r.handleCleanupVerb(verb, args)
	case "generated-artefacts", "generated_artefacts", "change-record", "change_record", "regenerate":
		r.handleGeneratedArtefactsVerb(verb, args)
	case "lint", "decompose-lint", "decompose_lint":
		r.handleDecomposeLintVerb(verb, args)
	case "standard-tasks", "standard_tasks":
		r.replyStandardTasks()
	case "uncommitted-work", "uncommitted_work":
		r.handleUncommittedWorkVerb(verb, args)
	case "unlanded-work", "unlanded_work":
		r.handleUnlandedWorkVerb(verb, args)
	case "request-watch", "request_watch":
		r.handleRequestWatchVerb(verb, args)
	case "protected-paths", "protected_paths", "authorize-paths", "authorize_paths":
		r.handleProtectedPathsVerb(verb, args)
	case "host-load", "host_load":
		r.handleHostLoadVerb(verb, args)
	case "merged-members", "merged_members":
		r.handleMergedMemberVerb(verb, args)
	case "overlap":
		r.handleOverlapVerb(verb, args)
	case "branch-refresh", "branch_refresh":
		r.handleBranchRefreshVerb(verb, args)
	case "branch-refresh-stats", "branch_refresh_stats":
		r.handleBranchRefreshStatsVerb()
	case "security-failure", "security_failure":
		r.handleSecurityFailureVerb(verb, args)
	case "tags", "tag-create", "tag_create", "tag-rename", "tag_rename",
		"tag-move", "tag_move", "tag-color", "tag_color", "tag-delete", "tag_delete",
		"set-tags", "set_tags", "set-primary", "set_primary",
		"session-tags", "session_tags", "attention",
		"tag-registry", "tag_registry", "tag-registry-dismiss", "tag_registry_dismiss":
		r.handleAutonomousTagVerb(verb, args)
	case "guided-mode", "guided_mode":
		if len(args) < 3 {
			r.reply("autonomous guided-mode failed", "usage: autonomous guided-mode <prd-id> on|off")
			return
		}
		guided := strings.ToLower(args[2]) == "on" || strings.ToLower(args[2]) == "true"
		body, _ := json.Marshal(map[string]bool{"guided_mode": guided})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/set_guided_mode", string(body))
		if err != nil {
			r.reply("autonomous guided-mode failed", err.Error())
			return
		}
		r.reply("autonomous guided-mode", prettyJSON(out))
	case "set-skills", "set_skills":
		if len(args) < 3 {
			r.reply("autonomous set-skills failed", "usage: autonomous set-skills <prd-id> <skill1,skill2,...>")
			return
		}
		var skills []string
		for _, s := range strings.Split(strings.Join(args[2:], " "), ",") {
			if s = strings.TrimSpace(s); s != "" {
				skills = append(skills, s)
			}
		}
		raw, _ := json.Marshal(map[string]any{"skills": skills})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/set_skills", string(raw))
		if err != nil {
			r.reply("autonomous set-skills failed", err.Error())
			return
		}
		r.reply("autonomous set-skills", prettyJSON(out))

	// BL221 (v6.2.0) Phase 5 — template store CRUD.
	case "templates", "template-list", "template_list":
		out, err := r.commGet("/api/autonomous/templates", nil)
		if err != nil {
			r.reply("autonomous templates failed", err.Error())
			return
		}
		r.reply("autonomous templates", prettyJSON(out))

	case "template-get", "template_get":
		if len(args) < 2 {
			r.reply("autonomous template-get", "usage: autonomous template-get <id>")
			return
		}
		out, err := r.commGet("/api/autonomous/templates/"+args[1], nil)
		if err != nil {
			r.reply("autonomous template-get failed", err.Error())
			return
		}
		r.reply("autonomous template-get", prettyJSON(out))

	case "template-create", "template_create":
		// usage: autonomous template-create <title> <spec…>
		if len(args) < 3 {
			r.reply("autonomous template-create", "usage: autonomous template-create <title> <spec…>")
			return
		}
		spec := strings.Join(args[2:], " ")
		if msg, refuse := commTruncationRefusal("template-create", "spec", spec); refuse {
			r.reply("autonomous template-create failed", msg)
			return
		}
		raw, _ := json.Marshal(map[string]any{"title": args[1], "spec": spec})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/templates", string(raw))
		if err != nil {
			r.reply("autonomous template-create failed", err.Error())
			return
		}
		r.reply("autonomous template-create", prettyJSON(out))

	case "template-update", "template_update":
		// usage: autonomous template-update <id> <title> <spec…>
		if len(args) < 4 {
			r.reply("autonomous template-update", "usage: autonomous template-update <id> <title> <spec…>")
			return
		}
		spec := strings.Join(args[3:], " ")
		if msg, refuse := commTruncationRefusal("template-update", "spec", spec); refuse {
			r.reply("autonomous template-update failed", msg)
			return
		}
		raw, _ := json.Marshal(map[string]any{"title": args[2], "spec": spec})
		out, err := r.commJSON(http.MethodPut, "/api/autonomous/templates/"+args[1], string(raw))
		if err != nil {
			r.reply("autonomous template-update failed", err.Error())
			return
		}
		r.reply("autonomous template-update", prettyJSON(out))

	case "template-delete", "template_delete":
		if len(args) < 2 {
			r.reply("autonomous template-delete", "usage: autonomous template-delete <id>")
			return
		}
		out, err := r.commJSON(http.MethodDelete, "/api/autonomous/templates/"+args[1], "")
		if err != nil {
			r.reply("autonomous template-delete failed", err.Error())
			return
		}
		r.reply("autonomous template-delete", prettyJSON(out))

	case "template-instantiate", "template_instantiate":
		// usage: autonomous template-instantiate <id> [project_dir] [k=v,k=v]
		if len(args) < 2 {
			r.reply("autonomous template-instantiate", "usage: autonomous template-instantiate <id> [project_dir] [k=v,k=v]")
			return
		}
		projectDir := ""
		vars := map[string]string{}
		if len(args) >= 3 {
			projectDir = args[2]
		}
		if len(args) >= 4 {
			for _, kv := range strings.Split(args[3], ",") {
				if i := strings.IndexByte(kv, '='); i > 0 {
					vars[strings.TrimSpace(kv[:i])] = strings.TrimSpace(kv[i+1:])
				}
			}
		}
		raw, _ := json.Marshal(map[string]any{"project_dir": projectDir, "vars": vars, "actor": "comm"})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/templates/"+args[1]+"/instantiate", string(raw))
		if err != nil {
			r.reply("autonomous template-instantiate failed", err.Error())
			return
		}
		r.reply("autonomous template-instantiate", prettyJSON(out))

	case "template-clone", "template_clone":
		// usage: autonomous template-clone <prd-id> [description]
		if len(args) < 2 {
			r.reply("autonomous template-clone", "usage: autonomous template-clone <prd-id> [description]")
			return
		}
		desc := ""
		if len(args) >= 3 {
			desc = strings.Join(args[2:], " ")
		}
		raw, _ := json.Marshal(map[string]string{"description": desc, "actor": "comm"})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/clone_to_template", string(raw))
		if err != nil {
			r.reply("autonomous template-clone failed", err.Error())
			return
		}
		r.reply("autonomous template-clone", prettyJSON(out))

	// BL382 (v8.27.0) — B88 comm-channel parity gap fixes.
	case "cancel-story", "cancel_story":
		if len(args) < 3 {
			r.reply("autonomous cancel-story failed", "usage: autonomous cancel-story <prd-id> <story-id> [reason]")
			return
		}
		reason := strings.TrimSpace(strings.Join(args[3:], " "))
		raw, _ := json.Marshal(map[string]string{"story_id": args[2], "reason": reason, "actor": "operator"})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/cancel_story", string(raw))
		if err != nil {
			r.reply("autonomous cancel-story failed", err.Error())
			return
		}
		r.reply("autonomous cancel-story", prettyJSON(out))

	case "set-failure-escalation", "set_failure_escalation":
		r.handleAutonomousSetFailureEscalation(args[1:])
	case "review-task", "review_task":
		r.handleAutonomousReviewTask(args[1:])
	case "cancel-task", "cancel_task":
		if len(args) < 3 {
			r.reply("autonomous cancel-task failed", "usage: autonomous cancel-task <prd-id> <task-id> [reason]")
			return
		}
		reason := strings.TrimSpace(strings.Join(args[3:], " "))
		raw, _ := json.Marshal(map[string]string{"task_id": args[2], "reason": reason, "actor": "operator"})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/cancel_task", string(raw))
		if err != nil {
			r.reply("autonomous cancel-task failed", err.Error())
			return
		}
		r.reply("autonomous cancel-task", prettyJSON(out))

	// BL406 Phase 5 — per-task approval gate (guided_mode_source="operator").
	case "approve-task", "approve_task":
		if len(args) < 3 {
			r.reply("autonomous approve-task failed", "usage: autonomous approve-task <prd-id> <task-id>")
			return
		}
		raw, _ := json.Marshal(map[string]string{"task_id": args[2], "actor": "operator"})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/approve_task", string(raw))
		if err != nil {
			r.reply("autonomous approve-task failed", err.Error())
			return
		}
		r.reply("autonomous approve-task", prettyJSON(out))

	// Resume a PRD the boot-resume gate parked (or a paused one).
	case "resume":
		if len(args) < 2 {
			r.reply("autonomous resume failed", "usage: autonomous resume <prd-id> [note]")
			return
		}
		note := strings.TrimSpace(strings.Join(args[2:], " "))
		raw, _ := json.Marshal(map[string]string{"actor": "operator", "note": note})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/resume", string(raw))
		if err != nil {
			r.reply("autonomous resume failed", err.Error())
			return
		}
		r.reply("autonomous resume", prettyJSON(out))

	case "reset-to-draft", "reset_to_draft":
		if len(args) < 2 {
			r.reply("autonomous reset-to-draft failed", "usage: autonomous reset-to-draft <prd-id>")
			return
		}
		raw, _ := json.Marshal(map[string]string{"actor": "operator"})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/reset_to_draft", string(raw))
		if err != nil {
			r.reply("autonomous reset-to-draft failed", err.Error())
			return
		}
		r.reply("autonomous reset-to-draft", prettyJSON(out))

	case "set-quality-gates", "set_quality_gates":
		// usage: autonomous set-quality-gates <prd-id> [enabled=bool] [test_command=cmd] [timeout=N] [block_on_regression=bool] [scope=full|touched-plus-smoke|touched] [smoke_packages=a,b]
		if len(args) < 2 {
			r.reply("autonomous set-quality-gates failed", "usage: autonomous set-quality-gates <prd-id> [enabled=bool] [test_command=cmd] [timeout=N] [block_on_regression=bool] [scope=full|touched-plus-smoke|touched] [smoke_packages=a,b]")
			return
		}
		payload := map[string]any{}
		for _, kv := range args[2:] {
			if i := strings.IndexByte(kv, '='); i > 0 {
				k, v := kv[:i], kv[i+1:]
				switch strings.ToLower(k) {
				case "enabled":
					payload["enabled"] = v == "true" || v == "1"
				case "test_command":
					payload["test_command"] = v
				case "timeout":
					if n, err2 := strconv.Atoi(v); err2 == nil {
						payload["timeout"] = n
					}
				case "block_on_regression":
					payload["block_on_regression"] = v == "true" || v == "1"
				case "scope":
					payload["scope"] = v
				case "smoke_packages":
					parts := []string{}
					for _, p := range strings.Split(v, ",") {
						if p = strings.TrimSpace(p); p != "" {
							parts = append(parts, p)
						}
					}
					payload["smoke_packages"] = parts
				}
			}
		}
		raw, _ := json.Marshal(payload)
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/set_quality_gates", string(raw))
		if err != nil {
			r.reply("autonomous set-quality-gates failed", err.Error())
			return
		}
		r.reply("autonomous set-quality-gates", prettyJSON(out))

	case "scan-results", "scan_results":
		if len(args) < 2 {
			r.reply("autonomous scan-results failed", "usage: autonomous scan-results <prd-id>")
			return
		}
		out, err := r.commGet("/api/autonomous/prds/"+args[1]+"/scan", nil)
		if err != nil {
			r.reply("autonomous scan-results failed", err.Error())
			return
		}
		r.reply("autonomous scan-results", prettyJSON(out))

	// BL427 (phase 3) — Automaton lifecycle-event subscriptions: comm-sink
	// records owned by (and delivering to) the calling channel, scoped by
	// owner so a channel can only list or delete its own subscriptions.
	// The daemon owns the store/engine/REST (internal/autonomous +
	// internal/server/autonomous_subscriptions.go); these verbs proxy it.
	case "subscribe":
		r.handleAutonomousSubsSubscribe(args[1:])
	case "unsubscribe":
		r.handleAutonomousSubsUnsubscribe(args[1:])
	case "subscriptions":
		r.handleAutonomousSubsList()
	case "subscription-update":
		r.handleAutonomousSubsUpdate(args[1:])
	case "subscription-test":
		r.handleAutonomousSubsTest(args[1:])
	case "events":
		r.handleAutonomousEvents(args[1:])
	case "event-log", "event_log":
		r.handleAutonomousEventLog(args[1:])

	default:
		r.reply("autonomous", "unknown verb "+verb+"\n"+help)
	}
}

// handleMCPResCmd handles the !mcp resources / !mcp read / !mcp templates / !mcp sample
// chat commands (BL302 S1 + S3).
func (r *Router) handleMCPResCmd(cmd Command) {
	lower := strings.ToLower(strings.TrimSpace(cmd.Text))
	switch {
	case lower == "" || lower == "resources" || lower == "list":
		body, err := r.commGet("/api/mcp/resources", nil)
		if err != nil {
			r.reply("mcp resources failed", err.Error())
			return
		}
		r.reply("mcp resources", body)
	case lower == "templates":
		body, err := r.commGet("/api/mcp/resources/templates", nil)
		if err != nil {
			r.reply("mcp templates failed", err.Error())
			return
		}
		r.reply("mcp templates", body)
	case strings.HasPrefix(lower, "read "):
		uri := strings.TrimSpace(cmd.Text[5:])
		q := url.Values{"uri": {uri}}
		body, err := r.commGet("/api/mcp/resources/read", q)
		if err != nil {
			r.reply("mcp read failed", err.Error())
			return
		}
		r.reply("mcp read "+uri, body)
	case strings.HasPrefix(lower, "sample "):
		// BL302 S3 — !mcp sample <message> → POST /api/mcp/sample
		message := strings.TrimSpace(cmd.Text[7:])
		if message == "" {
			r.reply("mcp sample", "usage: mcp sample <message>")
			return
		}
		jsonPayload, _ := json.Marshal(map[string]any{
			"trigger":  "comm",
			"messages": []map[string]string{{"role": "user", "content": message}},
		})
		body, err := r.commJSON("POST", "/api/mcp/sample", string(jsonPayload))
		if err != nil {
			r.reply("mcp sample failed", err.Error())
			return
		}
		r.reply("mcp sample", body)
	case lower == "prompts" || lower == "prompt list":
		// BL302 S4 — !mcp prompts → GET /api/mcp/prompts
		body, err := r.commGet("/api/mcp/prompts", nil)
		if err != nil {
			r.reply("mcp prompts failed", err.Error())
			return
		}
		r.reply("mcp prompts", body)
	case strings.HasPrefix(lower, "prompt "):
		// BL302 S4 — !mcp prompt <name> [key=val...] → POST /api/mcp/prompts/get
		rest := strings.TrimSpace(cmd.Text[7:])
		parts := strings.Fields(rest)
		if len(parts) == 0 {
			r.reply("mcp prompt", "usage: mcp prompt <name> [key=val...]")
			return
		}
		name := parts[0]
		argMap := map[string]string{}
		for _, kv := range parts[1:] {
			idx := strings.IndexByte(kv, '=')
			if idx > 0 {
				argMap[kv[:idx]] = kv[idx+1:]
			}
		}
		jsonPayload, _ := json.Marshal(map[string]any{
			"name":      name,
			"arguments": argMap,
		})
		body, err := r.commJSON("POST", "/api/mcp/prompts/get", string(jsonPayload))
		if err != nil {
			r.reply("mcp prompt failed", err.Error())
			return
		}
		r.reply("mcp prompt "+name, body)
	default:
		r.reply("mcp", "usage: mcp resources | mcp templates | mcp read <uri> | mcp sample <message> | mcp prompts | mcp prompt <name> [key=val...]")
	}
}

// takeReadOnlyToken strips a leading read_only=<bool> token from args and
// returns the remaining words plus the parsed flag (nil when absent).
func takeReadOnlyToken(args []string) ([]string, *bool, error) {
	rest, ro, _, err := takeTaskTokens(args)
	return rest, ro, err
}

// takeTaskTokens strips leading read_only=<bool> and report_path=<path>
// tokens (in either order) from args and returns the remaining words plus the
// parsed values (flag nil / path empty when absent). Any other word ends the
// token run and starts the positional text.
func takeTaskTokens(args []string) ([]string, *bool, string, error) {
	var ro *bool
	var reportPath string
tokens:
	for len(args) > 0 {
		k, v, ok := strings.Cut(args[0], "=")
		if !ok {
			break
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		switch k {
		case "read_only", "read-only":
			b, err := strconv.ParseBool(v)
			if err != nil {
				return nil, nil, "", fmt.Errorf("read_only must be true or false, got %q", v)
			}
			ro = &b
		case "report_path":
			reportPath = v
		default:
			break tokens
		}
		args = args[1:]
	}
	return args, ro, reportPath, nil
}
