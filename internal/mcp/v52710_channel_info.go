// v5.27.10 (BL216) — channel_info MCP tool.
//
// Forwards to GET /api/channel/info so IDE-side claude-code sessions
// can ask "which bridge am I plumbed through" without shelling out to
// `claude mcp list` or grepping the daemon log.

package mcp

import (
	"context"
	"net/http"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolChannelInfo() mcpsdk.Tool {
	return mcpsdk.NewTool("channel_info",
		mcpsdk.WithDescription("Daemon's resolved MCP-channel bridge: kind (go|js), path, ready state, plus any stale .mcp.json files that point at a missing channel.js. Read-only."),
	)
}

func (s *Server) handleChannelInfo(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodGet, "/api/channel/info", nil)
	if err != nil {
		return textOK("Error: " + err.Error()), nil
	}
	return textOK(string(out)), nil
}
