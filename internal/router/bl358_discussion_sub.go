package router

// BL358 — discussion push/subscribe comm channel handler.
//
// Commands:
//
//	discussion_sub subscribe discussion_id=<id> session_name=<name>
//	discussion_sub unsubscribe discussion_id=<id> session_name=<name>
//	discussion_sub list

import (
	"encoding/json"
	"fmt"
)

// handleDiscussionSubCmd dispatches discussion_sub commands over the comm channel (BL358).
func (r *Router) handleDiscussionSubCmd(cmd Command) {
	switch cmd.DiscussionSubVerb {
	case "subscribe":
		r.handleDiscussionSubSubscribeCmd(cmd)
	case "unsubscribe":
		r.handleDiscussionSubUnsubscribeCmd(cmd)
	case "list", "":
		r.handleDiscussionSubListCmd()
	default:
		r.send(fmt.Sprintf("[discussion_sub] unknown verb %q. Use: subscribe | unsubscribe | list", cmd.DiscussionSubVerb))
	}
}

func (r *Router) handleDiscussionSubSubscribeCmd(cmd Command) {
	if cmd.DiscussionSubDiscussionID == "" {
		r.send("[discussion_sub] subscribe requires discussion_id=<id>")
		return
	}
	if cmd.DiscussionSubSessionName == "" {
		r.send("[discussion_sub] subscribe requires session_name=<name>")
		return
	}
	body, _ := json.Marshal(map[string]any{
		"discussion_id": cmd.DiscussionSubDiscussionID,
		"session_name":  cmd.DiscussionSubSessionName,
	})
	out, err := r.commJSON("POST", "/api/discussion-subs", string(body))
	if err != nil {
		r.send(fmt.Sprintf("[discussion_sub] subscribe failed: %v", err))
		return
	}
	var res map[string]any
	if json.Unmarshal([]byte(out), &res) == nil {
		if status, ok := res["status"].(string); ok {
			r.send(fmt.Sprintf("[discussion_sub] session %q %s discussion %q",
				cmd.DiscussionSubSessionName, status, cmd.DiscussionSubDiscussionID))
			return
		}
	}
	r.send("[discussion_sub] subscribed: " + out)
}

func (r *Router) handleDiscussionSubUnsubscribeCmd(cmd Command) {
	if cmd.DiscussionSubDiscussionID == "" {
		r.send("[discussion_sub] unsubscribe requires discussion_id=<id>")
		return
	}
	if cmd.DiscussionSubSessionName == "" {
		r.send("[discussion_sub] unsubscribe requires session_name=<name>")
		return
	}
	path := fmt.Sprintf("/api/discussion-subs/%s/%s",
		cmd.DiscussionSubDiscussionID, cmd.DiscussionSubSessionName)
	out, err := r.commJSON("DELETE", path, "")
	if err != nil {
		r.send(fmt.Sprintf("[discussion_sub] unsubscribe failed: %v", err))
		return
	}
	var res map[string]any
	if json.Unmarshal([]byte(out), &res) == nil {
		if status, ok := res["status"].(string); ok {
			r.send(fmt.Sprintf("[discussion_sub] session %q %s discussion %q",
				cmd.DiscussionSubSessionName, status, cmd.DiscussionSubDiscussionID))
			return
		}
	}
	r.send("[discussion_sub] unsubscribed: " + out)
}

func (r *Router) handleDiscussionSubListCmd() {
	out, err := r.commGet("/api/discussion-subs", nil)
	if err != nil {
		r.send(fmt.Sprintf("[discussion_sub] list failed: %v", err))
		return
	}
	var subs []map[string]any
	if json.Unmarshal([]byte(out), &subs) != nil || len(subs) == 0 {
		r.send("[discussion_sub] no subscriptions found.")
		return
	}
	var lines string
	lines = fmt.Sprintf("[discussion_sub] %d subscription(s):\n", len(subs))
	for _, s := range subs {
		discID, _ := s["discussion_id"].(string)
		sessName, _ := s["session_name"].(string)
		lines += fmt.Sprintf("  discussion=%-20s  session=%s\n", discID, sessName)
	}
	r.send(lines)
}
