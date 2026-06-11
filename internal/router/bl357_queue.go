package router

// BL357 — durable role-based work queue comm channel handler.
//
// Commands:
//   queue push role=<r> [payload=<json>]
//   queue claim role=<r> [claimed_by=<s>] [lease=<sec>]
//   queue complete id=<id> [result=<json>]
//   queue fail id=<id> error=<msg>
//   queue list [role=<r>] [state=<s>]

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// handleQueueCmd dispatches queue commands over the comm channel (BL357).
func (r *Router) handleQueueCmd(cmd Command) {
	switch cmd.QueueVerb {
	case "push":
		r.handleQueuePushCmd(cmd)
	case "claim":
		r.handleQueueClaimCmd(cmd)
	case "complete":
		r.handleQueueCompleteCmd(cmd)
	case "fail":
		r.handleQueueFailCmd(cmd)
	case "list", "":
		r.handleQueueListCmd(cmd)
	default:
		r.send(fmt.Sprintf("[queue] unknown verb %q. Use: push | claim | complete | fail | list", cmd.QueueVerb))
	}
}

func (r *Router) handleQueuePushCmd(cmd Command) {
	if cmd.QueueRole == "" {
		r.send("[queue] push requires role=<role>")
		return
	}
	payload := cmd.QueuePayload
	if payload == "" {
		payload = "{}"
	}
	body, _ := json.Marshal(map[string]any{
		"role":    cmd.QueueRole,
		"payload": json.RawMessage(payload),
	})
	out, err := r.commJSON("POST", "/api/queue/push", string(body))
	if err != nil {
		r.send(fmt.Sprintf("[queue] push failed: %v", err))
		return
	}
	var it map[string]any
	if json.Unmarshal([]byte(out), &it) == nil {
		r.send(fmt.Sprintf("[queue] pushed item %v (role=%s state=%v)", it["id"], cmd.QueueRole, it["state"]))
		return
	}
	r.send("[queue] pushed: " + strings.TrimSpace(out))
}

func (r *Router) handleQueueClaimCmd(cmd Command) {
	if cmd.QueueRole == "" {
		r.send("[queue] claim requires role=<role>")
		return
	}
	lease := cmd.QueueLeaseSeconds
	if lease <= 0 {
		lease = 300
	}
	body, _ := json.Marshal(map[string]any{
		"role":          cmd.QueueRole,
		"claimed_by":    cmd.QueueClaimedBy,
		"lease_seconds": lease,
	})
	out, err := r.commJSON("POST", "/api/queue/claim", string(body))
	if err != nil {
		r.send(fmt.Sprintf("[queue] claim failed: %v", err))
		return
	}
	// nil response means no items available
	if strings.TrimSpace(out) == "null" {
		r.send("[queue] no pending items available for role: " + cmd.QueueRole)
		return
	}
	var it map[string]any
	if json.Unmarshal([]byte(out), &it) == nil {
		r.send(fmt.Sprintf("[queue] claimed item %v (role=%s claimed_by=%v)", it["id"], cmd.QueueRole, it["claimed_by"]))
		return
	}
	r.send("[queue] claimed: " + strings.TrimSpace(out))
}

func (r *Router) handleQueueCompleteCmd(cmd Command) {
	if cmd.QueueID == "" {
		r.send("[queue] complete requires id=<id>")
		return
	}
	result := cmd.QueueResult
	if result == "" {
		result = "{}"
	}
	body, _ := json.Marshal(map[string]any{
		"id":     cmd.QueueID,
		"result": json.RawMessage(result),
	})
	_, err := r.commJSON("POST", "/api/queue/complete", string(body))
	if err != nil {
		r.send(fmt.Sprintf("[queue] complete failed: %v", err))
		return
	}
	r.send("[queue] item " + cmd.QueueID + " marked complete.")
}

func (r *Router) handleQueueFailCmd(cmd Command) {
	if cmd.QueueID == "" {
		r.send("[queue] fail requires id=<id>")
		return
	}
	body, _ := json.Marshal(map[string]any{
		"id":    cmd.QueueID,
		"error": cmd.QueueError,
	})
	_, err := r.commJSON("POST", "/api/queue/fail", string(body))
	if err != nil {
		r.send(fmt.Sprintf("[queue] fail failed: %v", err))
		return
	}
	r.send("[queue] item " + cmd.QueueID + " marked failed.")
}

func (r *Router) handleQueueListCmd(cmd Command) {
	q := url.Values{}
	if cmd.QueueRole != "" {
		q.Set("role", cmd.QueueRole)
	}
	if cmd.QueueState != "" {
		q.Set("state", cmd.QueueState)
	}
	out, err := r.commGet("/api/queue", q)
	if err != nil {
		r.send(fmt.Sprintf("[queue] list failed: %v", err))
		return
	}
	var items []map[string]any
	if json.Unmarshal([]byte(out), &items) != nil || len(items) == 0 {
		r.send("[queue] no items found.")
		return
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "[queue] %d item(s):\n", len(items))
	for _, it := range items {
		id, _ := it["id"].(string)
		role, _ := it["role"].(string)
		state, _ := it["state"].(string)
		claimedBy, _ := it["claimed_by"].(string)
		line := fmt.Sprintf("  %s  role=%-12s  state=%-8s", id, role, state)
		if claimedBy != "" {
			line += "  claimed_by=" + claimedBy
		}
		sb.WriteString(line + "\n")
	}
	r.send(sb.String())
}
