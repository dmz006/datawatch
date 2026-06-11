package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/session"
)

// ---- tool definitions -------------------------------------------------------

func (s *Server) toolExitHookList() mcpsdk.Tool {
	return mcpsdk.NewTool("exit_hook_list",
		mcpsdk.WithDescription("List all session crash/exit hooks (BL356). Shows name pattern, action, enabled state, cooldown, and last fired time."),
	)
}

func (s *Server) toolExitHookAdd() mcpsdk.Tool {
	return mcpsdk.NewTool("exit_hook_add",
		mcpsdk.WithDescription("Add a session crash/exit hook (BL356). Fires when a session matching 'name' either goes zombie (claude_alive flips false) or enters failed/killed state. Action 'restart' relaunches with the same task; 'notify' sends a message to another session."),
		mcpsdk.WithString("name",
			mcpsdk.Required(),
			mcpsdk.Description("Session name to watch (exact match)"),
		),
		mcpsdk.WithString("action",
			mcpsdk.Required(),
			mcpsdk.Description("Action to take: 'restart' or 'notify'"),
		),
		mcpsdk.WithString("notify_session",
			mcpsdk.Description("For action=notify: name of the session to send a message to"),
		),
		mcpsdk.WithString("notify_message",
			mcpsdk.Description("For action=notify: message text to send (default: auto-generated)"),
		),
		mcpsdk.WithNumber("cooldown_seconds",
			mcpsdk.Description("Minimum seconds between firings (default 300)"),
		),
	)
}

func (s *Server) toolExitHookDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("exit_hook_delete",
		mcpsdk.WithDescription("Delete a session crash/exit hook by ID (BL356)."),
		mcpsdk.WithString("id",
			mcpsdk.Required(),
			mcpsdk.Description("Exit hook ID to delete"),
		),
	)
}

func (s *Server) toolExitHookEnable() mcpsdk.Tool {
	return mcpsdk.NewTool("exit_hook_enable",
		mcpsdk.WithDescription("Enable a session crash/exit hook (BL356)."),
		mcpsdk.WithString("id",
			mcpsdk.Required(),
			mcpsdk.Description("Exit hook ID to enable"),
		),
	)
}

func (s *Server) toolExitHookDisable() mcpsdk.Tool {
	return mcpsdk.NewTool("exit_hook_disable",
		mcpsdk.WithDescription("Disable a session crash/exit hook without deleting it (BL356)."),
		mcpsdk.WithString("id",
			mcpsdk.Required(),
			mcpsdk.Description("Exit hook ID to disable"),
		),
	)
}

// ---- handlers ---------------------------------------------------------------

func (s *Server) handleExitHookList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.exitHookStore == nil {
		return mcpsdk.NewToolResultText("Exit hook store not available."), nil
	}
	entries := s.exitHookStore.List()
	if len(entries) == 0 {
		return mcpsdk.NewToolResultText("No exit hooks configured."), nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Exit hooks (%d):\n\n", len(entries))
	for _, e := range entries {
		fmt.Fprintf(&sb, "ID:       %s\n", e.ID)
		fmt.Fprintf(&sb, "Name:     %s\n", e.Name)
		fmt.Fprintf(&sb, "Action:   %s\n", e.Action)
		if e.Action == session.ExitHookNotify {
			fmt.Fprintf(&sb, "Notify:   %s\n", e.NotifySession)
			if e.NotifyMessage != "" {
				fmt.Fprintf(&sb, "Message:  %s\n", e.NotifyMessage)
			}
		}
		fmt.Fprintf(&sb, "Enabled:  %v\n", e.Enabled)
		fmt.Fprintf(&sb, "Cooldown: %ds\n", e.CooldownSeconds)
		if !e.LastFiredAt.IsZero() {
			fmt.Fprintf(&sb, "LastFire: %s\n", e.LastFiredAt.Format(time.RFC3339))
		}
		fmt.Fprintf(&sb, "Created:  %s\n", e.CreatedAt.Format(time.RFC3339))
		sb.WriteString("\n")
	}
	return mcpsdk.NewToolResultText(sb.String()), nil
}

func (s *Server) handleExitHookAdd(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.exitHookStore == nil {
		return mcpsdk.NewToolResultText("Exit hook store not available."), nil
	}
	name := req.GetString("name", "")
	action := req.GetString("action", "")
	if name == "" || action == "" {
		return mcpsdk.NewToolResultText("Error: name and action are required."), nil
	}
	if action != "restart" && action != "notify" {
		return mcpsdk.NewToolResultText("Error: action must be 'restart' or 'notify'."), nil
	}
	notifySession := req.GetString("notify_session", "")
	notifyMessage := req.GetString("notify_message", "")
	cooldown := req.GetInt("cooldown_seconds", 300)

	e, err := s.exitHookStore.Add(name, session.ExitHookAction(action), notifySession, notifyMessage, cooldown)
	if err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error: %v", err)), nil
	}
	data, _ := json.MarshalIndent(e, "", "  ")
	return mcpsdk.NewToolResultText(fmt.Sprintf("Exit hook added:\n%s", string(data))), nil
}

func (s *Server) handleExitHookDelete(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.exitHookStore == nil {
		return mcpsdk.NewToolResultText("Exit hook store not available."), nil
	}
	id := req.GetString("id", "")
	if id == "" {
		return mcpsdk.NewToolResultText("Error: id is required."), nil
	}
	if err := s.exitHookStore.Delete(id); err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Exit hook %s deleted.", id)), nil
}

func (s *Server) handleExitHookEnable(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.exitHookStore == nil {
		return mcpsdk.NewToolResultText("Exit hook store not available."), nil
	}
	id := req.GetString("id", "")
	if id == "" {
		return mcpsdk.NewToolResultText("Error: id is required."), nil
	}
	if err := s.exitHookStore.SetEnabled(id, true); err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Exit hook %s enabled.", id)), nil
}

func (s *Server) handleExitHookDisable(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.exitHookStore == nil {
		return mcpsdk.NewToolResultText("Exit hook store not available."), nil
	}
	id := req.GetString("id", "")
	if id == "" {
		return mcpsdk.NewToolResultText("Error: id is required."), nil
	}
	if err := s.exitHookStore.SetEnabled(id, false); err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Exit hook %s disabled.", id)), nil
}
