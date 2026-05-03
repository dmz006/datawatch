// BL221 (v6.2.0) Phase 4 — MCP tools for type registry, Guided Mode, skills.

package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ----- autonomous_type_list / register --------------------------------------

func (s *Server) toolAutonomousTypeList() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_type_list",
		mcpsdk.WithDescription("BL221 Phase 4 — list the automaton type registry (built-in + operator-registered types)."),
	)
}
func (s *Server) handleAutonomousTypeList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/autonomous/types", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousTypeRegister() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_type_register",
		mcpsdk.WithDescription("BL221 Phase 4 — register or update an automaton type in the registry."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Type identifier (e.g., 'software', 'research', 'custom_type')")),
		mcpsdk.WithString("label", mcpsdk.Required(), mcpsdk.Description("Human-readable label")),
		mcpsdk.WithString("description", mcpsdk.Description("Optional description")),
		mcpsdk.WithString("color", mcpsdk.Description("Optional CSS color for badges (e.g., '#6366f1')")),
	)
}
func (s *Server) handleAutonomousTypeRegister(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body, _ := json.Marshal(map[string]string{
		"id":          req.GetString("id", ""),
		"label":       req.GetString("label", ""),
		"description": req.GetString("description", ""),
		"color":       req.GetString("color", ""),
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/types", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ----- autonomous_prd_set_type / guided_mode / skills -----------------------

func (s *Server) toolAutonomousPRDSetType() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_type",
		mcpsdk.WithDescription("BL221 Phase 4 — set the automaton type on a PRD (software|research|operational|personal or custom)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("type", mcpsdk.Required(), mcpsdk.Description("Type identifier from the type registry")),
	)
}
func (s *Server) handleAutonomousPRDSetType(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{"type": req.GetString("type", "")})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_type", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDSetGuidedMode() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_guided_mode",
		mcpsdk.WithDescription("BL221 Phase 4 — enable or disable Guided Mode on a PRD (step-by-step operator checkpoints)."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithBoolean("guided_mode", mcpsdk.Required(), mcpsdk.Description("true to enable, false to disable")),
	)
}
func (s *Server) handleAutonomousPRDSetGuidedMode(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]bool{"guided_mode": req.GetBool("guided_mode", false)})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_guided_mode", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDSetSkills() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_skills",
		mcpsdk.WithDescription("BL221 Phase 4 — assign skills to a PRD. Skills context is passed to spawned task sessions."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID")),
		mcpsdk.WithString("skills_json", mcpsdk.Required(), mcpsdk.Description(`JSON array of skill IDs, e.g. ["git","docker","pytest"]`)),
	)
}
func (s *Server) handleAutonomousPRDSetSkills(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	var skills []string
	_ = json.Unmarshal([]byte(req.GetString("skills_json", "[]")), &skills)
	body, _ := json.Marshal(map[string]any{"skills": skills})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/set_skills", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
