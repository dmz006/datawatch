// BL257 Phase 1 v6.8.0 — MCP tools for the operator identity / Telos
// layer. All three tools proxy to the REST surface.
//
//	get_identity     — read full identity
//	set_identity     — replace full identity (PUT semantics)
//	update_identity  — merge non-empty fields (PATCH semantics)

package mcp

import (
	"context"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ── Tool descriptors ────────────────────────────────────────────────────

func (s *Server) toolIdentityGet() mcpsdk.Tool {
	return mcpsdk.NewTool("get_identity",
		mcpsdk.WithDescription("BL257 P1 — read the operator identity / Telos document (role, north-star goals, current projects, values, current focus, context notes)."),
	)
}

func (s *Server) toolIdentitySet() mcpsdk.Tool {
	return mcpsdk.NewTool("set_identity",
		mcpsdk.WithDescription("BL257 P1 — REPLACE the full operator identity. Use update_identity for partial merges. Empty fields are cleared."),
		mcpsdk.WithString("role", mcpsdk.Description("operator role / job title")),
		mcpsdk.WithString("current_focus", mcpsdk.Description("what the operator is focused on right now")),
		mcpsdk.WithString("context_notes", mcpsdk.Description("free-form notes the LLM should know")),
		// list fields can't be array-typed easily across MCP SDK
		// versions — accept JSON arrays as strings or arrays via Args().
		mcpsdk.WithArray("north_star_goals", mcpsdk.Description("long-term goals (array of strings)")),
		mcpsdk.WithArray("current_projects", mcpsdk.Description("active projects (array of strings)")),
		mcpsdk.WithArray("values", mcpsdk.Description("operator values (array of strings)")),
	)
}

func (s *Server) toolIdentityUpdate() mcpsdk.Tool {
	return mcpsdk.NewTool("update_identity",
		mcpsdk.WithDescription("BL257 P1 — MERGE non-empty fields into the operator identity. Empty / omitted fields are preserved."),
		mcpsdk.WithString("role", mcpsdk.Description("operator role / job title")),
		mcpsdk.WithString("current_focus", mcpsdk.Description("what the operator is focused on right now")),
		mcpsdk.WithString("context_notes", mcpsdk.Description("free-form notes the LLM should know")),
		mcpsdk.WithArray("north_star_goals", mcpsdk.Description("long-term goals (array of strings) — non-empty replaces the existing list")),
		mcpsdk.WithArray("current_projects", mcpsdk.Description("active projects (array of strings)")),
		mcpsdk.WithArray("values", mcpsdk.Description("operator values (array of strings)")),
	)
}

func (s *Server) toolIdentityConfigure() mcpsdk.Tool {
	return mcpsdk.NewTool("configure_identity",
		mcpsdk.WithDescription("BL257 P2 v6.8.1 — instructions for running the operator identity setup wizard. The interactive flow lives in the PWA (robot icon in header) and the CLI (`datawatch identity configure`); this tool returns guidance because MCP itself is stateless and can't run a multi-step interview."),
	)
}

// ── Handlers ────────────────────────────────────────────────────────────

func (s *Server) handleIdentityGet(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/identity", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleIdentitySet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := identityBodyFromReq(req)
	out, err := s.proxyJSON("PUT", "/api/identity", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleIdentityConfigure(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	return textOK(`Identity setup wizard:

  PWA: click the robot icon (🤖) in the header.
  CLI: run "datawatch identity configure" — interactive 6-step prompt.
  REST: PUT /api/identity (full doc) or PATCH /api/identity (merge).
  MCP:  call set_identity (full) or update_identity (merge) directly.

Fields: role, north_star_goals[], current_projects[], values[], current_focus, context_notes.`), nil
}

func (s *Server) handleIdentityUpdate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := identityBodyFromReq(req)
	out, err := s.proxyJSON("PATCH", "/api/identity", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// identityBodyFromReq pulls every known field out of a CallToolRequest
// and returns a clean JSON-friendly map. List fields use optStringArray
// to accept either []any or comma-separated string forms.
func identityBodyFromReq(req mcpsdk.CallToolRequest) map[string]any {
	body := map[string]any{}
	if v := optString(req, "role"); v != "" {
		body["role"] = v
	}
	if v := optString(req, "current_focus"); v != "" {
		body["current_focus"] = v
	}
	if v := optString(req, "context_notes"); v != "" {
		body["context_notes"] = v
	}
	if arr := splitCSV(optString(req, "north_star_goals")); len(arr) > 0 {
		body["north_star_goals"] = arr
	}
	if arr := splitCSV(optString(req, "current_projects")); len(arr) > 0 {
		body["current_projects"] = arr
	}
	if arr := splitCSV(optString(req, "values")); len(arr) > 0 {
		body["values"] = arr
	}
	return body
}
