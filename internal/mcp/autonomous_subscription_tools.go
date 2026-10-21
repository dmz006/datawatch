// BL427 (BL430 Phase 5) — MCP tools for Automaton lifecycle-event
// subscriptions. The daemon side already exists (BL427 Phases 1–3,
// design: docs/plans/2026-10-13-prd-lifecycle-events.md):
//
//	internal/autonomous/lifecycle_subscription.go     Subscription shape + validation
//	internal/autonomous/lifecycle_subscription_store.go  json-backed store
//	internal/server/autonomous_subscriptions.go       REST routes
//
// These tools proxy those REST routes on the loopback exactly like the
// BL331 channel routing tools (bl331_channel_routing_tools.go) — the MCP
// server holds no subscription state of its own; the daemon's dispatcher
// is the source of truth:
//
//	POST   /api/autonomous/subscriptions            → autonomous_subscribe
//	DELETE /api/autonomous/subscriptions/{id}       → autonomous_unsubscribe
//	GET    /api/autonomous/subscriptions[?owner=]   → autonomous_subscriptions
//
// The same surface over CLI: `datawatch autonomous subscribe`,
// `datawatch autonomous unsubscribe <id>` and
// `datawatch autonomous subscriptions` (cmd/datawatch/cli_autonomous_subscriptions.go).
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// autonomousSubscribePath is the REST route all three tools proxy.
const autonomousSubscribePath = "/api/autonomous/subscriptions"

// subscriptionSinkTypes mirrors autonomous.SinkComm/SinkSession/SinkTopic/
// SinkAlert (the types accepted in the current phase) so tool descriptions
// stay in sync with ValidateSubscription.
var subscriptionSinkTypes = []string{"comm", "session", "topic", "alert"}

// --- Tool definitions ------------------------------------------------------

// toolAutonomousSubscribe registers a persisted lifecycle-event
// subscription on the daemon.
func (s *Server) toolAutonomousSubscribe() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_subscribe",
		mcpsdk.WithDescription("Register a persisted subscription to Automaton (PRD/task) lifecycle events on this daemon so matching events (task.session_started, task.completed, prd.blocked, ...) are delivered to a chosen sink when they fire. kinds: comma-separated event kinds (at least one of kinds or level is required). level: \"prd\" or \"task\". prd_id/task_id: scope to one Automaton/task (omitted = any). sink_type + sink_target: comm|session|topic|alert plus its target (session id, topic, router/alert name). action_text (optional, requires sink_type \"session\"): text typed into the target session on every match (the send_input action — the only action in this phase). one_shot: delete after the first delivery. coalesce: batch human-facing sinks in quiet windows. expires_in_seconds: optional TTL. Returns the stored subscription record (with its id) as JSON. The CLI equivalent is `datawatch autonomous subscribe`."),
		mcpsdk.WithString("kinds",
			mcpsdk.Description("Comma-separated event kinds, e.g. \"task.session_started,prd.blocked\". A kind may also be passed as a JSON string array. At least one of kinds or level is required."),
		),
		mcpsdk.WithString("level",
			mcpsdk.Description("Event level to match: \"prd\" or \"task\". At least one of kinds or level is required."),
		),
		mcpsdk.WithString("prd_id",
			mcpsdk.Description("Only match events for this Automaton (PRD) id; omitted = any Automaton."),
		),
		mcpsdk.WithString("task_id",
			mcpsdk.Description("Only match events for this task id; omitted = any task."),
		),
		mcpsdk.WithString("sink_type",
			mcpsdk.Required(),
			mcpsdk.Description("Sink type: \"comm\", \"session\", \"topic\" or \"alert\"."),
		),
		mcpsdk.WithString("sink_target",
			mcpsdk.Required(),
			mcpsdk.Description("Sink target (session id / topic / router or alert name)."),
		),
		mcpsdk.WithString("action_text",
			mcpsdk.Description("Optional text typed into the sink session when the event fires (requires sink_type \"session\"); the only action supported in this phase is the send_input action."),
		),
		mcpsdk.WithBoolean("one_shot",
			mcpsdk.Description("Delete the subscription after its first delivery. Default false."),
		),
		mcpsdk.WithBoolean("coalesce",
			mcpsdk.Description("Coalesce human-facing sink deliveries in quiet windows. Default false."),
		),
		mcpsdk.WithNumber("expires_in_seconds",
			mcpsdk.Description("Optional TTL in seconds; the subscription stops matching after it elapses. Must be positive."),
		),
	)
}

// toolAutonomousUnsubscribe removes one subscription by id.
func (s *Server) toolAutonomousUnsubscribe() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_unsubscribe",
		mcpsdk.WithDescription("Remove a lifecycle-event subscription by id (the id from autonomous_subscribe / autonomous_subscriptions). Proxies DELETE /api/autonomous/subscriptions/{id}; 404 for unknown ids. The CLI equivalent is `datawatch autonomous unsubscribe <id>`."),
		mcpsdk.WithString("id",
			mcpsdk.Required(),
			mcpsdk.Description("Subscription id to remove."),
		),
	)
}

// toolAutonomousSubscriptions lists the stored subscriptions.
func (s *Server) toolAutonomousSubscriptions() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_subscriptions",
		mcpsdk.WithDescription("List the daemon's persisted lifecycle-event subscriptions, optionally filtered by owner. Proxies GET /api/autonomous/subscriptions. The CLI equivalent is `datawatch autonomous subscriptions [--owner <owner>]`."),
		mcpsdk.WithString("owner",
			mcpsdk.Description("Optional owner filter (e.g. \"operator\"); omitted = all owners."),
		),
	)
}

// --- Handlers ---------------------------------------------------------------

// parseSubscribeKinds accepts the kinds argument as a string (comma or
// space separated) or as a string array, and returns the cleaned kinds.
func parseSubscribeKinds(arg any) ([]string, bool) {
	switch v := arg.(type) {
	case string:
		parts := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == ';' })
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out, len(out) > 0
	case []string:
		out := make([]string, 0, len(v))
		for _, p := range v {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out, len(out) > 0
	case []any:
		out := make([]string, 0, len(v))
		for _, p := range v {
			if s, ok := p.(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					out = append(out, s)
				}
			}
		}
		return out, len(out) > 0
	}
	return nil, false
}

// handleAutonomousSubscribe proxies POST /api/autonomous/subscriptions.
// expires_in_seconds (MCP-only) is converted to the REST expires_at field;
// all other parameters map 1:1, and the daemon's ValidateSubscription
// produces the authoritative validation errors (proxied back verbatim).
func (s *Server) handleAutonomousSubscribe(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	filter := map[string]any{}
	if kinds, ok := parseSubscribeKinds(req.GetArguments()["kinds"]); ok {
		filter["kinds"] = kinds
	}
	if level := req.GetString("level", ""); level != "" {
		filter["level"] = level
	}
	if prdID := req.GetString("prd_id", ""); prdID != "" {
		filter["prd_id"] = prdID
	}
	if taskID := req.GetString("task_id", ""); taskID != "" {
		filter["task_id"] = taskID
	}

	body := map[string]any{
		"filter": filter,
		"sink": map[string]any{
			"type":   req.GetString("sink_type", ""),
			"target": req.GetString("sink_target", ""),
		},
	}
	if actionText := req.GetString("action_text", ""); actionText != "" {
		body["action"] = map[string]any{"type": "send_input", "text": actionText}
	}
	if req.GetBool("one_shot", false) {
		body["one_shot"] = true
	}
	if req.GetBool("coalesce", false) {
		body["coalesce"] = true
	}
	if expires, ok := getNumberArg(req, "expires_in_seconds"); ok && expires > 0 {
		expiry := time.Now().UTC().Add(time.Duration(expires * float64(time.Second)))
		body["expires_at"] = expiry.Format(time.RFC3339)
	}

	respBody, err := s.proxyJSON(http.MethodPost, autonomousSubscribePath, body)
	if err != nil {
		return toolError("autonomous_subscribe", err), nil
	}
	return textOK(string(respBody)), nil
}

// handleAutonomousUnsubscribe proxies DELETE /api/autonomous/subscriptions/{id}.
func (s *Server) handleAutonomousUnsubscribe(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := strings.TrimSpace(req.GetString("id", ""))
	if id == "" {
		return toolErrorf("autonomous_unsubscribe", "id is required"), nil
	}
	respBody, err := s.proxyJSON(http.MethodDelete, autonomousSubscribePath+"/"+url.PathEscape(id), nil)
	if err != nil {
		return toolError("autonomous_unsubscribe", err), nil
	}
	if out := strings.TrimSpace(string(respBody)); out != "" {
		return textOK(out), nil
	}
	// 204 has no body; echo a small confirmation so the caller sees the id.
	idJSON, _ := json.Marshal(id)
	if idJSON == nil {
		idJSON = []byte(`""`)
	}
	return textOK(fmt.Sprintf(`{"unsubscribed":%s}`, idJSON)), nil
}

// handleAutonomousSubscriptions proxies GET /api/autonomous/subscriptions.
func (s *Server) handleAutonomousSubscriptions(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	path := autonomousSubscribePath
	if owner := strings.TrimSpace(req.GetString("owner", "")); owner != "" {
		path += "?owner=" + url.QueryEscape(owner)
	}
	respBody, err := s.proxyJSON(http.MethodGet, path, nil)
	if err != nil {
		return toolError("autonomous_subscriptions", err), nil
	}
	return textOK(string(respBody)), nil
}

// getNumberArg reads a float64 argument (MCP number or numeric string).
func getNumberArg(req mcpsdk.CallToolRequest, key string) (float64, bool) {
	v := req.GetArguments()[key]
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(n), "%g", &f); err == nil {
			return f, true
		}
	}
	return 0, false
}
