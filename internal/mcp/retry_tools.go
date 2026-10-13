// BL410 Phase 1 — MCP tools for retry profiles + per-Automaton retry
// policy. Mirrors internal/mcp/guardrail_tools.go's shape exactly.
//
// Tools:
//   retry_profile_list       — list retry profiles
//   retry_profile_create     — create a profile
//   retry_profile_get        — get one profile by ID
//   retry_profile_update     — update a profile
//   retry_profile_delete     — delete a profile
//   per_automaton_retry_policy_set — set per-Automaton retry-policy override

package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ── retry_profile_list ──────────────────────────────────────────────────

func (s *Server) toolRetryProfileList() mcpsdk.Tool {
	return mcpsdk.NewTool("retry_profile_list",
		mcpsdk.WithDescription("List all retry profiles (BL410)."),
	)
}

func (s *Server) handleRetryProfileList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	data, err := s.proxyJSON(http.MethodGet, "/api/autonomous/retry_profiles", nil)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

// ── retry_profile_create ────────────────────────────────────────────────

func (s *Server) toolRetryProfileCreate() mcpsdk.Tool {
	return mcpsdk.NewTool("retry_profile_create",
		mcpsdk.WithDescription("Create a new retry profile (BL410) — controls per-category retry budgets and behavior (ceiling, infra escalation threshold, content auto-fix, auto-fix approval-skip)."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Profile name.")),
		mcpsdk.WithString("description", mcpsdk.Description("Optional description.")),
		mcpsdk.WithString("budgets_json", mcpsdk.Description(`JSON object of category -> retry count, e.g. {"infra":10,"content":5} — categories: infra, content, scope, policy`)),
		mcpsdk.WithNumber("ceiling_total", mcpsdk.Description("Total auto-retry budget summed across every category for this profile (0 = inherit global).")),
		mcpsdk.WithNumber("infra_retry_escalate_after", mcpsdk.Description("Same-config infra retries before escalating to the global backend/model default (0 = inherit global).")),
		mcpsdk.WithBoolean("content_auto_fix_enabled", mcpsdk.Description("Whether a content-category guardrail block gets auto-corrective retry under this profile.")),
		mcpsdk.WithBoolean("auto_fix_skip_approval", mcpsdk.Description("Whether synthesized corrective work bypasses per-story approval under this profile.")),
	)
}

func (s *Server) handleRetryProfileCreate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	desc := req.GetString("description", "")
	var budgets map[string]int
	_ = json.Unmarshal([]byte(req.GetString("budgets_json", "{}")), &budgets)
	body := map[string]any{
		"name": name, "description": desc, "budgets": budgets,
		"ceiling_total":              int(req.GetFloat("ceiling_total", 0)),
		"infra_retry_escalate_after": int(req.GetFloat("infra_retry_escalate_after", 0)),
	}
	if args := req.GetArguments(); args != nil {
		if _, ok := args["content_auto_fix_enabled"]; ok {
			body["content_auto_fix_enabled"] = req.GetBool("content_auto_fix_enabled", true)
		}
		if _, ok := args["auto_fix_skip_approval"]; ok {
			body["auto_fix_skip_approval"] = req.GetBool("auto_fix_skip_approval", true)
		}
	}
	data, err := s.proxyJSON(http.MethodPost, "/api/autonomous/retry_profiles", body)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

// ── retry_profile_get ───────────────────────────────────────────────────

func (s *Server) toolRetryProfileGet() mcpsdk.Tool {
	return mcpsdk.NewTool("retry_profile_get",
		mcpsdk.WithDescription("Get a retry profile by ID (BL410)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Profile ID.")),
	)
}

func (s *Server) handleRetryProfileGet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	data, err := s.proxyJSON(http.MethodGet, "/api/autonomous/retry_profiles/"+id, nil)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

// ── retry_profile_update ────────────────────────────────────────────────

func (s *Server) toolRetryProfileUpdate() mcpsdk.Tool {
	return mcpsdk.NewTool("retry_profile_update",
		mcpsdk.WithDescription("Update a retry profile (BL410)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Profile ID.")),
		mcpsdk.WithString("name", mcpsdk.Description("New name (omit to keep existing).")),
		mcpsdk.WithString("description", mcpsdk.Description("New description.")),
		mcpsdk.WithString("budgets_json", mcpsdk.Description(`New budgets as a JSON object, e.g. {"infra":10}`)),
		mcpsdk.WithNumber("ceiling_total", mcpsdk.Description("New ceiling total.")),
		mcpsdk.WithNumber("infra_retry_escalate_after", mcpsdk.Description("New infra escalation threshold.")),
		mcpsdk.WithBoolean("content_auto_fix_enabled", mcpsdk.Description("New content-auto-fix-enabled value.")),
		mcpsdk.WithBoolean("auto_fix_skip_approval", mcpsdk.Description("New auto-fix-skip-approval value.")),
	)
}

func (s *Server) handleRetryProfileUpdate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	name := req.GetString("name", "")
	desc := req.GetString("description", "")
	var budgets map[string]int
	_ = json.Unmarshal([]byte(req.GetString("budgets_json", "{}")), &budgets)
	body := map[string]any{
		"name": name, "description": desc, "budgets": budgets,
		"ceiling_total":              int(req.GetFloat("ceiling_total", 0)),
		"infra_retry_escalate_after": int(req.GetFloat("infra_retry_escalate_after", 0)),
	}
	if args := req.GetArguments(); args != nil {
		if _, ok := args["content_auto_fix_enabled"]; ok {
			body["content_auto_fix_enabled"] = req.GetBool("content_auto_fix_enabled", true)
		}
		if _, ok := args["auto_fix_skip_approval"]; ok {
			body["auto_fix_skip_approval"] = req.GetBool("auto_fix_skip_approval", true)
		}
	}
	data, err := s.proxyJSON(http.MethodPut, "/api/autonomous/retry_profiles/"+id, body)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

// ── retry_profile_delete ────────────────────────────────────────────────

func (s *Server) toolRetryProfileDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("retry_profile_delete",
		mcpsdk.WithDescription("Delete a retry profile by ID (BL410)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Profile ID.")),
	)
}

func (s *Server) handleRetryProfileDelete(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	_, err := s.proxyJSON(http.MethodDelete, "/api/autonomous/retry_profiles/"+id, nil)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(`{"status":"deleted"}`), nil
}

// ── per_automaton_retry_policy_set ──────────────────────────────────────

func (s *Server) toolPerAutomatonRetryPolicySet() mcpsdk.Tool {
	return mcpsdk.NewTool("per_automaton_retry_policy_set",
		mcpsdk.WithDescription("Set per-Automaton retry-policy overrides (BL410). Priority: explicit > named profile > global config."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton (PRD) ID.")),
		mcpsdk.WithString("retry_profile", mcpsdk.Description("Named retry profile to apply (optional).")),
		mcpsdk.WithString("retry_policy_json", mcpsdk.Description(`Explicit per-category retry budgets as a JSON object (beats profile), e.g. {"infra":10}`)),
		mcpsdk.WithNumber("retry_ceiling_total", mcpsdk.Description("Explicit ceiling for this PRD (beats profile/global; 0 = inherit).")),
		mcpsdk.WithBoolean("content_auto_fix_enabled", mcpsdk.Description("Explicit content-auto-fix-enabled override for this PRD.")),
		mcpsdk.WithBoolean("auto_fix_skip_approval", mcpsdk.Description("Explicit auto-fix-skip-approval override for this PRD.")),
	)
}

func (s *Server) handlePerAutomatonRetryPolicySet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	profile := req.GetString("retry_profile", "")
	var policy map[string]int
	_ = json.Unmarshal([]byte(req.GetString("retry_policy_json", "{}")), &policy)
	body := map[string]any{
		"retry_profile":       profile,
		"retry_policy":        policy,
		"retry_ceiling_total": int(req.GetFloat("retry_ceiling_total", 0)),
	}
	if args := req.GetArguments(); args != nil {
		if _, ok := args["content_auto_fix_enabled"]; ok {
			body["content_auto_fix_enabled"] = req.GetBool("content_auto_fix_enabled", true)
		}
		if _, ok := args["auto_fix_skip_approval"]; ok {
			body["auto_fix_skip_approval"] = req.GetBool("auto_fix_skip_approval", true)
		}
	}
	data, err := s.proxyJSON(http.MethodPut, "/api/autonomous/prds/"+id+"/set_retry_policy", body)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}
