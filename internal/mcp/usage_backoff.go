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
		mcpsdk.WithDescription("List the active per-backend usage back-off flags (backend, until, reason, source: rate_limit|operator) the effective config (mode, max_wait_minutes, reserve_for_review, reserve_tokens_per_hour), and the rate-limit pattern-drift guard (patterns_version, patterns_stale, claude_cli_version — the text printed by the claude tool's version command). patterns_stale turns true whenever the effective patterns change since the last operator recheck, and also when the claude tool version text changes. A backed-off backend receives no new autonomous dispatch until the flag clears; interactive session starts are never blocked. The reserve array carries the current-window per-hour reserve usage (backend, tokens_used) for each backed-off backend; it is omitted when the daemon does not run the review-reserve gate or no flag is live."),
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
		mcpsdk.WithDescription("Back off a backend: hold new autonomous dispatch to it until a deadline. Give exactly one of minutes or until (default 30 minutes when neither). Without models the whole backend is held; with a non-empty models list only the named models of the backend are held and the others keep running. Works even when autonomous.usage_backoff is off — that mode only disables the automatic path from rate-limited sessions."),
		mcpsdk.WithString("backend", mcpsdk.Required(), mcpsdk.Description("Backend name, e.g. claude-code")),
		mcpsdk.WithNumber("minutes", mcpsdk.Description("Back off for this many minutes (>= 1). Exclusive with until.")),
		mcpsdk.WithString("until", mcpsdk.Description("Back off until this RFC3339 time (must be in the future). Exclusive with minutes.")),
		mcpsdk.WithString("reason", mcpsdk.Description("Operator-readable reason shown in the wait reason")),
		mcpsdk.WithArray("models", mcpsdk.WithStringItems(), mcpsdk.Description("Optional: hold only these models of the backend (e.g. [\"opus\", \"sonnet\"]); omit to hold the whole backend")),
	)
}

// backoffModelsArg reads the optional models argument of a request. present
// reports whether the argument was given at all (an empty or blank-only list
// is an operator error, not a whole-backend set).
func backoffModelsArg(req mcpsdk.CallToolRequest) (models []string, present bool) {
	raw, ok := req.GetArguments()["models"]
	if !ok {
		return nil, false
	}
	if list, ok := raw.([]any); ok {
		for _, v := range list {
			if m, ok := v.(string); ok {
				if m = strings.TrimSpace(m); m != "" {
					models = append(models, m)
				}
			}
		}
	}
	return models, true
}
func (s *Server) handleUsageBackoffSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	backend := strings.TrimSpace(req.GetString("backend", ""))
	if backend == "" {
		return toolErrorf("usage_backoff_set", "backend is required"), nil
	}
	models, modelsPresent := backoffModelsArg(req)
	if modelsPresent && len(models) == 0 {
		return toolErrorf("usage_backoff_set", "models: at least one model name is required"), nil
	}
	args := req.GetArguments()
	_, hasMinutes := args["minutes"]
	until := strings.TrimSpace(req.GetString("until", ""))
	if hasMinutes && until != "" {
		return toolErrorf("usage_backoff_set", "give exactly one of minutes or until"), nil
	}
	body := map[string]any{"backend": backend}
	if len(models) > 0 {
		body["models"] = models
	}
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
		mcpsdk.WithDescription("Clear the usage back-off flag for one backend, or every flag when backend is omitted or \"all\". With a non-empty models list only the named models are removed from the flag (the flag is dropped when that empties its model list); a clear-all is never model-scoped and whole-backend flags are left alone."),
		mcpsdk.WithString("backend", mcpsdk.Description("Backend to clear; omit or \"all\" clears every flag")),
		mcpsdk.WithArray("models", mcpsdk.WithStringItems(), mcpsdk.Description("Optional: narrow the clear to these models of the backend (requires a named backend)")),
	)
}
func (s *Server) handleUsageBackoffClear(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{}
	backend := strings.TrimSpace(req.GetString("backend", ""))
	if backend != "" {
		body["backend"] = backend
	}
	models, modelsPresent := backoffModelsArg(req)
	if modelsPresent && len(models) == 0 {
		return toolErrorf("usage_backoff_clear", "models: at least one model name is required"), nil
	}
	if modelsPresent && (backend == "" || strings.EqualFold(backend, "all")) {
		return toolErrorf("usage_backoff_clear", "models: name a backend to narrow the clear; a clear-all is not model-scoped"), nil
	}
	if modelsPresent {
		body["models"] = models
	}
	out, err := s.proxyJSON(http.MethodDelete, "/api/usage-backoff", body)
	if err != nil {
		return toolError("usage_backoff_clear", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolUsageBackoffRecheck() mcpsdk.Tool {
	return mcpsdk.NewTool("usage_backoff_recheck",
		mcpsdk.WithDescription("Record the current rate-limit detection pattern fingerprint (patterns_version) and the claude tool version text (claude_cli_version) as rechecked by the operator, which clears patterns_stale; the recheck stores both. Run it after confirming the patterns still match what the backends print; patterns_stale turns true again whenever the effective patterns change or the claude tool version text changes since the last recheck."),
	)
}
func (s *Server) handleUsageBackoffRecheck(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodPost, "/api/usage-backoff/recheck", map[string]any{})
	if err != nil {
		return toolError("usage_backoff_recheck", err), nil
	}
	return textOK(string(out)), nil
}
