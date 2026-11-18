// MCP tools for the peer event subscriber: read what is being read, and start
// or stop reading a federation peer's lifecycle events. They proxy
// /api/autonomous/peer-events (internal/server/autonomous_peer_events.go).

package mcp

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// PeerEventsToolNames lists the peer event tools, for the parity test.
func PeerEventsToolNames() []string {
	return []string{"autonomous_peer_events", "autonomous_peer_events_control"}
}

func (s *Server) toolAutonomousPeerEvents() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_peer_events",
		mcpsdk.WithDescription(`Read the peer event subscriber: whether reading federation peers' lifecycle events is on (autonomous.notify.forwarding.peer_events) and the peers being read right now. Takes no arguments; any argument is refused. Read-only. Returns the body of GET /api/autonomous/peer-events: {"enabled":bool,"running":[peer names]}.`),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleAutonomousPeerEvents(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if args := req.GetArguments(); len(args) > 0 {
		var names []string
		for k := range args {
			names = append(names, k)
		}
		return toolErrorf("autonomous_peer_events", "unknown argument(s) %s; usage: autonomous_peer_events (takes no arguments)", strings.Join(names, ", ")), nil
	}
	out, err := s.proxyGet("/api/autonomous/peer-events", nil)
	if err != nil {
		return toolError("autonomous_peer_events", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPeerEventsControl() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_peer_events_control",
		mcpsdk.WithDescription(`Start or stop reading a federation peer's lifecycle events into this daemon's local dispatch. action is "start" or "stop"; peer is the registered federation peer name (required). For start, queue (required) is the name of the peer's pending-events queue and since (optional) is a cursor to resume after. Start needs autonomous.notify.forwarding.enabled and autonomous.notify.forwarding.peer_events both true, and a peer that grants autonomous:read; it is refused with the reason otherwise. A read lasts until stopped or the daemon restarts. Calls POST /api/autonomous/peer-events/start or /stop.`),
		mcpsdk.WithString("action", mcpsdk.Required(), mcpsdk.Description(`"start" or "stop".`)),
		mcpsdk.WithString("peer", mcpsdk.Required(), mcpsdk.Description("Registered federation peer name.")),
		mcpsdk.WithString("queue", mcpsdk.Description("start only — the peer's pending-events queue name.")),
		mcpsdk.WithString("since", mcpsdk.Description("start only — queue cursor to resume after (whole number; default 0 reads the retained backlog).")),
	)
}

func (s *Server) handleAutonomousPeerEventsControl(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	str := func(k string) string {
		v, _ := req.GetArguments()[k].(string)
		return strings.TrimSpace(v)
	}
	action, peer := str("action"), str("peer")
	if peer == "" {
		return toolErrorf("autonomous_peer_events_control", "peer is required"), nil
	}
	var body map[string]any
	switch action {
	case "start":
		queue := str("queue")
		if queue == "" {
			return toolErrorf("autonomous_peer_events_control", "queue is required for start"), nil
		}
		var since uint64
		if v := str("since"); v != "" {
			if _, err := fmt.Sscanf(v, "%d", &since); err != nil {
				return toolErrorf("autonomous_peer_events_control", "since must be a whole number, got %q", v), nil
			}
		}
		body = map[string]any{"peer": peer, "queue": queue, "since": since}
	case "stop":
		body = map[string]any{"peer": peer}
	default:
		return toolErrorf("autonomous_peer_events_control", "action must be start or stop, got %q", action), nil
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/peer-events/"+action, body)
	if err != nil {
		return toolError("autonomous_peer_events_control", err), nil
	}
	return textOK(string(out)), nil
}
