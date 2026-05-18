// BL316 S2 — MCP tools for federation peer and group management. All proxy to REST.

package mcp

import (
	"context"
	"fmt"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/federation"
)

func (s *Server) RegisterFederationTools() {
	tracked := func(fn func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error)) func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			result, err := fn(ctx, req)
			if s.chanStats != nil {
				reqSize := len(fmt.Sprintf("%v", req.Params.Arguments))
				respSize := 0
				if result != nil {
					for _, c := range result.Content {
						if tc, ok := c.(mcpsdk.TextContent); ok {
							respSize += len(tc.Text)
						}
					}
				}
				s.chanStats.RecordRecv(reqSize)
				s.chanStats.RecordSent(respSize)
				if err != nil {
					s.chanStats.RecordError()
				}
			}
			return result, err
		}
	}

	s.srv.AddTool(s.toolFederationPeerList(), tracked(s.handleFederationPeerListMCP))
	s.srv.AddTool(s.toolFederationPeerAdd(), tracked(s.handleFederationPeerAddMCP))
	s.srv.AddTool(s.toolFederationPeerGet(), tracked(s.handleFederationPeerGetMCP))
	s.srv.AddTool(s.toolFederationPeerUpdate(), tracked(s.handleFederationPeerUpdateMCP))
	s.srv.AddTool(s.toolFederationPeerDelete(), tracked(s.handleFederationPeerDeleteMCP))
	s.srv.AddTool(s.toolFederationPeerTest(), tracked(s.handleFederationPeerTestMCP))
	s.srv.AddTool(s.toolFederationGroupList(), tracked(s.handleFederationGroupListMCP))
	s.srv.AddTool(s.toolFederationGroupListBuiltins(), tracked(s.handleFederationGroupListBuiltinsMCP))
	s.srv.AddTool(s.toolFederationGroupAdd(), tracked(s.handleFederationGroupAddMCP))
	s.srv.AddTool(s.toolFederationGroupGet(), tracked(s.handleFederationGroupGetMCP))
	s.srv.AddTool(s.toolFederationGroupUpdate(), tracked(s.handleFederationGroupUpdateMCP))
	s.srv.AddTool(s.toolFederationGroupDelete(), tracked(s.handleFederationGroupDeleteMCP))
}

// ---------------------------------------------------------------------------
// Tool descriptors

func (s *Server) toolFederationPeerList() mcpsdk.Tool {
	return mcpsdk.NewTool("federation_peer_list",
		mcpsdk.WithDescription("BL316 S2 — list all registered federation peers."),
	)
}

func (s *Server) toolFederationPeerAdd() mcpsdk.Tool {
	return mcpsdk.NewTool("federation_peer_add",
		mcpsdk.WithDescription("BL316 S2 — register a new federation peer."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("unique peer name")),
		mcpsdk.WithString("url", mcpsdk.Required(), mcpsdk.Description("base URL of the peer instance")),
		mcpsdk.WithString("token", mcpsdk.Description("bearer token for the peer (optional)")),
		mcpsdk.WithString("capabilities", mcpsdk.Description("comma-separated capability strings (optional; defaults to federation-peer)")),
	)
}

func (s *Server) toolFederationPeerGet() mcpsdk.Tool {
	return mcpsdk.NewTool("federation_peer_get",
		mcpsdk.WithDescription("BL316 S2 — fetch one federation peer by name."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("peer name")),
	)
}

func (s *Server) toolFederationPeerUpdate() mcpsdk.Tool {
	return mcpsdk.NewTool("federation_peer_update",
		mcpsdk.WithDescription("BL316 S2 — update a federation peer entry."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("peer name")),
		mcpsdk.WithString("url", mcpsdk.Description("new base URL")),
		mcpsdk.WithString("token", mcpsdk.Description("new bearer token")),
		mcpsdk.WithString("capabilities", mcpsdk.Description("comma-separated capability strings")),
	)
}

func (s *Server) toolFederationPeerDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("federation_peer_delete",
		mcpsdk.WithDescription("BL316 S2 — remove a federation peer."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("peer name")),
	)
}

func (s *Server) toolFederationPeerTest() mcpsdk.Tool {
	return mcpsdk.NewTool("federation_peer_test",
		mcpsdk.WithDescription("BL316 S2 — ping a federation peer's health endpoint."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("peer name")),
	)
}

func (s *Server) toolFederationGroupList() mcpsdk.Tool {
	return mcpsdk.NewTool("federation_group_list",
		mcpsdk.WithDescription("BL316 S2 — list all federation capability groups."),
	)
}

func (s *Server) toolFederationGroupListBuiltins() mcpsdk.Tool {
	return mcpsdk.NewTool("federation_group_list_builtins",
		mcpsdk.WithDescription("BL316 S2 — list built-in federation capability groups."),
	)
}

func (s *Server) toolFederationGroupAdd() mcpsdk.Tool {
	return mcpsdk.NewTool("federation_group_add",
		mcpsdk.WithDescription("BL316 S2 — create a new federation capability group."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("group name")),
		mcpsdk.WithString("caps", mcpsdk.Required(), mcpsdk.Description("comma-separated capability strings")),
		mcpsdk.WithString("description", mcpsdk.Description("human-readable description (optional)")),
	)
}

func (s *Server) toolFederationGroupGet() mcpsdk.Tool {
	return mcpsdk.NewTool("federation_group_get",
		mcpsdk.WithDescription("BL316 S2 — fetch one federation capability group by name."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("group name")),
	)
}

func (s *Server) toolFederationGroupUpdate() mcpsdk.Tool {
	return mcpsdk.NewTool("federation_group_update",
		mcpsdk.WithDescription("BL316 S2 — update a federation capability group."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("group name")),
		mcpsdk.WithString("caps", mcpsdk.Required(), mcpsdk.Description("comma-separated capability strings")),
		mcpsdk.WithString("description", mcpsdk.Description("human-readable description (optional)")),
	)
}

func (s *Server) toolFederationGroupDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("federation_group_delete",
		mcpsdk.WithDescription("BL316 S2 — remove a federation capability group."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("group name")),
	)
}

// ---------------------------------------------------------------------------
// Handlers (proxy to REST)

func (s *Server) handleFederationPeerListMCP(ctx context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if deny := mcpFedCap(ctx, federation.CapFederationList); deny != nil {
		return deny, nil
	}
	out, err := s.proxyGet("/api/federation/peers", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleFederationPeerAddMCP(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if deny := mcpFedCap(ctx, federation.CapFederationWrite); deny != nil {
		return deny, nil
	}
	body := map[string]any{
		"name": mustString(req, "name"),
		"url":  mustString(req, "url"),
	}
	if token := mustString(req, "token"); token != "" {
		body["token"] = token
	}
	if caps := mustString(req, "capabilities"); caps != "" {
		body["capabilities"] = strings.Split(caps, ",")
	}
	out, err := s.proxyJSON("POST", "/api/federation/peers", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleFederationPeerGetMCP(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if deny := mcpFedCap(ctx, federation.CapFederationList); deny != nil {
		return deny, nil
	}
	out, err := s.proxyGet("/api/federation/peers/"+mustString(req, "name"), nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleFederationPeerUpdateMCP(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if deny := mcpFedCap(ctx, federation.CapFederationWrite); deny != nil {
		return deny, nil
	}
	name := mustString(req, "name")
	body := map[string]any{"name": name}
	if url := mustString(req, "url"); url != "" {
		body["url"] = url
	}
	if token := mustString(req, "token"); token != "" {
		body["token"] = token
	}
	if caps := mustString(req, "capabilities"); caps != "" {
		body["capabilities"] = strings.Split(caps, ",")
	}
	out, err := s.proxyJSON("PUT", "/api/federation/peers/"+name, body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleFederationPeerDeleteMCP(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if deny := mcpFedCap(ctx, federation.CapFederationWrite); deny != nil {
		return deny, nil
	}
	out, err := s.proxyJSON("DELETE", "/api/federation/peers/"+mustString(req, "name"), nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleFederationPeerTestMCP(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if deny := mcpFedCap(ctx, federation.CapFederationList); deny != nil {
		return deny, nil
	}
	out, err := s.proxyJSON("POST", "/api/federation/peers/"+mustString(req, "name")+"/test", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleFederationGroupListMCP(ctx context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if deny := mcpFedCap(ctx, federation.CapFederationList); deny != nil {
		return deny, nil
	}
	out, err := s.proxyGet("/api/federation/groups", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleFederationGroupListBuiltinsMCP(ctx context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if deny := mcpFedCap(ctx, federation.CapFederationList); deny != nil {
		return deny, nil
	}
	out, err := s.proxyGet("/api/federation/groups/builtins", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleFederationGroupAddMCP(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if deny := mcpFedCap(ctx, federation.CapFederationWrite); deny != nil {
		return deny, nil
	}
	body := map[string]any{
		"name": mustString(req, "name"),
		"caps": strings.Split(mustString(req, "caps"), ","),
	}
	if desc := mustString(req, "description"); desc != "" {
		body["description"] = desc
	}
	out, err := s.proxyJSON("POST", "/api/federation/groups", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleFederationGroupGetMCP(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if deny := mcpFedCap(ctx, federation.CapFederationList); deny != nil {
		return deny, nil
	}
	out, err := s.proxyGet("/api/federation/groups/"+mustString(req, "name"), nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleFederationGroupUpdateMCP(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if deny := mcpFedCap(ctx, federation.CapFederationWrite); deny != nil {
		return deny, nil
	}
	name := mustString(req, "name")
	body := map[string]any{
		"name": name,
		"caps": strings.Split(mustString(req, "caps"), ","),
	}
	if desc := mustString(req, "description"); desc != "" {
		body["description"] = desc
	}
	out, err := s.proxyJSON("PUT", "/api/federation/groups/"+name, body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleFederationGroupDeleteMCP(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if deny := mcpFedCap(ctx, federation.CapFederationWrite); deny != nil {
		return deny, nil
	}
	out, err := s.proxyJSON("DELETE", "/api/federation/groups/"+mustString(req, "name"), nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
