// MCP tools to update and test-fire a lifecycle-event subscription. They
// proxy the daemon's REST routes on the loopback exactly like the other
// subscription tools (autonomous_subscription_tools.go) — the MCP server
// holds no subscription state of its own:
//
//	PUT  /api/autonomous/subscriptions/{id}        → autonomous_subscription_update
//	POST /api/autonomous/subscriptions/{id}/test   → autonomous_subscription_test
//
// The same surface over CLI: `datawatch autonomous subscription-update <id>`
// and `datawatch autonomous subscription-test <id>`
// (cmd/datawatch/cli_autonomous_subscription_update.go).
package mcp

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// --- Tool definitions ------------------------------------------------------

// toolAutonomousSubscriptionUpdate replaces the mutable fields of one
// stored subscription.
func (s *Server) toolAutonomousSubscriptionUpdate() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_subscription_update",
		mcpsdk.WithDescription("Update a lifecycle-event subscription in place by id (the id from autonomous_subscribe / autonomous_subscriptions). Takes the same fields as autonomous_subscribe and REPLACES the subscription's mutable fields (filter, sink, action, one_shot, coalesce, expiry) with them: a field you leave out is cleared, so pass the full desired configuration. The id and creation time are kept. Proxies PUT /api/autonomous/subscriptions/{id}; 404 for unknown ids, 400 with the daemon's validation message for a malformed configuration. Returns the updated record as JSON. The CLI equivalent is `datawatch autonomous subscription-update <id>`."),
		mcpsdk.WithString("id",
			mcpsdk.Required(),
			mcpsdk.Description("Subscription id to update."),
		),
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
			mcpsdk.Description(subscriptionSinkTypeDesc),
		),
		mcpsdk.WithString("sink_target",
			mcpsdk.Description(subscriptionSinkTargetDesc),
		),
		mcpsdk.WithString("secret_ref",
			mcpsdk.Description(subscriptionSecretRefDesc),
		),
		mcpsdk.WithString("action_text",
			mcpsdk.Description("Optional text typed into the sink session when the event fires (requires sink_type \"session\")."),
		),
		mcpsdk.WithBoolean("one_shot",
			mcpsdk.Description("Delete the subscription after its first delivery. Default false."),
		),
		mcpsdk.WithBoolean("coalesce",
			mcpsdk.Description("Coalesce human-facing sink deliveries in quiet windows. Default false."),
		),
		mcpsdk.WithNumber("expires_in_seconds",
			mcpsdk.Description("Optional TTL in seconds from now; the subscription stops matching after it elapses. Must be positive; omitted = never expires."),
		),
	)
}

// toolAutonomousSubscriptionTest delivers a labelled test event to one
// subscription's sink.
func (s *Server) toolAutonomousSubscriptionTest() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_subscription_test",
		mcpsdk.WithDescription("Deliver a labelled test event to one lifecycle-event subscription's sink so the route can be checked without waiting for a real event. Proxies POST /api/autonomous/subscriptions/{id}/test; returns {\"ok\",\"sink\",\"error\"}, 404 for unknown ids. The CLI equivalent is `datawatch autonomous subscription-test <id>`."),
		mcpsdk.WithString("id",
			mcpsdk.Required(),
			mcpsdk.Description("Subscription id to test."),
		),
	)
}

// --- Handlers ---------------------------------------------------------------

// subscriptionPutBody builds the REST body for a subscription update from
// the tool arguments (the same mapping autonomous_subscribe uses). A
// non-empty reason means an argument was invalid and body is nil.
func subscriptionPutBody(req mcpsdk.CallToolRequest) (body map[string]any, reason string) {
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

	body = map[string]any{
		"filter": filter,
		"sink":   subscriptionSinkBody(req),
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
	if _, present := req.GetArguments()["expires_in_seconds"]; present {
		expires, ok := getNumberArg(req, "expires_in_seconds")
		if !ok || expires <= 0 {
			return nil, "expires_in_seconds must be a positive number"
		}
		expiry := time.Now().UTC().Add(time.Duration(expires * float64(time.Second)))
		body["expires_at"] = expiry.Format(time.RFC3339)
	}
	return body, ""
}

// handleAutonomousSubscriptionUpdate proxies PUT /api/autonomous/subscriptions/{id}.
// The daemon's validation errors (400) and unknown-id answer (404) are
// passed back verbatim.
func (s *Server) handleAutonomousSubscriptionUpdate(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := strings.TrimSpace(req.GetString("id", ""))
	if id == "" {
		return toolErrorf("autonomous_subscription_update", "id is required"), nil
	}
	body, reason := subscriptionPutBody(req)
	if reason != "" {
		return toolErrorf("autonomous_subscription_update", "%s", reason), nil
	}
	respBody, err := s.proxyJSON(http.MethodPut, autonomousSubscribePath+"/"+url.PathEscape(id), body)
	if err != nil {
		return toolError("autonomous_subscription_update", err), nil
	}
	return textOK(string(respBody)), nil
}

// handleAutonomousSubscriptionTest proxies POST /api/autonomous/subscriptions/{id}/test.
func (s *Server) handleAutonomousSubscriptionTest(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := strings.TrimSpace(req.GetString("id", ""))
	if id == "" {
		return toolErrorf("autonomous_subscription_test", "id is required"), nil
	}
	respBody, err := s.proxyJSON(http.MethodPost, autonomousSubscribePath+"/"+url.PathEscape(id)+"/test", nil)
	if err != nil {
		return toolError("autonomous_subscription_test", err), nil
	}
	return textOK(string(respBody)), nil
}
