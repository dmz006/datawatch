// v7.0.0 alpha.5.x — MCP tools for the scope-hierarchy memory model.

package mcp

import (
	"context"
	"strconv"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolMemoryScopeRecall() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_scope_recall",
		mcpsdk.WithDescription("v7.0.0 — walk every scope top-down (persona-global → persona-in-project → project-shared → session-local) and return merged results with layer attribution."),
		mcpsdk.WithString("persona", mcpsdk.Description("persona name (omit to skip persona-* layers)")),
		mcpsdk.WithString("project", mcpsdk.Description("project dir")),
		mcpsdk.WithString("session", mcpsdk.Description("session id (omit to skip session-local)")),
		mcpsdk.WithString("top_k", mcpsdk.Description("max results per layer; default 10")),
	)
}

func (s *Server) toolMemoryScopeBorrow() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_scope_borrow",
		mcpsdk.WithDescription("v7.0.0 — read-only query against another scope (no copy). Useful for 'marketing session wants to peek at web-project session's learnings'."),
		mcpsdk.WithString("scope", mcpsdk.Required(), mcpsdk.Description("persona-global|persona-in-project|project-shared|session-local")),
		mcpsdk.WithString("persona", mcpsdk.Description("for persona-* scopes")),
		mcpsdk.WithString("project", mcpsdk.Description("for project-* / session-local")),
		mcpsdk.WithString("session", mcpsdk.Description("for session-local")),
		mcpsdk.WithString("top_k", mcpsdk.Description("max results; default 10")),
	)
}

func (s *Server) toolMemoryScopeSeed() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_scope_seed",
		mcpsdk.WithDescription("v7.0.0 — copy entries from one scope to another (operator-curated, with optional filter). Adds breadcrumb suffix to copied entries."),
		mcpsdk.WithString("from_scope", mcpsdk.Required()),
		mcpsdk.WithString("from_persona", mcpsdk.Description("source persona for persona-* scopes")),
		mcpsdk.WithString("from_project", mcpsdk.Description("source project")),
		mcpsdk.WithString("from_session", mcpsdk.Description("source session id")),
		mcpsdk.WithString("to_scope", mcpsdk.Required()),
		mcpsdk.WithString("to_persona", mcpsdk.Description("target persona")),
		mcpsdk.WithString("to_project", mcpsdk.Description("target project")),
		mcpsdk.WithString("to_session", mcpsdk.Description("target session id")),
		mcpsdk.WithString("role_prefix", mcpsdk.Description("filter: only entries with role matching this prefix (e.g. persona/security-skeptic)")),
		mcpsdk.WithString("content_substring", mcpsdk.Description("filter: case-insensitive content substring")),
		mcpsdk.WithString("limit", mcpsdk.Description("max entries to copy; default 100")),
	)
}

func (s *Server) toolMemoryScopePromote() mcpsdk.Tool {
	return mcpsdk.NewTool("memory_scope_promote",
		mcpsdk.WithDescription("v7.0.0 — move a memory up the scope hierarchy preserving breadcrumb provenance {session, persona, run, promoted_at, promoted_by}."),
		mcpsdk.WithString("memory_id", mcpsdk.Required(), mcpsdk.Description("source memory id")),
		mcpsdk.WithString("from_scope", mcpsdk.Required()),
		mcpsdk.WithString("from_persona", mcpsdk.Description("source persona")),
		mcpsdk.WithString("from_project", mcpsdk.Description("source project")),
		mcpsdk.WithString("from_session", mcpsdk.Description("source session id")),
		mcpsdk.WithString("to_scope", mcpsdk.Required()),
		mcpsdk.WithString("to_persona", mcpsdk.Description("target persona")),
		mcpsdk.WithString("to_project", mcpsdk.Description("target project")),
		mcpsdk.WithString("to_session", mcpsdk.Description("target session id")),
		mcpsdk.WithString("promoted_by", mcpsdk.Description("operator|<persona-name>; default 'operator'")),
		mcpsdk.WithString("persona", mcpsdk.Description("persona attribution for breadcrumb")),
		mcpsdk.WithString("run", mcpsdk.Description("run id attribution for breadcrumb")),
	)
}

func (s *Server) handleMemoryScopeRecallMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := "?persona=" + optString(req, "persona") +
		"&project=" + optString(req, "project") +
		"&session=" + optString(req, "session") +
		"&top_k=" + optString(req, "top_k")
	out, err := s.proxyGet("/api/memory/scopes/recall"+q, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleMemoryScopeBorrowMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := "?scope=" + mustString(req, "scope") +
		"&persona=" + optString(req, "persona") +
		"&project=" + optString(req, "project") +
		"&session=" + optString(req, "session") +
		"&top_k=" + optString(req, "top_k")
	out, err := s.proxyGet("/api/memory/scopes/borrow"+q, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleMemoryScopeSeedMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	limit, _ := strconv.Atoi(optString(req, "limit"))
	if limit == 0 {
		limit = 100
	}
	body := map[string]any{
		"from": map[string]any{
			"scope":      mustString(req, "from_scope"),
			"persona":    optString(req, "from_persona"),
			"project":    optString(req, "from_project"),
			"session_id": optString(req, "from_session"),
		},
		"to": map[string]any{
			"scope":      mustString(req, "to_scope"),
			"persona":    optString(req, "to_persona"),
			"project":    optString(req, "to_project"),
			"session_id": optString(req, "to_session"),
		},
		"filter": map[string]any{
			"role_prefix":       optString(req, "role_prefix"),
			"content_substring": optString(req, "content_substring"),
		},
		"limit": limit,
	}
	out, err := s.proxyJSON("POST", "/api/memory/scopes/seed", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleMemoryScopePromoteMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	memID, _ := strconv.ParseInt(mustString(req, "memory_id"), 10, 64)
	body := map[string]any{
		"memory_id": memID,
		"from": map[string]any{
			"scope":      mustString(req, "from_scope"),
			"persona":    optString(req, "from_persona"),
			"project":    optString(req, "from_project"),
			"session_id": optString(req, "from_session"),
		},
		"to": map[string]any{
			"scope":      mustString(req, "to_scope"),
			"persona":    optString(req, "to_persona"),
			"project":    optString(req, "to_project"),
			"session_id": optString(req, "to_session"),
		},
		"breadcrumb": map[string]any{
			"persona":     optString(req, "persona"),
			"run":         optString(req, "run"),
			"promoted_by": optString(req, "promoted_by"),
		},
	}
	out, err := s.proxyJSON("POST", "/api/memory/scopes/promote", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
