// BL242 Phase 1 — MCP tools for the centralized secrets manager.
//
//   secret_list    — list all secrets (no values)
//   secret_get     — get a secret including its value (audited)
//   secret_set     — create or update a secret
//   secret_delete  — delete a secret
//   secret_exists  — check if a secret exists

package mcp

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolSecretList() mcpsdk.Tool {
	return mcpsdk.NewTool("secret_list",
		mcpsdk.WithDescription("BL242 — list all secrets in the centralized store (names, tags, description — no values)."),
	)
}
func (s *Server) handleSecretList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/secrets", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolSecretGet() mcpsdk.Tool {
	return mcpsdk.NewTool("secret_get",
		mcpsdk.WithDescription("BL242 — get a secret by name, including its value. Access is audit-logged."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Secret name")),
	)
}
func (s *Server) handleSecretGet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	if name == "" {
		return nil, fmt.Errorf("name required")
	}
	out, err := s.proxyGet("/api/secrets/"+name, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolSecretSet() mcpsdk.Tool {
	return mcpsdk.NewTool("secret_set",
		mcpsdk.WithDescription("BL242 — create or update a secret. Tags are comma-separated."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Secret name")),
		mcpsdk.WithString("value", mcpsdk.Required(), mcpsdk.Description("Secret value")),
		mcpsdk.WithString("tags", mcpsdk.Description("Comma-separated tags (e.g. git,cloud)")),
		mcpsdk.WithString("description", mcpsdk.Description("Human-readable description")),
	)
}
func (s *Server) handleSecretSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	value := req.GetString("value", "")
	tagsRaw := req.GetString("tags", "")
	description := req.GetString("description", "")
	if name == "" {
		return nil, fmt.Errorf("name required")
	}
	var tags []string
	for _, t := range strings.Split(tagsRaw, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	body := map[string]any{
		"name":        name,
		"value":       value,
		"tags":        tags,
		"description": description,
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/secrets", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolSecretDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("secret_delete",
		mcpsdk.WithDescription("BL242 — delete a secret by name."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Secret name")),
	)
}
func (s *Server) handleSecretDelete(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	if name == "" {
		return nil, fmt.Errorf("name required")
	}
	out, err := s.proxyJSON(http.MethodDelete, "/api/secrets/"+name, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolSecretExists() mcpsdk.Tool {
	return mcpsdk.NewTool("secret_exists",
		mcpsdk.WithDescription("BL242 — check if a secret exists without revealing its value."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Secret name")),
	)
}
func (s *Server) handleSecretExists(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	if name == "" {
		return nil, fmt.Errorf("name required")
	}
	out, err := s.proxyGet("/api/secrets/"+name+"/exists", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
