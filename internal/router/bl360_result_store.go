package router

// BL360 — structured agent result store comm channel handler.
//
// Commands:
//   result put name=<n> payload=<json> [ttl=<sec>]
//   result get name=<n>
//   result list [prefix=<p>]
//   result delete name=<n>

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// handleResultCmd dispatches result commands over the comm channel (BL360).
func (r *Router) handleResultCmd(cmd Command) {
	switch cmd.ResultVerb {
	case "put":
		r.handleResultPutCmd(cmd)
	case "get":
		r.handleResultGetCmd(cmd)
	case "list", "":
		r.handleResultListCmd(cmd)
	case "delete", "del":
		r.handleResultDeleteCmd(cmd)
	default:
		r.send(fmt.Sprintf("[result] unknown verb %q. Use: put | get | list | delete", cmd.ResultVerb))
	}
}

func (r *Router) handleResultPutCmd(cmd Command) {
	if cmd.ResultName == "" {
		r.send("[result] put requires name=<name>")
		return
	}
	payload := cmd.ResultPayload
	if payload == "" {
		payload = "{}"
	}
	body, _ := json.Marshal(map[string]any{
		"name":        cmd.ResultName,
		"payload":     json.RawMessage(payload),
		"ttl_seconds": cmd.ResultTTL,
	})
	out, err := r.commJSON("POST", "/api/result-store", string(body))
	if err != nil {
		r.send(fmt.Sprintf("[result] put failed: %v", err))
		return
	}
	var entry map[string]any
	if json.Unmarshal([]byte(out), &entry) == nil {
		r.send(fmt.Sprintf("[result] stored %q", cmd.ResultName))
		return
	}
	r.send("[result] stored: " + strings.TrimSpace(out))
}

func (r *Router) handleResultGetCmd(cmd Command) {
	if cmd.ResultName == "" {
		r.send("[result] get requires name=<name>")
		return
	}
	out, err := r.commGet("/api/result-store/"+url.PathEscape(cmd.ResultName), nil)
	if err != nil {
		r.send(fmt.Sprintf("[result] get failed: %v", err))
		return
	}
	r.send("[result] " + strings.TrimSpace(out))
}

func (r *Router) handleResultListCmd(cmd Command) {
	q := url.Values{}
	if cmd.ResultPrefix != "" {
		q.Set("prefix", cmd.ResultPrefix)
	}
	out, err := r.commGet("/api/result-store", q)
	if err != nil {
		r.send(fmt.Sprintf("[result] list failed: %v", err))
		return
	}
	var entries []map[string]any
	if json.Unmarshal([]byte(out), &entries) != nil || len(entries) == 0 {
		r.send("[result] no entries found.")
		return
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "[result] %d entry(s):\n", len(entries))
	for _, e := range entries {
		name, _ := e["name"].(string)
		exp, _ := e["expires_at"].(string)
		if exp != "" {
			fmt.Fprintf(&sb, "  %-40s  expires=%s\n", name, exp)
		} else {
			fmt.Fprintf(&sb, "  %s\n", name)
		}
	}
	r.send(sb.String())
}

func (r *Router) handleResultDeleteCmd(cmd Command) {
	if cmd.ResultName == "" {
		r.send("[result] delete requires name=<name>")
		return
	}
	_, err := r.commJSON("DELETE", "/api/result-store/"+url.PathEscape(cmd.ResultName), "")
	if err != nil {
		r.send(fmt.Sprintf("[result] delete failed: %v", err))
		return
	}
	r.send("[result] deleted: " + cmd.ResultName)
}
