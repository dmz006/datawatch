// BL312 S1 — MCP tools for the multi-server registry. All proxy to REST.
//
//	server_list    — list every registered server
//	server_add     — create a runtime entry
//	server_get     — fetch one entry by name
//	server_update  — replace a runtime entry
//	server_delete  — remove a runtime entry
//	server_test    — ping the named server, return latency + version

package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/federation"
)

// RegisterServerTools adds all BL312 server-registry MCP tools to the
// server. Called from New() after the other tool groups are registered.
func (s *Server) RegisterServerTools() {
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

	s.srv.AddTool(s.toolServerList(), tracked(s.handleServerListMCP))
	s.srv.AddTool(s.toolServerAdd(), tracked(s.handleServerAddMCP))
	s.srv.AddTool(s.toolServerGet(), tracked(s.handleServerGetMCP))
	s.srv.AddTool(s.toolServerUpdate(), tracked(s.handleServerUpdateMCP))
	s.srv.AddTool(s.toolServerDelete(), tracked(s.handleServerDeleteMCP))
	s.srv.AddTool(s.toolServerTest(), tracked(s.handleServerTestMCP))
}

// ---------------------------------------------------------------------------
// Tool descriptors

func (s *Server) toolServerList() mcpsdk.Tool {
	return mcpsdk.NewTool("server_list",
		mcpsdk.WithDescription("BL312 S1 — list every registered remote datawatch server (YAML seeds + runtime entries)."),
	)
}

func (s *Server) toolServerAdd() mcpsdk.Tool {
	return mcpsdk.NewTool("server_add",
		mcpsdk.WithDescription("BL312 S1 — register a new remote datawatch server at runtime. Persisted to servers.json."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("short identifier, e.g. 'prod' or 'pi'")),
		mcpsdk.WithString("url", mcpsdk.Required(), mcpsdk.Description("base URL of the remote instance, e.g. 'http://203.0.113.10:8080'")),
		mcpsdk.WithString("token", mcpsdk.Description("bearer token for the remote instance (optional)")),
		mcpsdk.WithString("label", mcpsdk.Description("human-readable label (optional)")),
		mcpsdk.WithBoolean("enabled", mcpsdk.Description("whether the server is active (default true)")),
	)
}

func (s *Server) toolServerGet() mcpsdk.Tool {
	return mcpsdk.NewTool("server_get",
		mcpsdk.WithDescription("BL312 S1 — fetch one registered server by name."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("server name")),
	)
}

func (s *Server) toolServerUpdate() mcpsdk.Tool {
	return mcpsdk.NewTool("server_update",
		mcpsdk.WithDescription("BL312 S1 — replace a runtime server entry. YAML-seeded (builtin) entries cannot be updated."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("server name")),
		mcpsdk.WithString("url", mcpsdk.Description("new base URL")),
		mcpsdk.WithString("token", mcpsdk.Description("new bearer token")),
		mcpsdk.WithString("label", mcpsdk.Description("human-readable label")),
		mcpsdk.WithBoolean("enabled", mcpsdk.Description("whether the server is active")),
	)
}

func (s *Server) toolServerDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("server_delete",
		mcpsdk.WithDescription("BL312 S1 — remove a runtime server entry. YAML-seeded (builtin) entries cannot be deleted."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("server name")),
	)
}

func (s *Server) toolServerTest() mcpsdk.Tool {
	return mcpsdk.NewTool("server_test",
		mcpsdk.WithDescription("BL312 S1 — ping a registered server's /api/health endpoint. Returns latency_ms, version, ok."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("server name")),
	)
}

// ---------------------------------------------------------------------------
// Handlers (proxy to REST)

func (s *Server) handleServerListMCP(ctx context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if deny := mcpFedCap(ctx, federation.CapFederationList); deny != nil {
		return deny, nil
	}
	out, err := s.proxyGet("/api/servers", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleServerGetMCP(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if deny := mcpFedCap(ctx, federation.CapFederationList); deny != nil {
		return deny, nil
	}
	out, err := s.proxyGet("/api/servers/"+mustString(req, "name"), nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleServerAddMCP(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if deny := mcpFedCap(ctx, federation.CapFederationWrite); deny != nil {
		return deny, nil
	}
	enabled := req.GetBool("enabled", true)
	body := map[string]any{
		"name":    mustString(req, "name"),
		"url":     mustString(req, "url"),
		"enabled": enabled,
	}
	if token := mustString(req, "token"); token != "" {
		body["token"] = token
	}
	if label := mustString(req, "label"); label != "" {
		body["label"] = label
	}
	out, err := s.proxyJSON("POST", "/api/servers", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleServerUpdateMCP(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
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
	if label := mustString(req, "label"); label != "" {
		body["label"] = label
	}
	// Only include enabled if the caller explicitly passed it.
	if argsMap, ok := req.Params.Arguments.(map[string]any); ok {
		if _, has := argsMap["enabled"]; has {
			body["enabled"] = req.GetBool("enabled", true)
		}
	}
	out, err := s.proxyJSON("PUT", "/api/servers/"+name, body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleServerDeleteMCP(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if deny := mcpFedCap(ctx, federation.CapFederationWrite); deny != nil {
		return deny, nil
	}
	out, err := s.proxyJSON("DELETE", "/api/servers/"+mustString(req, "name"), nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) handleServerTestMCP(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if deny := mcpFedCap(ctx, federation.CapFederationList); deny != nil {
		return deny, nil
	}
	out, err := s.proxyJSON("POST", "/api/servers/"+mustString(req, "name")+"/test", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
