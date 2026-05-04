// BL243 Phase 1 — MCP tools for the Tailscale k8s sidecar feature.
//
//   tailscale_status   — aggregated status + node list
//   tailscale_nodes    — raw node/device list
//   tailscale_acl_push — push an ACL policy string to headscale

package mcp

import (
	"context"
	"fmt"
	"net/http"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolTailscaleStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("tailscale_status",
		mcpsdk.WithDescription("BL243 — get Tailscale k8s sidecar status: enabled state, backend (headscale/tailscale), coordinator URL, and connected node list."),
	)
}
func (s *Server) handleTailscaleStatus(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/tailscale/status", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolTailscaleNodes() mcpsdk.Tool {
	return mcpsdk.NewTool("tailscale_nodes",
		mcpsdk.WithDescription("BL243 — list all Tailscale nodes/devices visible to the admin API. Returns id, name, IP, online status, tags, and OS."),
	)
}
func (s *Server) handleTailscaleNodes(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/tailscale/nodes", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolTailscaleACLPush() mcpsdk.Tool {
	return mcpsdk.NewTool("tailscale_acl_push",
		mcpsdk.WithDescription("BL243 — push an ACL policy (HCL or JSON string) to the headscale coordinator. Only supported when coordinator_url is set."),
		mcpsdk.WithString("policy", mcpsdk.Required(), mcpsdk.Description("HCL or JSON ACL policy string to push to headscale")),
	)
}
func (s *Server) handleTailscaleACLPush(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	policy := req.GetString("policy", "")
	if policy == "" {
		return nil, fmt.Errorf("policy required")
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/tailscale/acl/push", policy)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
