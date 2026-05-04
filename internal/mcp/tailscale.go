// BL243 Phase 1+2+3 — MCP tools for the Tailscale k8s sidecar feature.
//
//   tailscale_status       — aggregated status + node list
//   tailscale_nodes        — raw node/device list
//   tailscale_acl_push     — push an ACL policy string to headscale
//   tailscale_acl_generate — generate ACL policy from config (no push) (Phase 3)
//   tailscale_auth_key     — generate a headscale pre-auth key (Phase 2)

package mcp

import (
	"context"
	"fmt"
	"net/http"
	"strings"

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

func (s *Server) toolTailscaleACLGenerate() mcpsdk.Tool {
	return mcpsdk.NewTool("tailscale_acl_generate",
		mcpsdk.WithDescription("BL243 Phase 3 — generate a headscale ACL policy from the current daemon config and live node list. Returns the JSON policy string without pushing it. Use tailscale_acl_push with no policy to auto-generate and push in one step."),
	)
}
func (s *Server) handleTailscaleACLGenerate(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodPost, "/api/tailscale/acl/generate", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolTailscaleAuthKey() mcpsdk.Tool {
	return mcpsdk.NewTool("tailscale_auth_key",
		mcpsdk.WithDescription("BL243 Phase 2 — generate a new headscale pre-auth key. Returns the key string and metadata. Only supported with headscale (coordinator_url set)."),
		mcpsdk.WithBoolean("reusable", mcpsdk.Description("Allow multiple nodes to use this key (default: false = single-use)")),
		mcpsdk.WithBoolean("ephemeral", mcpsdk.Description("Mark nodes using this key as ephemeral — removed when they go offline (default: false)")),
		mcpsdk.WithString("tags", mcpsdk.Description("Comma-separated ACL tags for nodes joining with this key, e.g. 'tag:dw-agent,tag:dw-research'. Defaults to the configured tags.")),
		mcpsdk.WithNumber("expiry_hours", mcpsdk.Description("Hours until the key expires (default: 24)")),
	)
}
func (s *Server) handleTailscaleAuthKey(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	payload := map[string]interface{}{
		"reusable":     req.GetBool("reusable", false),
		"ephemeral":    req.GetBool("ephemeral", false),
		"expiry_hours": req.GetInt("expiry_hours", 24),
	}
	if tags := req.GetString("tags", ""); tags != "" {
		payload["tags"] = splitTrim(tags, ",")
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/tailscale/auth/key", payload)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// splitTrim splits s by sep and trims spaces from each element.
func splitTrim(s, sep string) []string {
	parts := make([]string, 0)
	for _, p := range strings.Split(s, sep) {
		if t := strings.TrimSpace(p); t != "" {
			parts = append(parts, t)
		}
	}
	return parts
}
