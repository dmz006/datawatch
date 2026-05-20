// BL241 P1 — Matrix backend MCP tools.
//
// matrix_status: GET /api/matrix/status
// matrix_test:   POST /api/matrix/test
package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) toolMatrixStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("matrix_status",
		mcpsdk.WithDescription("Get Matrix backend connection status (homeserver, user ID, room, connected/enabled)."),
	)
}

func (s *Server) handleMatrixStatus(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	body, err := s.proxyGet("/api/matrix/status", url.Values{})
	if err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(string(body)), nil
}

func (s *Server) toolMatrixTest() mcpsdk.Tool {
	return mcpsdk.NewTool("matrix_test",
		mcpsdk.WithDescription("Send a test message via the Matrix backend to verify connectivity."),
		mcpsdk.WithString("room", mcpsdk.Description("Target room ID or alias (default: configured room)")),
		mcpsdk.WithString("message", mcpsdk.Description("Message to send (default: 'datawatch test message')")),
	)
}

func (s *Server) handleMatrixTest(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	payload := map[string]interface{}{}
	if room := req.GetString("room", ""); room != "" {
		payload["room"] = room
	}
	if msg := req.GetString("message", ""); msg != "" {
		payload["message"] = msg
	}
	body, err := s.proxyJSON(http.MethodPost, "/api/matrix/test", payload)
	if err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error: %v", err)), nil
	}
	return mcpsdk.NewToolResultText(string(body)), nil
}
