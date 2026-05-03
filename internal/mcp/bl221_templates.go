// BL221 (v6.2.0) Phase 5 — MCP tools for autonomous template store CRUD.

package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ----- autonomous_template_list / create / get / update / delete -----------

func (s *Server) toolAutonomousTemplateList() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_template_list",
		mcpsdk.WithDescription("BL221 Phase 5 — list all automaton templates in the template store."),
	)
}
func (s *Server) handleAutonomousTemplateList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/autonomous/templates", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousTemplateCreate() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_template_create",
		mcpsdk.WithDescription("BL221 Phase 5 — create a new automaton template."),
		mcpsdk.WithString("title", mcpsdk.Required(), mcpsdk.Description("Template title")),
		mcpsdk.WithString("spec", mcpsdk.Required(), mcpsdk.Description("Template spec / intent text (may include {{variable}} placeholders)")),
		mcpsdk.WithString("description", mcpsdk.Description("Optional human-readable description")),
		mcpsdk.WithString("type", mcpsdk.Description("Automaton type (software|research|operational|personal or custom)")),
		mcpsdk.WithString("tags_json", mcpsdk.Description(`JSON array of tag strings, e.g. ["security","python"]`)),
	)
}
func (s *Server) handleAutonomousTemplateCreate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	var tags []string
	_ = json.Unmarshal([]byte(req.GetString("tags_json", "[]")), &tags)
	body, _ := json.Marshal(map[string]any{
		"title":       req.GetString("title", ""),
		"spec":        req.GetString("spec", ""),
		"description": req.GetString("description", ""),
		"type":        req.GetString("type", ""),
		"tags":        tags,
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/templates", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousTemplateGet() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_template_get",
		mcpsdk.WithDescription("BL221 Phase 5 — fetch one automaton template by ID."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Template ID")),
	)
}
func (s *Server) handleAutonomousTemplateGet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/autonomous/templates/"+req.GetString("id", ""), nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousTemplateUpdate() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_template_update",
		mcpsdk.WithDescription("BL221 Phase 5 — update an existing automaton template."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Template ID")),
		mcpsdk.WithString("title", mcpsdk.Description("New title (empty = keep existing)")),
		mcpsdk.WithString("spec", mcpsdk.Description("New spec (empty = keep existing)")),
		mcpsdk.WithString("description", mcpsdk.Description("New description")),
		mcpsdk.WithString("type", mcpsdk.Description("New automaton type")),
		mcpsdk.WithString("tags_json", mcpsdk.Description(`JSON array of tags, e.g. ["security"]`)),
	)
}
func (s *Server) handleAutonomousTemplateUpdate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	var tags []string
	_ = json.Unmarshal([]byte(req.GetString("tags_json", "[]")), &tags)
	body, _ := json.Marshal(map[string]any{
		"title":       req.GetString("title", ""),
		"spec":        req.GetString("spec", ""),
		"description": req.GetString("description", ""),
		"type":        req.GetString("type", ""),
		"tags":        tags,
	})
	out, err := s.proxyJSON(http.MethodPut, "/api/autonomous/templates/"+id, body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousTemplateDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_template_delete",
		mcpsdk.WithDescription("BL221 Phase 5 — permanently delete an automaton template."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Template ID")),
	)
}
func (s *Server) handleAutonomousTemplateDelete(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodDelete, "/api/autonomous/templates/"+req.GetString("id", ""), nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// ----- autonomous_template_instantiate / autonomous_prd_clone_to_template --

func (s *Server) toolAutonomousTemplateInstantiate() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_template_instantiate",
		mcpsdk.WithDescription("BL221 Phase 5 — create a new PRD from a template, filling in variables."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Template ID to instantiate")),
		mcpsdk.WithString("project_dir", mcpsdk.Required(), mcpsdk.Description("Working directory for the new PRD")),
		mcpsdk.WithString("vars_json", mcpsdk.Description(`JSON object of variable substitutions, e.g. {"service":"payments"}`)),
		mcpsdk.WithString("backend", mcpsdk.Description("LLM backend override")),
		mcpsdk.WithString("effort", mcpsdk.Description("Effort hint (low/medium/high/max)")),
	)
}
func (s *Server) handleAutonomousTemplateInstantiate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	var vars map[string]string
	_ = json.Unmarshal([]byte(req.GetString("vars_json", "{}")), &vars)
	body, _ := json.Marshal(map[string]any{
		"project_dir": req.GetString("project_dir", ""),
		"vars":        vars,
		"backend":     req.GetString("backend", ""),
		"effort":      req.GetString("effort", ""),
		"actor":       "mcp",
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/templates/"+id+"/instantiate", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDCloneToTemplate() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_clone_to_template",
		mcpsdk.WithDescription("BL221 Phase 5 — save a completed PRD as a reusable template in the template store."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("PRD ID to clone")),
		mcpsdk.WithString("description", mcpsdk.Description("Optional description for the new template")),
	)
}
func (s *Server) handleAutonomousPRDCloneToTemplate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{
		"description": req.GetString("description", ""),
		"actor":       "mcp",
	})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+id+"/clone_to_template", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
