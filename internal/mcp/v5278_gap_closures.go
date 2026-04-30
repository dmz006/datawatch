// v5.27.8 (BL210) — daemon MCP coverage gap closures.
//
// Operator-flagged 2026-04-29 audit identified ~12 daemon REST
// endpoints with no MCP equivalent. This file lands the
// operator-priority subset: memory_wal / memory_test_embedder /
// memory_wakeup (the three flagged memory gaps) + claude_models /
// claude_efforts / claude_permission_modes (v5.27.5 LLM listing
// endpoints) + the RTK quartet (rtk_version / rtk_check / rtk_update
// / rtk_discover) + daemon_logs.
//
// All forward to the matching /api/* path via the existing
// proxyJSON helper — no new daemon-side state.

package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ── memory subsystem (operator priority) ─────────────────────────────────────

func (s *Server) toolMemoryWAL() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_wal",
		mcpsdk.WithDescription("Tail of the memory write-ahead log — operator inspection of save/delete/prune events. Read-only."),
		mcpsdk.WithNumber("n", mcpsdk.Description("Number of recent entries (default 50)")),
	)
}
func (s *Server) handleMemoryWAL(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	n := req.GetInt("n", 50)
	out, err := s.proxyJSON(http.MethodGet, fmt.Sprintf("/api/memory/wal?n=%d", n), nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolMemoryTestEmbedder() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_test_embedder",
		mcpsdk.WithDescription("Probe the configured embedder's reachability + dimension. Useful before enabling memory or after switching providers."),
	)
}
func (s *Server) handleMemoryTestEmbedder(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodPost, "/api/memory/test", nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolMemoryWakeup() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_wakeup",
		mcpsdk.WithDescription("Compose the L0+L1+L4+L5 wake-up bundle for a project (and optional agent). Read-only — returns what an agent would see at session start."),
		mcpsdk.WithString("project_dir", mcpsdk.Description("Project directory (empty = default project)")),
		mcpsdk.WithString("agent_id", mcpsdk.Description("Optional self agent ID (adds L4+L5)")),
		mcpsdk.WithString("parent_agent_id", mcpsdk.Description("Optional parent agent ID (adds L4)")),
		mcpsdk.WithString("parent_namespace", mcpsdk.Description("Optional parent agent namespace")),
	)
}
func (s *Server) handleMemoryWakeup(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if v := req.GetString("project_dir", ""); v != "" {
		q.Set("project_dir", v)
	}
	if v := req.GetString("agent_id", ""); v != "" {
		q.Set("agent_id", v)
	}
	if v := req.GetString("parent_agent_id", ""); v != "" {
		q.Set("parent_agent_id", v)
	}
	if v := req.GetString("parent_namespace", ""); v != "" {
		q.Set("parent_namespace", v)
	}
	path := "/api/memory/wakeup"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	out, err := s.proxyJSON(http.MethodGet, path, nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

// ── claude listing endpoints (v5.27.5) ───────────────────────────────────────

func (s *Server) toolClaudeModels() mcpsdk.Tool {
	return mcpsdk.NewTool("claude_models",
		mcpsdk.WithDescription("Available claude-code model aliases + full names. Hardcoded list refreshed each major release per AGENT.md § Major release alias refresh."),
	)
}
func (s *Server) handleClaudeModels(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodGet, "/api/llm/claude/models", nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolClaudeEfforts() mcpsdk.Tool {
	return mcpsdk.NewTool("claude_efforts",
		mcpsdk.WithDescription("claude-code --effort enum — low | medium | high | xhigh | max."),
	)
}
func (s *Server) handleClaudeEfforts(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodGet, "/api/llm/claude/efforts", nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolClaudePermissionModes() mcpsdk.Tool {
	return mcpsdk.NewTool("claude_permission_modes",
		mcpsdk.WithDescription("claude-code --permission-mode enum — default | plan | acceptEdits | auto | bypassPermissions | dontAsk. Use 'plan' for design-only PRD sessions."),
	)
}
func (s *Server) handleClaudePermissionModes(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodGet, "/api/llm/claude/permission_modes", nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

// ── RTK quartet ──────────────────────────────────────────────────────────────

func (s *Server) toolRTKVersion() mcpsdk.Tool {
	return mcpsdk.NewTool("rtk_version",
		mcpsdk.WithDescription("Cached RTK token-tracker version + last-check status."),
	)
}
func (s *Server) handleRTKVersion(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodGet, "/api/rtk/version", nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolRTKCheck() mcpsdk.Tool {
	return mcpsdk.NewTool("rtk_check",
		mcpsdk.WithDescription("Trigger a fresh RTK version check now (returns the result without installing)."),
	)
}
func (s *Server) handleRTKCheck(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodPost, "/api/rtk/check", nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolRTKUpdate() mcpsdk.Tool {
	return mcpsdk.NewTool("rtk_update",
		mcpsdk.WithDescription("Install the latest RTK version. Daemon runs the install.sh one-liner in the background."),
	)
}
func (s *Server) handleRTKUpdate(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodPost, "/api/rtk/update", nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolRTKDiscover() mcpsdk.Tool {
	return mcpsdk.NewTool("rtk_discover",
		mcpsdk.WithDescription("Walk recent Claude Code sessions for missed RTK usage opportunities."),
	)
}
func (s *Server) handleRTKDiscover(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodGet, "/api/rtk/discover", nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

// ── daemon logs ──────────────────────────────────────────────────────────────

func (s *Server) toolDaemonLogs() mcpsdk.Tool {
	return mcpsdk.NewTool("daemon_logs",
		mcpsdk.WithDescription("Tail of daemon.log — useful when debugging session issues from an IDE."),
		mcpsdk.WithNumber("n", mcpsdk.Description("Number of recent lines (default 100)")),
	)
}
func (s *Server) handleDaemonLogs(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	n := req.GetInt("n", 100)
	out, err := s.proxyJSON(http.MethodGet, fmt.Sprintf("/api/logs?n=%d", n), nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

// Registration of all tools in this file is inlined into server.go
// SetMemoryAPI alongside the rest of the memory tool block — keeps
// the registration pattern uniform with the rest of the file.
