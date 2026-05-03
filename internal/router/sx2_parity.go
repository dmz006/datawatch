// Sprint Sx2 (v3.7.3) — comm-channel parity for v3.5–v3.7 endpoints.
//
// Each handler proxies the corresponding REST endpoint via the
// in-process HTTP loopback so the full surface is reachable from
// chat. The router gets webPort via SetWebPort at startup; when 0
// the handler reports a clear "loopback not configured" error.

package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// commHTTP is the shared HTTP client for in-process REST calls.
var commHTTP = &http.Client{Timeout: 60 * time.Second}

// commGet calls GET <local>/<path> and returns the body.
func (r *Router) commGet(path string, q url.Values) (string, error) {
	if r.webPort == 0 {
		return "", fmt.Errorf("REST loopback not configured")
	}
	u := fmt.Sprintf("http://127.0.0.1:%d%s", r.webPort, path)
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	resp, err := commHTTP.Get(u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return string(body), nil
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
	resp, err := commHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return string(body), nil
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

func (r *Router) handleAudit(cmd Command) {
	// Allow "audit actor=<x> action=<y> session_id=<z> limit=<n>"
	q := url.Values{}
	for _, kv := range strings.Fields(cmd.Text) {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			continue
		}
		k, v := kv[:i], kv[i+1:]
		switch k {
		case "actor", "action", "session_id", "since", "until", "limit":
			q.Set(k, v)
		}
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
//   "peers"                       → list
//   "peers <name>"                → detail
//   "peers <name> stats"          → last snapshot
//   "peers register <name> [shape] [version]"
//   "peers delete <name>"
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
func (r *Router) handleAutonomous(cmd Command) {
	args := strings.Fields(strings.TrimSpace(cmd.Text))
	help := "usage: autonomous {status|list|get <id>|decompose <id>|approve <id>|reject <id> [reason]|request-revision <id> [note]|edit-task <prd> <task> <new-spec>|set-llm <prd> <backend> [effort] [model]|set-task-llm <prd> <task> <backend> [effort] [model]|instantiate <template> [k=v,k=v]|run <id>|cancel <id>|learnings|children <id>|create <spec>|scan <id>|scan-fix <id>|scan-rules <id>|scan-config [get|set k=v]|types|set-type <id> <type>|guided-mode <id> on|off|set-skills <id> <skill1,skill2>|templates|template-get <id>|template-create <title> <spec>|template-update <id> <title> <spec>|template-delete <id>|template-instantiate <id> [dir] [k=v,k=v]|template-clone <prd-id> [desc]}"
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
	case "list":
		out, err := r.commGet("/api/autonomous/prds", nil)
		if err != nil {
			r.reply("autonomous list failed", err.Error())
			return
		}
		r.reply("autonomous list", prettyJSON(out))
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
		r.reply("autonomous get", prettyJSON(out))
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
		r.reply("autonomous decompose", prettyJSON(out))
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
		if len(args) < 4 {
			r.reply("autonomous edit-task failed", "usage: autonomous edit-task <prd-id> <task-id> <new-spec…>")
			return
		}
		newSpec := strings.TrimSpace(strings.Join(args[3:], " "))
		body, _ := json.Marshal(map[string]string{"task_id": args[2], "new_spec": newSpec, "actor": "operator"})
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/edit_task", string(body))
		if err != nil {
			r.reply("autonomous edit-task failed", err.Error())
			return
		}
		r.reply("autonomous edit-task", prettyJSON(out))
	case "set-llm", "set_llm":
		if len(args) < 3 {
			r.reply("autonomous set-llm failed", "usage: autonomous set-llm <prd-id> <backend> [effort] [model]")
			return
		}
		body := map[string]string{"backend": args[2], "actor": "operator"}
		if len(args) >= 4 {
			body["effort"] = args[3]
		}
		if len(args) >= 5 {
			body["model"] = strings.Join(args[4:], " ")
		}
		raw, _ := json.Marshal(body)
		out, err := r.commJSON(http.MethodPost, "/api/autonomous/prds/"+args[1]+"/set_llm", string(raw))
		if err != nil {
			r.reply("autonomous set-llm failed", err.Error())
			return
		}
		r.reply("autonomous set-llm", prettyJSON(out))
	case "set-task-llm", "set_task_llm":
		if len(args) < 4 {
			r.reply("autonomous set-task-llm failed", "usage: autonomous set-task-llm <prd-id> <task-id> <backend> [effort] [model]")
			return
		}
		body := map[string]string{"task_id": args[2], "backend": args[3], "actor": "operator"}
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
		r.reply("autonomous set-task-llm", prettyJSON(out))
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
					if v == "true" {
						kvBody[k] = true
					} else if v == "false" {
						kvBody[k] = false
					} else {
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
		raw, _ := json.Marshal(map[string]any{"title": args[1], "spec": strings.Join(args[2:], " ")})
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
		raw, _ := json.Marshal(map[string]any{"title": args[2], "spec": strings.Join(args[3:], " ")})
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

	default:
		r.reply("autonomous", "unknown verb "+verb+"\n"+help)
	}
}
