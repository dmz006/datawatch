// internal/mcp/smoke_forward.go — #54 MCP tools for smoke-run cross-instance forwarding.
//
//   smoke_forward_config_get   — get current forward URL + token-set flag
//   smoke_forward_config_set   — update forward URL (and optionally token)

package mcp

import (
	"context"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolSmokeForwardConfigGet() mcpsdk.Tool {
	return mcpsdk.NewTool("smoke_forward_config_get",
		mcpsdk.WithDescription("#54 — get the smoke-run cross-instance forward URL and whether a token is configured."),
	)
}

func (s *Server) toolSmokeForwardConfigSet() mcpsdk.Tool {
	return mcpsdk.NewTool("smoke_forward_config_set",
		mcpsdk.WithDescription("#54 — set the smoke-run forward URL so this daemon also POSTs every progress write to a remote dashboard. Clear with empty string."),
		mcpsdk.WithString("forward_url", mcpsdk.Required(), mcpsdk.Description("production daemon base URL, e.g. https://prod.example.com:8443 (empty to disable)")),
		mcpsdk.WithString("forward_token", mcpsdk.Description("bearer token for the remote daemon (optional)")),
	)
}

func (s *Server) handleSmokeForwardConfigGetMCP(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body, err := s.proxyGet("/api/smoke/forward-url", nil)
	if err != nil {
		return nil, err
	}
	return mcpsdk.NewToolResultText(string(body)), nil
}

func (s *Server) handleSmokeForwardConfigSetMCP(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	payload := map[string]any{
		"forward_url": req.GetString("forward_url", ""),
	}
	if tok := req.GetString("forward_token", ""); tok != "" {
		payload["forward_token"] = tok
	}
	body, err := s.proxyJSON("PUT", "/api/smoke/forward-url", payload)
	if err != nil {
		return nil, err
	}
	return mcpsdk.NewToolResultText(string(body)), nil
}
