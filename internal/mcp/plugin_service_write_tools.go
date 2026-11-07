// MCP write tools over the plugin-service lifecycle and plugin token
// rotation the REST layer already serves: start/stop/restart of a
// plugin's managed service (the enable route with the managed opt-in
// body — enable+managed:true starts the supervised child,
// enable+managed:false stops it and keeps the plugin itself enabled),
// and per-secret token rotation. They proxy the same handlers the
// operator-facing routes use, so auth, capability, attach-only refusal
// and value-redaction behaviour stay owned by the server package.

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// attachOnlyRefusal rewrites the server package's 400 body — which is
// already a refusal — into the message an agent should act on: the
// plugin's manifest does not offer the managed mode, so none of the
// lifecycle tools can apply to it. Any other errors pass through
// untouched.
func attachOnlyRefusal(name string, err error) error {
	if err != nil && strings.Contains(err.Error(), "does not declare managed lifecycle support") {
		return fmt.Errorf("refused: plugin %q is attach-only (its manifest does not declare the managed lifecycle mode), so datawatch cannot start, stop or restart its service — only plugins that list \"managed\" in lifecycle.supported_modes accept these tools", name)
	}
	return err
}

// toolPluginServiceStart — opt the plugin into managed supervision: the
// daemon launches the lifecycle entry and health-checks/restarts it per
// the manifest policy. POST /api/plugins/{name}/enable with
// {"managed": true}; the REST handler refuses attach-only plugins.
func (s *Server) toolPluginServiceStart() mcpsdk.Tool {
	return mcpsdk.NewTool("plugin_service_start",
		mcpsdk.WithDescription("Start the managed service of a plugin: datawatch launches its lifecycle entry and supervises it (health checks, restart policy). Refused with a clear error when the plugin is attach-only or does not declare managed lifecycle support."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Plugin name")),
	)
}
func (s *Server) handlePluginServiceStart(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/plugins/"+name+"/enable", map[string]any{"managed": true})
	if err != nil {
		return nil, attachOnlyRefusal(name, err)
	}
	return textOK(string(out)), nil
}

// toolPluginServiceStop — stop the managed service (opt out of managed
// supervision) while leaving the plugin enabled. POST
// /api/plugins/{name}/enable with {"managed": false}; attach-only
// plugins are refused by the REST handler.
func (s *Server) toolPluginServiceStop() mcpsdk.Tool {
	return mcpsdk.NewTool("plugin_service_stop",
		mcpsdk.WithDescription("Stop the managed service of a plugin: datawatch terminates the supervised child (per its stop timeout) and leaves the plugin enabled. Refused with a clear error when the plugin is attach-only or does not declare managed lifecycle support."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Plugin name")),
	)
}
func (s *Server) handlePluginServiceStop(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	out, err := s.proxyJSON(http.MethodPost, "/api/plugins/"+name+"/enable", map[string]any{"managed": false})
	if err != nil {
		return nil, attachOnlyRefusal(name, err)
	}
	return textOK(string(out)), nil
}

// toolPluginServiceRestart — stop then start via the same managed
// opt-in body, in that order. If the stop leg is refused (attach-only),
// the start leg is never attempted and the refusal is surfaced.
func (s *Server) toolPluginServiceRestart() mcpsdk.Tool {
	return mcpsdk.NewTool("plugin_service_restart",
		mcpsdk.WithDescription("Restart the managed service of a plugin: stop it (opt out of managed supervision) and immediately start it again. Refused with a clear error when the plugin is attach-only or does not declare managed lifecycle support."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Plugin name")),
	)
}
func (s *Server) handlePluginServiceRestart(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	stopOut, err := s.proxyJSON(http.MethodPost, "/api/plugins/"+name+"/enable", map[string]any{"managed": false})
	if err != nil {
		return nil, attachOnlyRefusal(name, err)
	}
	startOut, err := s.proxyJSON(http.MethodPost, "/api/plugins/"+name+"/enable", map[string]any{"managed": true})
	if err != nil {
		return nil, attachOnlyRefusal(name, fmt.Errorf("stop succeeded but start failed again: %v", err))
	}
	stopJSON := strings.TrimSpace(string(stopOut))
	startJSON := strings.TrimSpace(string(startOut))
	if stopJSON == "" {
		return textOK(startJSON), nil
	}
	res := textOK(fmt.Sprintf(`{"stop":%s,"start":%s}`, stopJSON, startJSON))
	if len(res.Content) > 0 {
		return res, nil
	}
	// The combined view is best-effort; fall back to the
	// authoritative start response verbatim.
	return textOK(startJSON), nil
}

// toolPluginTokenRotate — POST
// /api/plugins/{name}/tokens/{secret}/rotate with the new value. The
// REST handler stores it through the secrets store, revokes+re-mints
// the service token when the same name is registered, audits the
// action, and returns only the status/plugin/secret names. This tool
// re-emits exactly those three fields and nothing else, so the new
// value never appears in the tool response regardless of what the
// upstream sent.
func (s *Server) toolPluginTokenRotate() mcpsdk.Tool {
	return mcpsdk.NewTool("plugin_token_rotate",
		mcpsdk.WithDescription("Rotate one secret a plugin's manifest references: stores the new value, and when the name is also registered as a service token revokes the old token and mints a fresh one under the same name. The new value is sent to the server and never returned: the tool response carries only status, plugin and secret names."),
		mcpsdk.WithString("name", mcpsdk.Required(), mcpsdk.Description("Plugin name")),
		mcpsdk.WithString("secret", mcpsdk.Required(), mcpsdk.Description("Secret name as referenced by the plugin's manifest (e.g. connection.auth.token, env.<KEY>)")),
		mcpsdk.WithString("new_value", mcpsdk.Required(), mcpsdk.Description("The new secret value. Sent to the server for storage; never echoed back.")),
	)
}
func (s *Server) handlePluginTokenRotate(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.GetString("name", "")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	secret := req.GetString("secret", "")
	if secret == "" {
		return nil, fmt.Errorf("secret is required")
	}
	value := req.GetString("new_value", "")
	if value == "" {
		return nil, fmt.Errorf("new_value is required")
	}
	path := "/api/plugins/" + name + "/tokens/" + url.PathEscape(secret) + "/rotate"
	out, err := s.proxyJSON(http.MethodPost, path, map[string]any{"value": value})
	if err != nil {
		return nil, err
	}
	// Never echo the value: re-serialize the response through a fixed
	// field list instead of passing the upstream body through verbatim.
	var res struct {
		Status string `json:"status"`
		Plugin string `json:"plugin"`
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, fmt.Errorf("unexpected rotate response from the server: %v", err)
	}
	if res.Status == "" {
		return nil, fmt.Errorf("server did not confirm the rotation (no status in response)")
	}
	outBytes, err := json.Marshal(res)
	if err != nil {
		return nil, err
	}
	return textOK(string(outBytes)), nil
}
