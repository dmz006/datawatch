// v6.0.4 (BL210-remaining) — MCP coverage gap closures round 2.
//
// Operator-flagged 2026-04-29 audit found ~12 daemon REST endpoints with
// no MCP equivalent. Round 1 (v5.27.8) landed the memory/LLM/RTK/logs
// subset. This file closes the remaining gaps:
//
//   filter_list / filter_add / filter_delete / filter_toggle — detection filter CRUD
//   backends_list / backends_active              — backend info + reachability
//   session_set_state                            — manual state override
//   federation_sessions                          — aggregated sessions from peers
//   device_register / device_list / device_delete — mobile push token registry
//   files_list                                   — directory browser
//
// All forward to /api/* via the proxyJSON helper — no new daemon-side state.

package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ── detection filters ─────────────────────────────────────────────────────────

func (s *Server) toolFilterList() mcpsdk.Tool {
	return mcpsdk.NewTool("filter_list",
		mcpsdk.WithDescription("List all detection filters (pattern/action rules applied to session output). Read-only."),
	)
}
func (s *Server) handleFilterList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodGet, "/api/filters", nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolFilterAdd() mcpsdk.Tool {
	return mcpsdk.NewTool("filter_add",
		mcpsdk.WithDescription("Create a detection filter. Pattern is a regex; action is one of: alert, kill, redact, tag."),
		mcpsdk.WithString("pattern", mcpsdk.Required(), mcpsdk.Description("Regex pattern to match in session output")),
		mcpsdk.WithString("action", mcpsdk.Required(), mcpsdk.Description("Action: alert | kill | redact | tag")),
		mcpsdk.WithString("value", mcpsdk.Description("Optional value for the action (e.g. tag name)")),
	)
}
func (s *Server) handleFilterAdd(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]string{
		"pattern": req.GetString("pattern", ""),
		"action":  req.GetString("action", ""),
		"value":   req.GetString("value", ""),
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/filters", body)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolFilterDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("filter_delete",
		mcpsdk.WithDescription("Delete a detection filter by ID."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Filter ID (from filter_list)")),
	)
}
func (s *Server) handleFilterDelete(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	if id == "" {
		return textOK("Error: id is required"), nil
	}
	out, err := s.proxyJSON(http.MethodDelete, fmt.Sprintf("/api/filters?id=%s", url.QueryEscape(id)), nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolFilterToggle() mcpsdk.Tool {
	return mcpsdk.NewTool("filter_toggle",
		mcpsdk.WithDescription("Enable or disable a detection filter by ID without deleting it."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Filter ID (from filter_list)")),
		mcpsdk.WithBoolean("enabled", mcpsdk.Required(), mcpsdk.Description("true to enable, false to disable")),
	)
}
func (s *Server) handleFilterToggle(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	enabled := req.GetBool("enabled", true)
	body := map[string]interface{}{
		"id":      req.GetString("id", ""),
		"enabled": enabled,
	}
	out, err := s.proxyJSON(http.MethodPatch, "/api/filters", body)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

// ── backends ──────────────────────────────────────────────────────────────────

func (s *Server) toolBackendsList() mcpsdk.Tool {
	return mcpsdk.NewTool("backends_list",
		mcpsdk.WithDescription("List all configured LLM backends with enabled/available status and cached version strings."),
	)
}
func (s *Server) handleBackendsList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodGet, "/api/backends", nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolBackendsActive() mcpsdk.Tool {
	return mcpsdk.NewTool("backends_active",
		mcpsdk.WithDescription("Probe each enabled backend for reachability and return live version strings. Slower than backends_list (makes exec calls); use for health-check purposes."),
	)
}
func (s *Server) handleBackendsActive(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodGet, "/api/backends/active", nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

// ── session state override ────────────────────────────────────────────────────

func (s *Server) toolSessionSetState() mcpsdk.Tool {
	return mcpsdk.NewTool("session_set_state",
		mcpsdk.WithDescription("Manually override a session's state. Valid states: running, waiting_input, complete, killed, failed, rate_limited. Use with care — bypasses normal lifecycle transitions."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Session ID")),
		mcpsdk.WithString("state", mcpsdk.Required(), mcpsdk.Description("New state: running | waiting_input | complete | killed | failed | rate_limited")),
	)
}
func (s *Server) handleSessionSetState(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]string{
		"id":    req.GetString("id", ""),
		"state": req.GetString("state", ""),
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/sessions/state", body)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

// ── federation sessions ───────────────────────────────────────────────────────

func (s *Server) toolFederationSessions() mcpsdk.Tool {
	return mcpsdk.NewTool("federation_sessions",
		mcpsdk.WithDescription("List sessions aggregated from all federated peers. Returns sessions visible across the observer federation mesh."),
		mcpsdk.WithString("peer", mcpsdk.Description("Filter to a specific peer hostname (empty = all peers)")),
		mcpsdk.WithString("state", mcpsdk.Description("Filter by session state (empty = all states)")),
	)
}
func (s *Server) handleFederationSessions(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if v := req.GetString("peer", ""); v != "" {
		q.Set("peer", v)
	}
	if v := req.GetString("state", ""); v != "" {
		q.Set("state", v)
	}
	path := "/api/federation/sessions"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	out, err := s.proxyJSON(http.MethodGet, path, nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

// ── device registry ───────────────────────────────────────────────────────────

func (s *Server) toolDeviceRegister() mcpsdk.Tool {
	return mcpsdk.NewTool("device_register",
		mcpsdk.WithDescription("Register a mobile device for push notifications. Stores the FCM/APNS token linked to an alias."),
		mcpsdk.WithString("alias", mcpsdk.Required(), mcpsdk.Description("Device alias (e.g. 'my-phone')")),
		mcpsdk.WithString("token", mcpsdk.Required(), mcpsdk.Description("FCM or APNS push token")),
		mcpsdk.WithString("platform", mcpsdk.Description("Platform: android | ios (default: android)")),
	)
}
func (s *Server) handleDeviceRegister(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]string{
		"alias":    req.GetString("alias", ""),
		"token":    req.GetString("token", ""),
		"platform": req.GetString("platform", "android"),
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/devices/register", body)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolDeviceList() mcpsdk.Tool {
	return mcpsdk.NewTool("device_list",
		mcpsdk.WithDescription("List all registered push-notification devices (aliases + truncated tokens)."),
	)
}
func (s *Server) handleDeviceList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodGet, "/api/devices", nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolDeviceDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("device_delete",
		mcpsdk.WithDescription("Deregister a device by ID (from device_list)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Device ID from device_list")),
	)
}
func (s *Server) handleDeviceDelete(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	if id == "" {
		return textOK("Error: id is required"), nil
	}
	out, err := s.proxyJSON(http.MethodDelete, fmt.Sprintf("/api/devices/%s", url.PathEscape(id)), nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

// ── file browser ──────────────────────────────────────────────────────────────

func (s *Server) toolFilesList() mcpsdk.Tool {
	return mcpsdk.NewTool("files_list",
		mcpsdk.WithDescription("Browse the operator's configured project directory tree. Returns directory entries with type, size, and modification time."),
		mcpsdk.WithString("path", mcpsdk.Description("Subdirectory path relative to the project root (empty = root)")),
		mcpsdk.WithBoolean("hidden", mcpsdk.Description("Include hidden (dot) files (default false)")),
	)
}
func (s *Server) handleFilesList(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if v := req.GetString("path", ""); v != "" {
		q.Set("path", v)
	}
	if req.GetBool("hidden", false) {
		q.Set("hidden", "true")
	}
	path := "/api/files"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	out, err := s.proxyJSON(http.MethodGet, path, nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}
