// v6.0.8 (BL219) — MCP surface for tooling artifact lifecycle.
//
// Three tools forward to the /api/tooling/* endpoints:
//
//   tooling_status   — query artifact presence + gitignore state for a backend
//   tooling_gitignore — append backend patterns to .gitignore (+ .cfignore/.dockerignore)
//   tooling_cleanup   — remove ephemeral backend artifacts from project dir

package mcp

import (
	"context"
	"net/http"
	"net/url"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ── tooling_status ─────────────────────────────────────────────────────────────

func (s *Server) toolToolingStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("tooling_status",
		mcpsdk.WithDescription("Report which LLM backend artifact files (aider cache, goose sessions, etc.) are present in a project directory and whether they are already in .gitignore."),
		mcpsdk.WithString("project_dir", mcpsdk.Description("Absolute path of the project to inspect. Defaults to session.default_project_dir.")),
		mcpsdk.WithString("backend", mcpsdk.Description("Specific backend to query (claude-code, opencode, aider, goose, gemini). Omit for all backends.")),
	)
}

func (s *Server) handleToolingStatus(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if pd := req.GetString("project_dir", ""); pd != "" {
		q.Set("project_dir", pd)
	}
	if b := req.GetString("backend", ""); b != "" {
		q.Set("backend", b)
	}
	path := "/api/tooling/status"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	out, err := s.proxyJSON(http.MethodGet, path, nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

// ── tooling_gitignore ──────────────────────────────────────────────────────────

func (s *Server) toolToolingGitignore() mcpsdk.Tool {
	return mcpsdk.NewTool("tooling_gitignore",
		mcpsdk.WithDescription("Append a backend's artifact patterns to .gitignore (and .cfignore/.dockerignore if present) in the project directory. Idempotent."),
		mcpsdk.WithString("project_dir", mcpsdk.Description("Absolute path to the project. Defaults to session.default_project_dir.")),
		mcpsdk.WithString("backend", mcpsdk.Required(), mcpsdk.Description("Backend whose patterns to add: claude-code, opencode, aider, goose, gemini.")),
	)
}

func (s *Server) handleToolingGitignore(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]string{}
	if pd := req.GetString("project_dir", ""); pd != "" {
		body["project_dir"] = pd
	}
	if b := req.GetString("backend", ""); b != "" {
		body["backend"] = b
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/tooling/gitignore", body)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

// ── tooling_cleanup ────────────────────────────────────────────────────────────

func (s *Server) toolToolingCleanup() mcpsdk.Tool {
	return mcpsdk.NewTool("tooling_cleanup",
		mcpsdk.WithDescription("Remove ephemeral LLM backend artifact files (aider cache, goose session JSONLs, etc.) from a project directory."),
		mcpsdk.WithString("project_dir", mcpsdk.Description("Absolute path to the project. Defaults to session.default_project_dir.")),
		mcpsdk.WithString("backend", mcpsdk.Required(), mcpsdk.Description("Backend whose artifacts to remove: aider, goose, gemini, opencode, claude-code.")),
	)
}

func (s *Server) handleToolingCleanup(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]string{}
	if pd := req.GetString("project_dir", ""); pd != "" {
		body["project_dir"] = pd
	}
	if b := req.GetString("backend", ""); b != "" {
		body["backend"] = b
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/tooling/cleanup", body)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}
