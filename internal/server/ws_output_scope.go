// WebSocket output scoping and unsubscribe (BL432 / GH#242).
//
// Terminal output used to reach every connected client: each running
// session's output, raw_output and pane_capture frames went to every
// WebSocket, whether or not that client had the session open (~1000
// frames/min on a phone with no terminal showing). A client can now opt
// in to receiving output only for the sessions it subscribed to, through
// the same hello message that negotiates the session list:
//
//	client → server  {"type":"hello","data":{"output":"subscribed"}}
//	server → client  {"type":"hello_ack","data":{...,"output":"subscribed",...}}
//	client → server  {"type":"subscribe","data":{"session_id":"..."}}
//	client → server  {"type":"unsubscribe","data":{"session_id":"..."}}
//
// output:
//   - "all"        — the legacy behavior: terminal frames for every
//     session. The PWA relies on this (it buffers output for sessions it
//     is not viewing), so it stays the default.
//   - "subscribed" — output / raw_output / pane_capture / chat_message
//     frames for a session are delivered only while the client is
//     subscribed to it.
//
// Scoped frame types: output, raw_output and pane_capture are the
// terminal stream itself. chat_message is scoped too: it is the
// chat-mode sessions' equivalent of the terminal stream (streaming
// chunks emitted per token batch by chat backends, rendered only in the
// session view), so leaving it unscoped would keep the per-session
// firehose for chat sessions. response is NOT scoped: it fires once per
// completed LLM turn and clients use it outside the session view (copy
// last response, alert context), so it behaves like a control frame.
// session_state, sessions, session_removed, needs_input, alert,
// alerts_read, notification, channel_* and all other control frames are
// never scoped.
//
// unsubscribe removes the subscription and cancels the screen capture
// this client started for the session; previously captures ran until the
// client disconnected. It works in either output mode.
//
// Session IDs: output frames carry the session's FullID
// ("<hostname>-<id>"), while clients may subscribe with either the full
// or the short ID. sessionIDsMatch treats the two forms as equal
// (case-insensitively), so either form subscribes and unsubscribes.
//
// A client that never sends output in hello follows
// server.ws_output_scope_default ("all" unless configured), read from the
// live config per frame, so PUT /api/config applies without reconnect.

package server

import (
	"context"
	"strings"
)

// Negotiable output modes.
const (
	outputScopeAll        = "all"
	outputScopeSubscribed = "subscribed"
)

// MsgUnsubscribe is client → server {"session_id":"..."}.
const MsgUnsubscribe MessageType = "unsubscribe"

// normalizeOutputScope returns the mode, or "" if it is not one.
func normalizeOutputScope(v string) string {
	switch v {
	case outputScopeAll, outputScopeSubscribed:
		return v
	}
	return ""
}

// defaultOutputScope is the mode for clients that never chose one.
func (h *Hub) defaultOutputScope() string {
	if h.outputScopeDefaultFn != nil {
		if v := normalizeOutputScope(h.outputScopeDefaultFn()); v != "" {
			return v
		}
	}
	return outputScopeAll
}

// outputScope returns c's effective output mode.
func (h *Hub) outputScope(c *client) string {
	c.mu.Lock()
	v := c.outputScope
	c.mu.Unlock()
	if v == "" {
		v = h.defaultOutputScope()
	}
	return v
}

// sessionIDsMatch reports whether a and b name the same session, where
// either may be a FullID ("<hostname>-<id>") or a short ID ("<id>").
func sessionIDsMatch(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a, b = strings.ToLower(a), strings.ToLower(b)
	return a == b || strings.HasSuffix(a, "-"+b) || strings.HasSuffix(b, "-"+a)
}

// subscribedTo reports whether c holds a subscription matching sessionID.
func (c *client) subscribedTo(sessionID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.subscribed[sessionID] {
		return true
	}
	for id, on := range c.subscribed {
		if on && sessionIDsMatch(id, sessionID) {
			return true
		}
	}
	return false
}

// wantsSessionOutput is the predicate for per-session terminal frames.
func (h *Hub) wantsSessionOutput(sessionID string) func(*client) bool {
	return func(c *client) bool {
		return h.outputScope(c) != outputScopeSubscribed || c.subscribedTo(sessionID)
	}
}

// BroadcastPaneCapture sends a pane capture for a session to the clients
// that want that session's output.
func (h *Hub) BroadcastPaneCapture(sessionID string, lines []string) {
	h.broadcastTo("pane_capture", OutputData{SessionID: sessionID, Lines: lines}, h.wantsSessionOutput(sessionID))
}

// unsubscribe drops every subscription of c matching sessionID and cancels
// the screen captures c started for it. Returns how many captures it
// cancelled.
func (c *client) unsubscribe(sessionID string) int {
	c.mu.Lock()
	var cancels []context.CancelFunc
	for id := range c.subscribed {
		if sessionIDsMatch(id, sessionID) {
			delete(c.subscribed, id)
		}
	}
	for id, cancel := range c.captureCancels {
		if sessionIDsMatch(id, sessionID) {
			cancels = append(cancels, cancel)
			delete(c.captureCancels, id)
		}
	}
	c.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	return len(cancels)
}

// wsOutputScopeDefault reads server.ws_output_scope_default from the live
// config; anything unset or invalid means "all".
func (s *Server) wsOutputScopeDefault() string {
	if s.cfg != nil {
		if v := normalizeOutputScope(s.cfg.Server.WSOutputScopeDefault); v != "" {
			return v
		}
	}
	return outputScopeAll
}
