// BL358 — Discussion push/subscribe MCP tools.
//
// Three new tools:
//   discussion_subscribe    — subscribe a session to a discussion
//   discussion_unsubscribe  — remove a subscription
//   discussion_subscriptions — list all subscriptions

package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ── discussion_subscribe ─────────────────────────────────────────────────────

func (s *Server) toolDiscussionSubscribe() mcpsdk.Tool {
	return mcpsdk.NewTool("discussion_subscribe",
		mcpsdk.WithDescription("Subscribe a session to a discussion (BL358). "+
			"New entries written to the discussion are delivered to the session via send_input."),
		mcpsdk.WithString("discussion_id", mcpsdk.Required(), mcpsdk.Description("Discussion scope ID")),
		mcpsdk.WithString("session_name", mcpsdk.Required(), mcpsdk.Description("Session name to deliver entries to")),
	)
}

func (s *Server) handleDiscussionSubscribe(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("discussion_id", "")
	if id == "" {
		return textOK("Error: discussion_id is required"), nil
	}
	sname := req.GetString("session_name", "")
	if sname == "" {
		return textOK("Error: session_name is required"), nil
	}
	if s.subStore == nil {
		return textOK("Error: discussion subscription store not configured"), nil
	}
	if err := s.subStore.Subscribe(id, sname); err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(fmt.Sprintf(`{"ok":true,"discussion_id":%q,"session_name":%q,"status":"subscribed"}`, id, sname)), nil
}

// ── discussion_unsubscribe ───────────────────────────────────────────────────

func (s *Server) toolDiscussionUnsubscribe() mcpsdk.Tool {
	return mcpsdk.NewTool("discussion_unsubscribe",
		mcpsdk.WithDescription("Unsubscribe a session from a discussion (BL358). "+
			"The session will no longer receive new entries from the discussion."),
		mcpsdk.WithString("discussion_id", mcpsdk.Required(), mcpsdk.Description("Discussion scope ID")),
		mcpsdk.WithString("session_name", mcpsdk.Required(), mcpsdk.Description("Session name to remove from discussion")),
	)
}

func (s *Server) handleDiscussionUnsubscribe(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("discussion_id", "")
	if id == "" {
		return textOK("Error: discussion_id is required"), nil
	}
	sname := req.GetString("session_name", "")
	if sname == "" {
		return textOK("Error: session_name is required"), nil
	}
	if s.subStore == nil {
		return textOK("Error: discussion subscription store not configured"), nil
	}
	if err := s.subStore.Unsubscribe(id, sname); err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(fmt.Sprintf(`{"ok":true,"discussion_id":%q,"session_name":%q,"status":"unsubscribed"}`, id, sname)), nil
}

// ── discussion_subscriptions (list) ─────────────────────────────────────────

func (s *Server) toolDiscussionSubscriptions() mcpsdk.Tool {
	return mcpsdk.NewTool("discussion_subscriptions",
		mcpsdk.WithDescription("List all discussion subscriptions (BL358). "+
			"Returns all active (discussion_id, session_name) subscription pairs."),
		mcpsdk.WithString("discussion_id", mcpsdk.Description("Filter by discussion ID (optional)")),
	)
}

func (s *Server) handleDiscussionSubscriptions(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.subStore == nil {
		return textOK("Error: discussion subscription store not configured"), nil
	}
	subs := s.subStore.List()
	// Optional filter
	filterID := req.GetString("discussion_id", "")
	if filterID != "" {
		filtered := make([]interface{}, 0)
		for _, sub := range subs {
			if sub.DiscussionID == filterID {
				filtered = append(filtered, sub)
			}
		}
		out, _ := json.Marshal(filtered)
		return textOK(string(out)), nil
	}
	out, _ := json.Marshal(subs)
	if out == nil {
		out = []byte("[]")
	}
	return textOK(string(out)), nil
}

// ── dispatchDiscussionEntry ──────────────────────────────────────────────────

// dispatchDiscussionEntry delivers new discussion content to all subscribed sessions
// via send_input. Called asynchronously after a successful discussion write (BL358).
func (s *Server) dispatchDiscussionEntry(discussionID, content string) {
	if s.subStore == nil || s.manager == nil {
		return
	}
	for _, sname := range s.subStore.GetSubscribers(discussionID) {
		sess, ok := s.manager.FindSessionByName(sname)
		if !ok {
			continue
		}
		msg := fmt.Sprintf("[discussion:%s] %s", discussionID, content)
		if err := s.manager.SendInput(sess.FullID, msg, "discussion-sub"); err != nil {
			fmt.Printf("[discussion-sub] failed to deliver to %s: %v\n", sname, err)
		}
	}
}

