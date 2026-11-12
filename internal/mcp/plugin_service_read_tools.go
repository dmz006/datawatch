// Read-only MCP tools over the plugin service surfaces the REST layer
// already serves (BL33 / BL403): one plugin's service status object
// (state, mode, address, version, uptime, last health check), its
// declared widgets with latest values, and the token usage listing.
// They proxy the same handlers — /api/plugins/{name},
// /api/plugins/{name}/widgets[...] and /api/plugins/{name}/tokens —
// rather than reimplementing them, so auth, capability and value-redaction
// behaviour stays owned by the server package.

package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// toolPluginServiceStatus — the service object of one plugin: state,
// mode, address, version, uptime and last health check. GET
// /api/plugins/{name} carries that object; the tool returns the same
// response so non-service plugins simply report no service object.
func (s *Server) toolPluginServiceStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("plugin_service_status",
		mcpsdk.WithDescription("Read one plugin's service status: mode (managed | attach_only), state, address, version, uptime and last health check, as reported by GET /api/plugins/{name}."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Plugin name")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}
func (s *Server) handlePluginServiceStatus(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	out, err := s.proxyGet("/api/plugins/"+name, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// toolPluginWidgetList — the plugin's declared widgets with their latest
// stored value each (nil when none has arrived yet).
func (s *Server) toolPluginWidgetList() mcpsdk.Tool {
	return mcpsdk.NewTool("plugin_widget_list",
		mcpsdk.WithDescription("List one plugin's declared widgets with their latest stored values, as returned by GET /api/plugins/{name}/widgets."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Plugin name")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}
func (s *Server) handlePluginWidgetList(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	out, err := s.proxyGet("/api/plugins/"+name+"/widgets", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// toolPluginWidgetGet — one declared widget plus its latest value.
func (s *Server) toolPluginWidgetGet() mcpsdk.Tool {
	return mcpsdk.NewTool("plugin_widget_get",
		mcpsdk.WithDescription("Fetch one plugin widget (declaration + latest stored value), as returned by GET /api/plugins/{name}/widgets/{widget_id}."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Plugin name")),
		mcpsdk.WithString("widget_id", mcpsdk.Required(), mcpsdk.Description("Widget id as declared by the plugin's manifest")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}
func (s *Server) handlePluginWidgetGet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	widgetID := req.GetString("widget_id", "")
	if widgetID == "" {
		return nil, fmt.Errorf("widget_id is required")
	}
	out, err := s.proxyGet("/api/plugins/"+name+"/widgets/"+widgetID, nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// toolPluginTokenList — token usage for one plugin: the secret names the
// manifest references plus the service token names, with existence and
// creation times. The REST handler (handlePluginTokens) never returns a
// secret or token value, and this tool does not change that.
func (s *Server) toolPluginTokenList() mcpsdk.Tool {
	return mcpsdk.NewTool("plugin_token_list",
		mcpsdk.WithDescription("List one plugin's token usage: the secret names its manifest references (with usage fields and store existence) and the service token names minted for it. Never returns a secret or token value — the same response as GET /api/plugins/{name}/tokens."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Plugin name")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}
func (s *Server) handlePluginTokenList(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	out, err := s.proxyGet("/api/plugins/"+name+"/tokens", nil)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}
