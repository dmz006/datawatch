// MCP tools for Automation records (a chain of Automatons with per-outcome
// edges). Mirrors internal/mcp/retry_tools.go's shape exactly.
//
// Automation records currently store data only — no automatic advance yet.
//
// Tools:
//   automation_list   — list Automations
//   automation_get    — get one Automation by ID
//   automation_create — create an Automation
//   automation_update — update an Automation
//   automation_delete — delete an Automation

package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ── automation_list ─────────────────────────────────────────────────────

func (s *Server) toolAutomationList() mcpsdk.Tool {
	return mcpsdk.NewTool("automation_list",
		mcpsdk.WithDescription("List all Automations. An Automation is a chain of Automatons (PRDs) connected by per-outcome edges. Automation records currently store data only — no automatic advance yet."),
	)
}

func (s *Server) handleAutomationList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	data, err := s.proxyJSON(http.MethodGet, "/api/automations", nil)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

// ── automation_create ───────────────────────────────────────────────────

func (s *Server) toolAutomationCreate() mcpsdk.Tool {
	return mcpsdk.NewTool("automation_create",
		mcpsdk.WithDescription("Create a new Automation — a named chain of Automatons with per-outcome edges, an entry member, optional budgets and execution/verification defaults. The record currently stores data only; nothing advances automatically yet."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Automation name.")),
		mcpsdk.WithString("description", mcpsdk.Description("Optional description.")),
		mcpsdk.WithString("entry_member_id", mcpsdk.Description("Member ID where the chain starts.")),
		mcpsdk.WithString("members_json", mcpsdk.Description(`JSON array of members; each member has: id, prd_id, on_complete, on_blocked, on_failed (member IDs or "" = stop), added_reason`)),
		mcpsdk.WithString("budget_policy_json", mcpsdk.Description("Optional JSON object — budget policy for this Automation.")),
		mcpsdk.WithString("project_dir", mcpsdk.Description("Optional project directory (informational).")),
		mcpsdk.WithString("backend", mcpsdk.Description("Optional execution backend default.")),
		mcpsdk.WithString("model", mcpsdk.Description("Optional execution model default.")),
		mcpsdk.WithString("effort", mcpsdk.Description("Optional execution effort default.")),
		mcpsdk.WithString("verification_backend", mcpsdk.Description("Optional verification backend default.")),
		mcpsdk.WithString("verification_model", mcpsdk.Description("Optional verification model default.")),
		mcpsdk.WithString("verification_effort", mcpsdk.Description("Optional verification effort default.")),
	)
}

func (s *Server) handleAutomationCreate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	desc := req.GetString("description", "")
	entry := req.GetString("entry_member_id", "")
	var members []map[string]any
	_ = json.Unmarshal([]byte(req.GetString("members_json", "[]")), &members)
	var budget map[string]any
	_ = json.Unmarshal([]byte(req.GetString("budget_policy_json", "{}")), &budget)
	body := map[string]any{
		"name": name, "description": desc, "entry_member_id": entry,
		"members": members, "budget_policy": budget,
		"project_dir":          req.GetString("project_dir", ""),
		"backend":              req.GetString("backend", ""),
		"model":                req.GetString("model", ""),
		"effort":               req.GetString("effort", ""),
		"verification_backend": req.GetString("verification_backend", ""),
		"verification_model":   req.GetString("verification_model", ""),
		"verification_effort":  req.GetString("verification_effort", ""),
	}
	data, err := s.proxyJSON(http.MethodPost, "/api/automations", body)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

// ── automation_get ──────────────────────────────────────────────────────

func (s *Server) toolAutomationGet() mcpsdk.Tool {
	return mcpsdk.NewTool("automation_get",
		mcpsdk.WithDescription("Get an Automation by ID (status, members, edges, budget policy, execution defaults)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automation ID.")),
	)
}

func (s *Server) handleAutomationGet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	data, err := s.proxyJSON(http.MethodGet, "/api/automations/"+id, nil)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

// ── automation_update ───────────────────────────────────────────────────

func (s *Server) toolAutomationUpdate() mcpsdk.Tool {
	return mcpsdk.NewTool("automation_update",
		mcpsdk.WithDescription("Update an Automation (name, description, entry member, members, budgets, defaults). Provide only the fields to change. The record currently stores data only; nothing advances automatically yet."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automation ID.")),
		mcpsdk.WithNumber("expected_version", mcpsdk.Description("Optional — the version you read; the update is rejected if the record moved since (optimistic concurrency).")),
		mcpsdk.WithString("reason", mcpsdk.Description("Reason for this update (recorded alongside the change).")),
		mcpsdk.WithString("name", mcpsdk.Description("New name.")),
		mcpsdk.WithString("description", mcpsdk.Description("New description.")),
		mcpsdk.WithString("entry_member_id", mcpsdk.Description("New entry member ID.")),
		mcpsdk.WithString("members_json", mcpsdk.Description(`New members as a JSON array; each member has: id, prd_id, on_complete, on_blocked, on_failed (member IDs or "" = stop), added_reason`)),
		mcpsdk.WithString("budget_policy_json", mcpsdk.Description("New budget policy as a JSON object.")),
		mcpsdk.WithString("project_dir", mcpsdk.Description("New project directory.")),
		mcpsdk.WithString("backend", mcpsdk.Description("New execution backend default.")),
		mcpsdk.WithString("model", mcpsdk.Description("New execution model default.")),
		mcpsdk.WithString("effort", mcpsdk.Description("New execution effort default.")),
		mcpsdk.WithString("verification_backend", mcpsdk.Description("New verification backend default.")),
		mcpsdk.WithString("verification_model", mcpsdk.Description("New verification model default.")),
		mcpsdk.WithString("verification_effort", mcpsdk.Description("New verification effort default.")),
	)
}

func (s *Server) handleAutomationUpdate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	name := req.GetString("name", "")
	desc := req.GetString("description", "")
	entry := req.GetString("entry_member_id", "")
	var members []map[string]any
	_ = json.Unmarshal([]byte(req.GetString("members_json", "[]")), &members)
	var budget map[string]any
	_ = json.Unmarshal([]byte(req.GetString("budget_policy_json", "{}")), &budget)
	body := map[string]any{
		"name": name, "description": desc, "entry_member_id": entry,
		"members": members, "budget_policy": budget,
		"reason":               req.GetString("reason", ""),
		"project_dir":          req.GetString("project_dir", ""),
		"backend":              req.GetString("backend", ""),
		"model":                req.GetString("model", ""),
		"effort":               req.GetString("effort", ""),
		"verification_backend": req.GetString("verification_backend", ""),
		"verification_model":   req.GetString("verification_model", ""),
		"verification_effort":  req.GetString("verification_effort", ""),
	}
	if v, ok := req.GetArguments()["expected_version"]; ok && v != nil {
		body["expected_version"] = v
	}
	data, err := s.proxyJSON(http.MethodPut, "/api/automations/"+id, body)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

// ── automation_delete ───────────────────────────────────────────────────

func (s *Server) toolAutomationDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("automation_delete",
		mcpsdk.WithDescription("Delete an Automation by ID. Its member Automatons are not affected — the back-reference is cleared and each keeps running or sitting as an ordinary Automaton."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automation ID.")),
	)
}

func (s *Server) handleAutomationDelete(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	_, err := s.proxyJSON(http.MethodDelete, "/api/automations/"+id, nil)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(`{"status":"deleted"}`), nil
}
