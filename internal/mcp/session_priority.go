package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

// sessionPriorityOption declares the optional priority argument shared by
// start_session and set_session_priority.
func sessionPriorityOption() mcpsdk.ToolOption {
	return mcpsdk.WithNumber("priority",
		mcpsdk.Description("Capacity-queue priority for the session (integer; higher is admitted first when capacity is contended). Default 0. A non-integer value is rejected."),
	)
}

// priorityFromReq returns the caller's priority and whether one was given.
// A value that is not a whole number is an error.
func priorityFromReq(req mcpsdk.CallToolRequest) (int, bool, error) {
	raw, ok := req.GetArguments()["priority"]
	if !ok || raw == nil {
		return 0, false, nil
	}
	p, err := session.ParsePriority(raw)
	if err != nil {
		return 0, false, err
	}
	return p, true, nil
}

func (s *Server) toolSetSessionPriority() mcpsdk.Tool {
	return mcpsdk.NewTool("set_session_priority",
		mcpsdk.WithDescription("Set the capacity-queue priority of an existing session. Higher priority sessions are admitted first when compute capacity is contended (default 0)."),
		mcpsdk.WithString("session_id",
			mcpsdk.Description("Session ID (required if session_name not provided)"),
		),
		mcpsdk.WithString("session_name",
			mcpsdk.Description("Session name (alternative to session_id; resolves to oldest active session with this name)"),
		),
		mcpsdk.WithNumber("priority",
			mcpsdk.Required(),
			mcpsdk.Description("Integer priority; higher runs first. 0 clears it."),
		),
	)
}

func (s *Server) handleSetSessionPriority(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if deny := mcpFedCap(ctx, federation.CapSessionsWrite); deny != nil {
		return deny, nil
	}
	id := req.GetString("session_id", "")
	if sn := req.GetString("session_name", ""); sn != "" && id == "" {
		id = sn // resolveSession handles name lookup
	}
	if id == "" {
		return toolErrorf("set_session_priority", "session_id or session_name is required"), nil
	}
	priority, ok, err := priorityFromReq(req)
	if err != nil {
		return toolError("set_session_priority", err), nil
	}
	if !ok {
		return toolErrorf("set_session_priority", "priority is required"), nil
	}
	sess, err := s.resolveSession(id)
	if err != nil {
		return toolError("set_session_priority", err), nil
	}
	if sess == nil {
		return toolErrorf("set_session_priority", "session %q not found", id), nil
	}
	if err := s.manager.SetPriority(sess.FullID, priority); err != nil {
		return toolError("set_session_priority", err), nil
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Session %s priority set to %d.", sess.ID, priority)), nil
}
