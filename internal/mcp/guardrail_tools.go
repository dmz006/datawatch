// BL303 S2/S3 — MCP tools for the guardrail library + profiles + session guardrail.
//
// Tools:
//   guardrail_library_list       — list all registered guardrails
//   guardrail_profile_list       — list guardrail profiles
//   guardrail_profile_create     — create a profile
//   guardrail_profile_get        — get one profile by ID
//   guardrail_profile_update     — update a profile
//   guardrail_profile_delete     — delete a profile
//   per_automaton_guardrails_set — set per-Automaton guardrail overrides
//   session_guardrail_run        — run a named guardrail on a session (S3 T15)

package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ── guardrail_library_list ────────────────────────────────────────────────

func (s *Server) toolGuardrailLibraryList() mcpsdk.Tool {
	return mcpsdk.NewTool("guardrail_library_list",
		mcpsdk.WithDescription("List all registered guardrails in the guardrail library (built-in scan guardrails + skill-contributed)."),
	)
}

func (s *Server) handleGuardrailLibraryList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	data, err := s.proxyJSON(http.MethodGet, "/api/autonomous/guardrails", nil)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

// ── guardrail_profile_list ────────────────────────────────────────────────

func (s *Server) toolGuardrailProfileList() mcpsdk.Tool {
	return mcpsdk.NewTool("guardrail_profile_list",
		mcpsdk.WithDescription("List all guardrail profiles."),
	)
}

func (s *Server) handleGuardrailProfileList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	data, err := s.proxyJSON(http.MethodGet, "/api/autonomous/guardrail_profiles", nil)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

// ── guardrail_profile_create ──────────────────────────────────────────────

func (s *Server) toolGuardrailProfileCreate() mcpsdk.Tool {
	return mcpsdk.NewTool("guardrail_profile_create",
		mcpsdk.WithDescription("Create a new guardrail profile."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Profile name.")),
		mcpsdk.WithString("description", mcpsdk.Description("Optional description.")),
		mcpsdk.WithString("guardrails_json", mcpsdk.Description(`JSON array of guardrail names, e.g. ["sast-scan","secrets-scan"]`)),
	)
}

func (s *Server) handleGuardrailProfileCreate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	desc := req.GetString("description", "")
	var guardrails []string
	_ = json.Unmarshal([]byte(req.GetString("guardrails_json", "[]")), &guardrails)
	body := map[string]any{"name": name, "description": desc, "guardrails": guardrails}
	data, err := s.proxyJSON(http.MethodPost, "/api/autonomous/guardrail_profiles", body)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

// ── guardrail_profile_get ─────────────────────────────────────────────────

func (s *Server) toolGuardrailProfileGet() mcpsdk.Tool {
	return mcpsdk.NewTool("guardrail_profile_get",
		mcpsdk.WithDescription("Get a guardrail profile by ID."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Profile ID.")),
	)
}

func (s *Server) handleGuardrailProfileGet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	data, err := s.proxyJSON(http.MethodGet, "/api/autonomous/guardrail_profiles/"+id, nil)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

// ── guardrail_profile_update ──────────────────────────────────────────────

func (s *Server) toolGuardrailProfileUpdate() mcpsdk.Tool {
	return mcpsdk.NewTool("guardrail_profile_update",
		mcpsdk.WithDescription("Update a guardrail profile."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Profile ID.")),
		mcpsdk.WithString("name", mcpsdk.Description("New name (omit to keep existing).")),
		mcpsdk.WithString("description", mcpsdk.Description("New description.")),
		mcpsdk.WithString("guardrails_json", mcpsdk.Description(`New guardrail list as JSON array, e.g. ["sast-scan","deps-scan"]`)),
	)
}

func (s *Server) handleGuardrailProfileUpdate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	name := req.GetString("name", "")
	desc := req.GetString("description", "")
	var guardrails []string
	_ = json.Unmarshal([]byte(req.GetString("guardrails_json", "[]")), &guardrails)
	body := map[string]any{"name": name, "description": desc, "guardrails": guardrails}
	data, err := s.proxyJSON(http.MethodPut, "/api/autonomous/guardrail_profiles/"+id, body)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

// ── guardrail_profile_delete ──────────────────────────────────────────────

func (s *Server) toolGuardrailProfileDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("guardrail_profile_delete",
		mcpsdk.WithDescription("Delete a guardrail profile by ID."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Profile ID.")),
	)
}

func (s *Server) handleGuardrailProfileDelete(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	_, err := s.proxyJSON(http.MethodDelete, "/api/autonomous/guardrail_profiles/"+id, nil)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(`{"status":"deleted"}`), nil
}

// ── per_automaton_guardrails_set ──────────────────────────────────────────

func (s *Server) toolPerAutomatonGuardrailsSet() mcpsdk.Tool {
	return mcpsdk.NewTool("per_automaton_guardrails_set",
		mcpsdk.WithDescription("Set per-Automaton guardrail overrides. Priority: explicit > named profile > global config."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton (PRD) ID.")),
		mcpsdk.WithString("guardrail_profile", mcpsdk.Description("Named profile to apply (optional).")),
		mcpsdk.WithString("per_task_guardrails_json", mcpsdk.Description(`Explicit per-task guardrail list as JSON array (beats profile), e.g. ["sast-scan"]`)),
		mcpsdk.WithString("per_story_guardrails_json", mcpsdk.Description(`Explicit per-story guardrail list as JSON array (beats profile), e.g. ["deps-scan"]`)),
	)
}

func (s *Server) handlePerAutomatonGuardrailsSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	profile := req.GetString("guardrail_profile", "")
	var perTask, perStory []string
	_ = json.Unmarshal([]byte(req.GetString("per_task_guardrails_json", "[]")), &perTask)
	_ = json.Unmarshal([]byte(req.GetString("per_story_guardrails_json", "[]")), &perStory)
	body := map[string]any{
		"guardrail_profile":    profile,
		"per_task_guardrails":  perTask,
		"per_story_guardrails": perStory,
	}
	data, err := s.proxyJSON(http.MethodPut, "/api/autonomous/prds/"+id+"/guardrails", body)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}

// ── session_guardrail_run (BL303 S3 T15) ─────────────────────────────────

func (s *Server) toolSessionGuardrailRun() mcpsdk.Tool {
	return mcpsdk.NewTool("session_guardrail_run",
		mcpsdk.WithDescription("Run a named guardrail on a session's project directory. Appends the verdict to the session's telemetry."),
		mcpsdk.WithString("session_id", mcpsdk.Required(), mcpsdk.Description("Session ID.")),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Guardrail name from the library (e.g. sast-scan, secrets-scan, deps-scan).")),
	)
}

func (s *Server) handleSessionGuardrailRun(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	sid := req.GetString("session_id", "")
	name := req.GetString("name", "")
	body := map[string]any{"name": name}
	data, err := s.proxyJSON(http.MethodPost, "/api/sessions/"+sid+"/guardrail", body)
	if err != nil {
		return mcpsdk.NewToolResultText("error: " + err.Error()), nil
	}
	return mcpsdk.NewToolResultText(string(data)), nil
}
