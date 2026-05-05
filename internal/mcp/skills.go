// BL255 v6.7.0 — MCP tools for the skills subsystem.
//
//   skills_registry_list           — list configured skill registries
//   skills_registry_get            — fetch one registry by name
//   skills_registry_create         — add a new git-backed registry
//   skills_registry_update         — modify an existing registry
//   skills_registry_delete         — remove a registry (cascades synced)
//   skills_registry_add_default    — idempotently add the PAI default
//   skills_registry_connect        — shallow clone + populate available
//   skills_registry_available      — list cached available skills
//   skills_registry_sync           — sync selected/all skills from a registry
//   skills_registry_unsync         — unsync selected/all skills
//   skills_list                    — list synced skills (across registries)
//   skills_get                     — get one synced skill (manifest + path)
//   skill_load                     — read a skill's markdown content (option D)

package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ── Tool descriptors ────────────────────────────────────────────────────

func (s *Server) toolSkillsRegistryList() mcpsdk.Tool {
	return mcpsdk.NewTool("skills_registry_list",
		mcpsdk.WithDescription("BL255 — list configured skill registries (PAI default + operator-added)."),
	)
}
func (s *Server) toolSkillsRegistryGet() mcpsdk.Tool {
	return mcpsdk.NewTool("skills_registry_get",
		mcpsdk.WithDescription("BL255 — get one skill registry by name."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("registry name")),
	)
}
func (s *Server) toolSkillsRegistryCreate() mcpsdk.Tool {
	return mcpsdk.NewTool("skills_registry_create",
		mcpsdk.WithDescription("BL255 — add a new git-backed skill registry."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("unique registry name")),
		mcpsdk.WithString("url", mcpsdk.Required(), mcpsdk.Description("git URL")),
		mcpsdk.WithString("branch", mcpsdk.Description("branch (default: main)")),
		mcpsdk.WithString("auth_secret_ref", mcpsdk.Description("${secret:...} ref for private repos (per Secrets-Store Rule)")),
		mcpsdk.WithString("description", mcpsdk.Description("free-form description")),
	)
}
func (s *Server) toolSkillsRegistryUpdate() mcpsdk.Tool {
	return mcpsdk.NewTool("skills_registry_update",
		mcpsdk.WithDescription("BL255 — update fields on an existing skill registry."),
		mcpsdk.WithString("name", mcpsdk.Required()),
		mcpsdk.WithString("url", mcpsdk.Description("git URL (preserved if empty)")),
		mcpsdk.WithString("branch", mcpsdk.Description("branch")),
		mcpsdk.WithString("auth_secret_ref", mcpsdk.Description("${secret:...}")),
		mcpsdk.WithString("description", mcpsdk.Description("description")),
	)
}
func (s *Server) toolSkillsRegistryDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("skills_registry_delete",
		mcpsdk.WithDescription("BL255 — delete a skill registry; synced skills from this registry are removed from the index."),
		mcpsdk.WithString("name", mcpsdk.Required()),
	)
}
func (s *Server) toolSkillsRegistryAddDefault() mcpsdk.Tool {
	return mcpsdk.NewTool("skills_registry_add_default",
		mcpsdk.WithDescription("BL255 — idempotently add the built-in PAI (danielmiessler/Personal_AI_Infrastructure) default registry. No-op if 'pai' already present."),
	)
}
func (s *Server) toolSkillsRegistryConnect() mcpsdk.Tool {
	return mcpsdk.NewTool("skills_registry_connect",
		mcpsdk.WithDescription("BL255 — shallow-clone (or fetch) the registry repo and populate the available-skills cache."),
		mcpsdk.WithString("name", mcpsdk.Required()),
	)
}
func (s *Server) toolSkillsRegistryAvailable() mcpsdk.Tool {
	return mcpsdk.NewTool("skills_registry_available",
		mcpsdk.WithDescription("BL255 — list available skills in a registry (from cache; auto-connects if empty)."),
		mcpsdk.WithString("name", mcpsdk.Required()),
	)
}
func (s *Server) toolSkillsRegistrySync() mcpsdk.Tool {
	return mcpsdk.NewTool("skills_registry_sync",
		mcpsdk.WithDescription("BL255 — sync selected (or all) available skills from a registry into the synced area."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("registry name")),
		mcpsdk.WithString("skills", mcpsdk.Description("comma-separated skill names; pass '*' or omit and set all=true to sync all available")),
		mcpsdk.WithBoolean("all", mcpsdk.Description("sync every available skill in the registry")),
	)
}
func (s *Server) toolSkillsRegistryUnsync() mcpsdk.Tool {
	return mcpsdk.NewTool("skills_registry_unsync",
		mcpsdk.WithDescription("BL255 — remove selected (or all) synced skills from a registry."),
		mcpsdk.WithString("name", mcpsdk.Required()),
		mcpsdk.WithString("skills", mcpsdk.Description("comma-separated skill names")),
		mcpsdk.WithBoolean("all", mcpsdk.Description("unsync everything from this registry")),
	)
}
func (s *Server) toolSkillsList() mcpsdk.Tool {
	return mcpsdk.NewTool("skills_list",
		mcpsdk.WithDescription("BL255 — list every synced skill across all registries."),
	)
}
func (s *Server) toolSkillsGet() mcpsdk.Tool {
	return mcpsdk.NewTool("skills_get",
		mcpsdk.WithDescription("BL255 — get a synced skill's manifest + on-disk path."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("skill name")),
	)
}
func (s *Server) toolSkillLoad() mcpsdk.Tool {
	return mcpsdk.NewTool("skill_load",
		mcpsdk.WithDescription("BL255 option D — read a synced skill's markdown into context. Agents call this on-demand instead of having every skill injected into the prompt."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("skill name (must be synced)")),
	)
}

// ── Handlers ────────────────────────────────────────────────────────────

func (s *Server) handleSkillsRegistryList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/skills/registries", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleSkillsRegistryGet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return nil, err
	}
	out, err := s.proxyGet("/api/skills/registries/"+name, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleSkillsRegistryCreate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body := map[string]any{
		"name":            mustString(req, "name"),
		"url":             mustString(req, "url"),
		"branch":          optString(req, "branch"),
		"auth_secret_ref": optString(req, "auth_secret_ref"),
		"description":     optString(req, "description"),
		"enabled":         true,
		"kind":            "git",
	}
	out, err := s.proxyJSON("POST", "/api/skills/registries", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleSkillsRegistryUpdate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := mustString(req, "name")
	body := map[string]any{
		"url":             optString(req, "url"),
		"branch":          optString(req, "branch"),
		"auth_secret_ref": optString(req, "auth_secret_ref"),
		"description":     optString(req, "description"),
		"enabled":         true,
		"kind":            "git",
	}
	out, err := s.proxyJSON("PUT", "/api/skills/registries/"+name, body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleSkillsRegistryDelete(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := mustString(req, "name")
	out, err := s.proxyJSON("DELETE", "/api/skills/registries/"+name, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleSkillsRegistryAddDefault(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON("POST", "/api/skills/registries/add-default", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleSkillsRegistryConnect(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := mustString(req, "name")
	out, err := s.proxyJSON("POST", "/api/skills/registries/"+name+"/connect", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleSkillsRegistryAvailable(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := mustString(req, "name")
	out, err := s.proxyGet("/api/skills/registries/"+name+"/available", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleSkillsRegistrySync(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := mustString(req, "name")
	all, _ := req.RequireBool("all")
	skillsCSV := optString(req, "skills")
	body := map[string]any{"all": all}
	if skillsCSV != "" {
		body["skills"] = splitCSV(skillsCSV)
	}
	out, err := s.proxyJSON("POST", "/api/skills/registries/"+name+"/sync", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleSkillsRegistryUnsync(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := mustString(req, "name")
	all, _ := req.RequireBool("all")
	skillsCSV := optString(req, "skills")
	body := map[string]any{"all": all}
	if skillsCSV != "" {
		body["skills"] = splitCSV(skillsCSV)
	}
	out, err := s.proxyJSON("POST", "/api/skills/registries/"+name+"/unsync", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleSkillsList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/skills", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleSkillsGet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := mustString(req, "name")
	out, err := s.proxyGet("/api/skills/"+name, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
func (s *Server) handleSkillLoad(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := mustString(req, "name")
	body, err := s.proxyGet("/api/skills/"+name+"/content", nil)
	if err != nil {
		return nil, fmt.Errorf("load skill %s: %w", name, err)
	}
	return textOK(string(body)), nil
}

// ── helpers ─────────────────────────────────────────────────────────────

func mustString(req mcpsdk.CallToolRequest, key string) string {
	v, _ := req.RequireString(key)
	return v
}
func optString(req mcpsdk.CallToolRequest, key string) string {
	v, _ := req.RequireString(key)
	return v
}
func splitCSV(s string) []string {
	var out []string
	for _, p := range jsonSplitCSV(s) {
		out = append(out, p)
	}
	return out
}
func jsonSplitCSV(s string) []string {
	if s == "" {
		return nil
	}
	// We tolerate either a JSON array or a CSV string here so ad-hoc
	// MCP callers can pass the more convenient form.
	var arr []string
	if err := json.Unmarshal([]byte(s), &arr); err == nil {
		return arr
	}
	var out []string
	for _, p := range splitOn(s, ',') {
		if p = trimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
func splitOn(s string, sep rune) []string {
	var out []string
	start := 0
	for i, r := range s {
		if r == sep {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n') {
		s = s[:len(s)-1]
	}
	return s
}
