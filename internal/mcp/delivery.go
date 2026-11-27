// Delivery check MCP tools. They proxy the loopback REST surface
// (/api/delivery) so names and validation match REST, the CLI and the chat
// channels. The loopback uses the tool server's own token, so a call made with
// a session or peer credential names that caller in a header; the daemon
// records it as who acted. No tool argument can set it. The admin token also
// means the daemon cannot refuse a federation peer that is connected straight
// to the tool server, so every delivery tool checks that peer's capability
// itself before it calls.

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/federation"
)

// deliveryCaller is the session or peer this call is made for, or "" for the
// operator.
func (s *Server) deliveryCaller(ctx context.Context) string {
	if id, scoped := s.scopedCallerSessionID(ctx); scoped && id != "" {
		return "session:" + id
	}
	if peer := mcpPeerFromContext(ctx); peer != nil {
		return "peer:" + peer.Name
	}
	switch p := federation.PrincipalFromContext(ctx); {
	case strings.HasPrefix(p, "peer:"):
		return p
	case p == "session-scoped":
		return "session:unknown"
	}
	return ""
}

// deliveryDo calls one delivery route; a nil body sends none.
func (s *Server) deliveryDo(ctx context.Context, method, path string, body map[string]any) ([]byte, error) {
	if s.webPort == 0 {
		return nil, fmt.Errorf("REST loopback unavailable (web server disabled)")
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", s.webPort, path), rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if caller := s.deliveryCaller(ctx); caller != "" {
		req.Header.Set(federation.HeaderDeliveryCaller, caller)
	}
	resp, err := s.webDo(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, boundedErrorBody(out))
	}
	return out, nil
}

// deliveryCall runs one tool: it needs the capability the tool is listed with.
func (s *Server) deliveryCall(ctx context.Context, tool, method, path string, body map[string]any) (*mcpsdk.CallToolResult, error) {
	if deny := mcpFedCap(ctx, federation.MCPToolCap[tool]); deny != nil {
		return deny, nil
	}
	out, err := s.deliveryDo(ctx, method, path, body)
	if err != nil {
		return toolError(tool, err), nil
	}
	return textOK(string(out)), nil
}

// deliverySelectorArgs are the arguments that choose entries.
var deliverySelectorArgs = []string{"keys", "kind", "state", "match_reason", "backlog"}

// deliveryBody builds an action's body from the selector arguments and extra.
// An unknown or mistyped argument is refused: dropping it would widen what the
// action applies to.
func deliveryBody(args map[string]any, extra ...string) (map[string]any, error) {
	allowed := append(slices.Clone(deliverySelectorArgs), extra...)
	names := make([]string, 0, len(args))
	for name := range args {
		names = append(names, name)
	}
	sort.Strings(names)
	body := map[string]any{}
	for _, name := range names {
		if !slices.Contains(allowed, name) {
			return nil, fmt.Errorf("unknown argument %q; this tool takes %s", name, strings.Join(allowed, ", "))
		}
		switch v := args[name]; name {
		case "keys":
			list, ok := v.([]any)
			if !ok {
				return nil, fmt.Errorf("keys must be a list of entry keys")
			}
			for _, item := range list {
				if key, isText := item.(string); !isText || strings.TrimSpace(key) == "" {
					return nil, fmt.Errorf("keys must be a list of entry keys")
				}
			}
			if len(list) > 0 {
				body[name] = list
			}
		case "backlog":
			b, ok := v.(bool)
			if text, isText := v.(string); isText {
				parsed, err := strconv.ParseBool(strings.TrimSpace(text))
				b, ok = parsed, err == nil
			}
			if !ok {
				return nil, fmt.Errorf("backlog must be true or false")
			}
			if b {
				body[name] = true
			}
		default:
			text, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("%s must be text", name)
			}
			if text = strings.TrimSpace(text); text != "" {
				body[name] = text
			}
		}
	}
	return body, nil
}

func (s *Server) deliveryAction(ctx context.Context, req mcpsdk.CallToolRequest, tool, path string, extra ...string) (*mcpsdk.CallToolResult, error) {
	body, err := deliveryBody(req.GetArguments(), extra...)
	if err != nil {
		return toolError(tool, err), nil
	}
	return s.deliveryCall(ctx, tool, http.MethodPost, path, body)
}

func deliverySelectorOptions(desc string, more ...mcpsdk.ToolOption) []mcpsdk.ToolOption {
	return append([]mcpsdk.ToolOption{
		mcpsdk.WithDescription(desc + " Name the entries with keys, kind, state, match_reason or backlog; every one given narrows the choice and at least one is needed. Who did it is recorded from the caller's credential."),
		mcpsdk.WithArray("keys", mcpsdk.WithStringItems(), mcpsdk.Description("Entry keys, as <kind>:<id>:<state>")),
		mcpsdk.WithString("kind", mcpsdk.Description("Only entries of this kind (automaton, task, bug, schedule, session, ...)")),
		mcpsdk.WithString("state", mcpsdk.Description("Only entries in this state")),
		mcpsdk.WithString("match_reason", mcpsdk.Description("Only entries listed for this reason code")),
		mcpsdk.WithBoolean("backlog", mcpsdk.Description("Only the backlog: entries already true when the check was first started")),
	}, more...)
}

func (s *Server) toolDeliveryStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("delivery_status",
		mcpsdk.WithDescription("Show the delivery check's current report without running it: green or red and why, the listed unfinished or undelivered work, the backlog, accepted entries and the message channels. IDs, kinds, states, reason codes and times only. The check reports; it blocks nothing."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}
func (s *Server) handleDeliveryStatus(ctx context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	return s.deliveryCall(ctx, "delivery_status", http.MethodGet, "/api/delivery", nil)
}

func (s *Server) toolDeliveryCheck() mcpsdk.Tool {
	return mcpsdk.NewTool("delivery_check",
		mcpsdk.WithDescription("Run the delivery check now and return its report. It reads existing records, saves the list and may send the one message that is due; it blocks nothing and types nothing into a session."),
	)
}
func (s *Server) handleDeliveryCheck(ctx context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	return s.deliveryCall(ctx, "delivery_check", http.MethodPost, "/api/delivery/check", nil)
}

func (s *Server) toolDeliveryAck() mcpsdk.Tool {
	return mcpsdk.NewTool("delivery_ack",
		deliverySelectorOptions("Mark delivery list entries as seen. It stops their messages; they stay listed.")...)
}
func (s *Server) handleDeliveryAck(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	return s.deliveryAction(ctx, req, "delivery_ack", "/api/delivery/ack")
}

func (s *Server) toolDeliveryAccept() mcpsdk.Tool {
	return mcpsdk.NewTool("delivery_accept",
		deliverySelectorOptions("Let delivery list entries wait until a review date at most 30 days ahead.",
			mcpsdk.WithString("reason", mcpsdk.Required(), mcpsdk.Description("Reason code: a-z, 0-9, dot, dash and underscore, 1 to 64 characters")),
			mcpsdk.WithString("until", mcpsdk.Required(), mcpsdk.Description("Review date: YYYY-MM-DD or RFC 3339")),
		)...)
}
func (s *Server) handleDeliveryAccept(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	return s.deliveryAction(ctx, req, "delivery_accept", "/api/delivery/accept", "reason", "until")
}

func (s *Server) toolDeliveryDrop() mcpsdk.Tool {
	return mcpsdk.NewTool("delivery_drop",
		deliverySelectorOptions("Close failed or backlog delivery list entries. Other entries cannot be dropped; accept them with a review date.",
			mcpsdk.WithString("reason", mcpsdk.Required(), mcpsdk.Description("Reason code: a-z, 0-9, dot, dash and underscore, 1 to 64 characters")),
		)...)
}
func (s *Server) handleDeliveryDrop(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	return s.deliveryAction(ctx, req, "delivery_drop", "/api/delivery/drop", "reason")
}

func (s *Server) toolDeliveryTestMessage() mcpsdk.Tool {
	return mcpsdk.NewTool("delivery_test_message",
		mcpsdk.WithDescription("Send the delivery check's test message on every message channel that can reach a person, and report what each answered."),
	)
}
func (s *Server) handleDeliveryTestMessage(ctx context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	return s.deliveryCall(ctx, "delivery_test_message", http.MethodPost, "/api/delivery/test-message", nil)
}
