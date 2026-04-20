// BL33 (v3.11.0) — MCP-tool parity for the plugin framework.

package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolPluginsList() mcpsdk.Tool {
	return mcpsdk.NewTool("plugins_list",
		mcpsdk.WithDescription("BL33 — list discovered plugins with enabled/status + invoke stats."),
	)
}
func (s *Server) handlePluginsList(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet("/api/plugins", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolPluginsReload() mcpsdk.Tool {
	return mcpsdk.NewTool("plugins_reload",
		mcpsdk.WithDescription("BL33 — rescan the plugins directory (post-install or config change)."),
	)
}
func (s *Server) handlePluginsReload(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodPost, "/api/plugins/reload", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolPluginGet() mcpsdk.Tool {
	return mcpsdk.NewTool("plugin_get",
		mcpsdk.WithDescription("BL33 — fetch manifest + invocation stats for one plugin."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Plugin name")),
	)
}
func (s *Server) handlePluginGet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	out, err := s.proxyGet("/api/plugins/"+name, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolPluginEnable() mcpsdk.Tool {
	return mcpsdk.NewTool("plugin_enable",
		mcpsdk.WithDescription("BL33 — enable a named plugin."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Plugin name")),
	)
}
func (s *Server) handlePluginEnable(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	out, err := s.proxyJSON(http.MethodPost, "/api/plugins/"+name+"/enable", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolPluginDisable() mcpsdk.Tool {
	return mcpsdk.NewTool("plugin_disable",
		mcpsdk.WithDescription("BL33 — disable a named plugin."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Plugin name")),
	)
}
func (s *Server) handlePluginDisable(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	out, err := s.proxyJSON(http.MethodPost, "/api/plugins/"+name+"/disable", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

func (s *Server) toolPluginTest() mcpsdk.Tool {
	return mcpsdk.NewTool("plugin_test",
		mcpsdk.WithDescription("BL33 — synthetic hook invocation for debugging a plugin."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Plugin name")),
		mcpsdk.WithString("hook", mcpsdk.Required(), mcpsdk.Description("Hook name (pre_session_start | post_session_output | post_session_complete | on_alert)")),
		mcpsdk.WithString("payload", mcpsdk.Description("Optional JSON payload (string) passed as the hook request.")),
	)
}
func (s *Server) handlePluginTest(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	hook := req.GetString("hook", "")
	payloadRaw := req.GetString("payload", "")
	var payload map[string]any
	if payloadRaw != "" {
		if err := json.Unmarshal([]byte(payloadRaw), &payload); err != nil {
			return nil, err
		}
	}
	body := map[string]any{"hook": hook, "payload": payload}
	out, err := s.proxyJSON(http.MethodPost, "/api/plugins/"+name+"/test", body)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
