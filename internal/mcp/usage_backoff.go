// BL428 Phase 4 — usage back-off MCP tools. They proxy the loopback REST
// surface (/api/usage-backoff) so argument names and validation match
// REST, the CLI and the comm channel exactly.

package mcp

import (
	"context"
	"net/http"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolUsageBackoffStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("usage_backoff_status",
		mcpsdk.WithDescription("List the active per-backend usage back-off flags (backend, until, reason, source: rate_limit|operator) and the effective config (mode, max_wait_minutes). A backed-off backend receives no new autonomous dispatch until the flag clears; interactive session starts are never blocked."),
	)
}
func (s *Server) handleUsageBackoffStatus(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/usage-backoff", nil)
	if err != nil {
		return toolError("usage_backoff_status", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolUsageBackoffSet() mcpsdk.Tool {
	return mcpsdk.NewTool("usage_backoff_set",
		mcpsdk.WithDescription("Back off a backend: hold new autonomous dispatch to it until a deadline. Give exactly one of minutes or until (default 30 minutes when neither). Works even when autonomous.usage_backoff is off — that mode only disables the automatic path from rate-limited sessions."),
		mcpsdk.WithString("backend", mcpsdk.Required(), mcpsdk.Description("Backend name, e.g. claude-code")),
		mcpsdk.WithNumber("minutes", mcpsdk.Description("Back off for this many minutes (>= 1). Exclusive with until.")),
		mcpsdk.WithString("until", mcpsdk.Description("Back off until this RFC3339 time (must be in the future). Exclusive with minutes.")),
		mcpsdk.WithString("reason", mcpsdk.Description("Operator-readable reason shown in the wait reason")),
	)
}
func (s *Server) handleUsageBackoffSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	backend := strings.TrimSpace(req.GetString("backend", ""))
	if backend == "" {
		return toolErrorf("usage_backoff_set", "backend is required"), nil
	}
	args := req.GetArguments()
	_, hasMinutes := args["minutes"]
	until := strings.TrimSpace(req.GetString("until", ""))
	if hasMinutes && until != "" {
		return toolErrorf("usage_backoff_set", "give exactly one of minutes or until"), nil
	}
	body := map[string]any{"backend": backend}
	if hasMinutes {
		minutes := int(req.GetFloat("minutes", 0))
		if minutes < 1 {
			return toolErrorf("usage_backoff_set", "minutes must be >= 1"), nil
		}
		body["minutes"] = minutes
	}
	if until != "" {
		body["until"] = until
	}
	if reason := strings.TrimSpace(req.GetString("reason", "")); reason != "" {
		body["reason"] = reason
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/usage-backoff", body)
	if err != nil {
		return toolError("usage_backoff_set", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolUsageBackoffClear() mcpsdk.Tool {
	return mcpsdk.NewTool("usage_backoff_clear",
		mcpsdk.WithDescription("Clear the usage back-off flag for one backend, or every flag when backend is omitted or \"all\"."),
		mcpsdk.WithString("backend", mcpsdk.Description("Backend to clear; omit or \"all\" clears every flag")),
	)
}
func (s *Server) handleUsageBackoffClear(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{}
	if backend := strings.TrimSpace(req.GetString("backend", "")); backend != "" {
		body["backend"] = backend
	}
	out, err := s.proxyJSON(http.MethodDelete, "/api/usage-backoff", body)
	if err != nil {
		return toolError("usage_backoff_clear", err), nil
	}
	return textOK(string(out)), nil
}
