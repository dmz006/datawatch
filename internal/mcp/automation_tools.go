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

// automationProxyBody converts the MCP tool arguments into a REST request
// body, forwarding only the fields the caller actually provided. Empty
// strings, nulls, an empty members array and an empty budget object are
// dropped so the server treats them as "unchanged" (partial updates) rather
// than as explicit empty values, which would fail server-side validation
// (e.g. a zero-valued budget policy has no ceiling type).
func automationProxyBody(req mcpsdk.CallToolRequest) map[string]any {
	body := map[string]any{}
	args := req.GetArguments()
	for k, v := range args {
		if v == nil {
			continue
		}
		switch k {
		case "members_json":
			var m []map[string]any
			if err := json.Unmarshal([]byte(asJSONString(v)), &m); err == nil && len(m) > 0 {
				body["members"] = m
			}
		case "budget_policy_json":
			var b map[string]any
			if err := json.Unmarshal([]byte(asJSONString(v)), &b); err == nil && len(b) > 0 {
				body["budget_policy"] = b
			}
		case "id":
			// path parameter, not part of the body
		default:
			if str, ok := v.(string); ok && str == "" {
				continue
			}
			body[k] = v
		}
	}
	return body
}

func asJSONString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func (s *Server) handleAutomationCreate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := automationProxyBody(req)
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
	body := automationProxyBody(req)
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
