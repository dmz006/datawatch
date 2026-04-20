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
