package mcp

// BL357 — durable role-based work queue MCP tools.
//
// Tools exposed:
//   queue_push     — push a new work item for a role
//   queue_claim    — atomically claim the oldest pending item for a role
//   queue_complete — mark a claimed item as complete
//   queue_fail     — mark a claimed item as failed
//   queue_list     — list queue items, optionally filtered

import (
	"context"
	"encoding/json"
	"fmt"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// toolQueuePush returns the queue_push tool definition.
func (s *Server) toolQueuePush() mcpsdk.Tool {
	return mcpsdk.NewTool("queue_push",
		mcpsdk.WithDescription("Push a new work item onto the role-based work queue (BL357)."),
		mcpsdk.WithString("role",
			mcpsdk.Required(),
			mcpsdk.Description("Role that can claim this work item."),
		),
		mcpsdk.WithString("payload",
			mcpsdk.Description("Optional JSON object payload for the work item. Example: {\"key\":\"value\"}"),
		),
	)
}

func (s *Server) handleQueuePush(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.queueStore == nil {
		return mcpsdk.NewToolResultText("Queue store not available. Start the daemon first."), nil
	}
	role := req.GetString("role", "")
	if role == "" {
		return mcpsdk.NewToolResultText("Error: role is required"), nil
	}
	var payload map[string]any
	if ps := req.GetString("payload", ""); ps != "" {
		if err := json.Unmarshal([]byte(ps), &payload); err != nil {
			return mcpsdk.NewToolResultText(fmt.Sprintf("Error: invalid payload JSON: %v", err)), nil
		}
	}
	it, err := s.queueStore.Push(role, payload)
	if err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error: push failed: %v", err)), nil
	}
	b, _ := json.MarshalIndent(it, "", "  ")
	return mcpsdk.NewToolResultText(string(b)), nil
}

// toolQueueClaim returns the queue_claim tool definition.
func (s *Server) toolQueueClaim() mcpsdk.Tool {
	return mcpsdk.NewTool("queue_claim",
		mcpsdk.WithDescription("Atomically claim the oldest pending work item for the given role (BL357)."),
		mcpsdk.WithString("role",
			mcpsdk.Required(),
			mcpsdk.Description("Role to claim a work item for."),
		),
		mcpsdk.WithString("claimed_by",
			mcpsdk.Required(),
			mcpsdk.Description("Session FullID or identifier claiming the item."),
		),
		mcpsdk.WithNumber("lease_seconds",
			mcpsdk.Description("How long (seconds) to hold the claim before it expires. Default: 300."),
		),
	)
}

func (s *Server) handleQueueClaim(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.queueStore == nil {
		return mcpsdk.NewToolResultText("Queue store not available. Start the daemon first."), nil
	}
	role := req.GetString("role", "")
	if role == "" {
		return mcpsdk.NewToolResultText("Error: role is required"), nil
	}
	claimedBy := req.GetString("claimed_by", "")
	if claimedBy == "" {
		return mcpsdk.NewToolResultText("Error: claimed_by is required"), nil
	}
	leaseSeconds := 300
	if ls := req.GetFloat("lease_seconds", 0); ls > 0 {
		leaseSeconds = int(ls)
	}
	it, err := s.queueStore.Claim(role, claimedBy, leaseSeconds)
	if err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error: claim failed: %v", err)), nil
	}
	if it == nil {
		return mcpsdk.NewToolResultText("No pending items available for role: " + role), nil
	}
	b, _ := json.MarshalIndent(it, "", "  ")
	return mcpsdk.NewToolResultText(string(b)), nil
}

// toolQueueComplete returns the queue_complete tool definition.
func (s *Server) toolQueueComplete() mcpsdk.Tool {
	return mcpsdk.NewTool("queue_complete",
		mcpsdk.WithDescription("Mark a claimed work item as complete (BL357)."),
		mcpsdk.WithString("id",
			mcpsdk.Required(),
			mcpsdk.Description("ID of the queue item to complete."),
		),
		mcpsdk.WithString("result",
			mcpsdk.Description("Optional JSON object result payload. Example: {\"output\":\"done\"}"),
		),
	)
}

func (s *Server) handleQueueComplete(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.queueStore == nil {
		return mcpsdk.NewToolResultText("Queue store not available. Start the daemon first."), nil
	}
	id := req.GetString("id", "")
	if id == "" {
		return mcpsdk.NewToolResultText("Error: id is required"), nil
	}
	var result map[string]any
	if rs := req.GetString("result", ""); rs != "" {
		if err := json.Unmarshal([]byte(rs), &result); err != nil {
			return mcpsdk.NewToolResultText(fmt.Sprintf("Error: invalid result JSON: %v", err)), nil
		}
	}
	if err := s.queueStore.Complete(id, result); err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error: complete failed: %v", err)), nil
	}
	return mcpsdk.NewToolResultText("Queue item " + id + " marked complete."), nil
}

// toolQueueFail returns the queue_fail tool definition.
func (s *Server) toolQueueFail() mcpsdk.Tool {
	return mcpsdk.NewTool("queue_fail",
		mcpsdk.WithDescription("Mark a claimed work item as failed (BL357)."),
		mcpsdk.WithString("id",
			mcpsdk.Required(),
			mcpsdk.Description("ID of the queue item to fail."),
		),
		mcpsdk.WithString("error",
			mcpsdk.Required(),
			mcpsdk.Description("Error message describing why the item failed."),
		),
	)
}

func (s *Server) handleQueueFail(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.queueStore == nil {
		return mcpsdk.NewToolResultText("Queue store not available. Start the daemon first."), nil
	}
	id := req.GetString("id", "")
	if id == "" {
		return mcpsdk.NewToolResultText("Error: id is required"), nil
	}
	errMsg := req.GetString("error", "")
	if err := s.queueStore.Fail(id, errMsg); err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error: fail operation failed: %v", err)), nil
	}
	return mcpsdk.NewToolResultText("Queue item " + id + " marked failed."), nil
}

// toolQueueList returns the queue_list tool definition.
func (s *Server) toolQueueList() mcpsdk.Tool {
	return mcpsdk.NewTool("queue_list",
		mcpsdk.WithDescription("List work queue items, optionally filtered by role and/or state (BL357)."),
		mcpsdk.WithString("role",
			mcpsdk.Description("Filter by role. Empty = all roles."),
		),
		mcpsdk.WithString("state",
			mcpsdk.Description("Filter by state (pending|claimed|complete|failed). Empty = all states."),
		),
	)
}

func (s *Server) handleQueueList(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.queueStore == nil {
		return mcpsdk.NewToolResultText("Queue store not available. Start the daemon first."), nil
	}
	role := req.GetString("role", "")
	state := req.GetString("state", "")
	items := s.queueStore.List(role, state)
	if len(items) == 0 {
		return mcpsdk.NewToolResultText("No queue items found."), nil
	}
	b, _ := json.MarshalIndent(items, "", "  ")
	return mcpsdk.NewToolResultText(string(b)), nil
}
