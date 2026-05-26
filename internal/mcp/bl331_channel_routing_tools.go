// BL331 parity — channel_routing_config_get and channel_routing_config_set MCP tools.
//
// Forwards to GET/PUT /api/channel/routing so sessions can inspect and
// modify channel→peer routing rules without shelling out to the REST API.

package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolChannelRoutingConfigGet() mcpsdk.Tool {
	return mcpsdk.NewTool("channel_routing_config_get",
		mcpsdk.WithDescription("List channel routing rules: pattern→peer mappings that route inbound channel commands to federation peers."),
	)
}

func (s *Server) handleChannelRoutingConfigGet(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodGet, "/api/channel/routing", nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolChannelRoutingConfigSet() mcpsdk.Tool {
	return mcpsdk.NewTool("channel_routing_config_set",
		mcpsdk.WithDescription("Replace channel routing rules. Each rule maps a channel_pattern (regex) to a peer_name. Optional: automata_type, default_project_dir."),
		mcpsdk.WithString("rules_json",
			mcpsdk.Required(),
			mcpsdk.Description(`JSON array of rules, e.g. [{"channel_pattern":"alerts-*","peer_name":"peer-alpha","automata_type":"operational"}]`),
		),
	)
}

func (s *Server) handleChannelRoutingConfigSet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	raw := req.GetString("rules_json", "")
	if raw == "" {
		return textOK("Error: rules_json is required"), nil
	}
	var rules []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return textOK("Error: rules_json must be a JSON array: " + err.Error()), nil
	}
	body := map[string]any{"rules": rules}
	out, err := s.proxyJSON(http.MethodPut, "/api/channel/routing", body)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}
