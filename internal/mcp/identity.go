// MCP tools for the operator identity / Telos layer. Every tool proxies to
// the REST surface.
//
//	get_identity      — read full identity
//	set_identity      — replace full identity (PUT semantics)
//	update_identity   — merge non-empty fields (PATCH semantics)
//	identity_backups  — list the saved copies, newest first
//	restore_identity  — bring a saved copy back
//	reset_identity    — clear the identity on purpose, keeping a backup

package mcp

import (
	"context"
	"fmt"
	"strings"

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
		mcpsdk.WithDescription("BL257 P1 — REPLACE the full operator identity. Use update_identity for partial merges. A call with no fields, or only empty fields, over a saved identity is refused (HTTP 409 identity_wipe_refused) and nothing is written; to clear the identity on purpose call reset_identity."),
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

func (s *Server) toolIdentityBackups() mcpsdk.Tool {
	return mcpsdk.NewTool("identity_backups",
		mcpsdk.WithDescription("List the saved copies of the operator identity, newest first (name, time, role, field count), plus how many copies are kept. Pass a name from this list to restore_identity."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) toolIdentityRestore() mcpsdk.Tool {
	return mcpsdk.NewTool("restore_identity",
		mcpsdk.WithDescription("Restore the operator identity from a saved copy and return the restored identity. The current identity is backed up first, so a restore can itself be undone. Use identity_backups to see the names."),
		mcpsdk.WithString("backup", mcpsdk.Description("backup name from identity_backups, or \"latest\" (default)")),
	)
}

func (s *Server) toolIdentityReset() mcpsdk.Tool {
	return mcpsdk.NewTool("reset_identity",
		mcpsdk.WithDescription("Clear the operator identity on purpose. This is the only way to empty a saved identity: set_identity with no fields is refused. A backup of what was cleared is kept, so restore_identity can undo it."),
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
		return identityWriteError("set_identity", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) handleIdentityConfigure(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	return textOK(`Identity setup wizard:

  PWA: click the robot icon (🤖) in the header.
  CLI: run "datawatch identity configure" — interactive 6-step prompt.
  REST: PUT /api/identity (full doc) or PATCH /api/identity (merge).
  MCP:  call set_identity (full) or update_identity (merge) directly.

A write with no content over a saved identity is refused. Every write keeps a
timestamped backup: identity_backups lists them and restore_identity brings one
back (default "latest"). reset_identity clears the identity on purpose and keeps
a backup, so restore_identity can undo it.

Fields: role, north_star_goals[], current_projects[], values[], current_focus, context_notes.`), nil
}

func (s *Server) handleIdentityUpdate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := identityBodyFromReq(req)
	out, err := s.proxyJSON("PATCH", "/api/identity", body)
	if err != nil {
		return identityWriteError("update_identity", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) handleIdentityBackups(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/identity/backups", nil)
	if err != nil {
		return toolError("identity_backups", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) handleIdentityRestore(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	backup := strings.TrimSpace(optString(req, "backup"))
	if backup == "" {
		backup = "latest"
	}
	out, err := s.proxyJSON("POST", "/api/identity/restore", map[string]any{"backup": backup})
	if err != nil {
		return toolError("restore_identity", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) handleIdentityReset(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON("POST", "/api/identity/reset", nil)
	if err != nil {
		return toolError("reset_identity", err), nil
	}
	return textOK(string(out)), nil
}

// identityWriteError turns a failed identity write into a tool error that
// carries the daemon's answer. proxyJSON already puts the HTTP status and
// body in err; for the refusal of an empty overwrite (HTTP 409
// identity_wipe_refused) the caller also learns the MCP route to clear the
// identity on purpose, since the body names the REST route.
func identityWriteError(op string, err error) *mcpsdk.CallToolResult {
	if strings.Contains(err.Error(), "identity_wipe_refused") {
		err = fmt.Errorf("%w. Nothing was written. To clear the identity on purpose call reset_identity; a backup is kept and restore_identity undoes it", err)
	}
	return toolError(op, err)
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
