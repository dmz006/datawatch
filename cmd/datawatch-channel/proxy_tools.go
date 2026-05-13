// proxy_tools.go — dynamic proxy helpers for the datawatch channel bridge.
//
// At startup, discoverTools fetches all daemon MCP tools via GET /api/mcp/tools
// and registers a generic forwarding handler for each one via makeForwarder.
// Tool calls are dispatched to POST /api/mcp/call on the daemon, which executes
// the tool in-process and returns the result as JSON.
//
// The reply tool is the only hardcoded stub — it is outbound-only and not
// reachable via the daemon tool surface.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// daemonTool is a tool descriptor returned by GET /api/mcp/tools.
type daemonTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// asMCPTool converts a daemonTool to an MCP tool using the raw JSON schema.
func (d daemonTool) asMCPTool() mcpsdk.Tool {
	return mcpsdk.NewToolWithRawSchema(d.Name, d.Description, d.InputSchema)
}

// discoverTools fetches the daemon's full MCP tool list via GET /api/mcp/tools.
func (b *bridge) discoverTools() ([]daemonTool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := b.callParent(ctx, http.MethodGet, "/api/mcp/tools", nil)
	if err != nil {
		return nil, fmt.Errorf("GET /api/mcp/tools: %w", err)
	}
	var tools []daemonTool
	if err := json.Unmarshal(out, &tools); err != nil {
		return nil, fmt.Errorf("parse tool list: %w", err)
	}
	return tools, nil
}

// makeForwarder returns a ToolHandlerFunc that forwards the call to POST /api/mcp/call.
func (b *bridge) makeForwarder(toolName string) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := req.GetArguments()
		if args == nil {
			args = map[string]any{}
		}
		payload := map[string]any{
			"tool": toolName,
			"args": args,
		}
		out, err := b.callParent(ctx, http.MethodPost, "/api/mcp/call", payload)
		if err != nil {
			return mcpsdk.NewToolResultError(fmt.Sprintf("%s: %v", toolName, err)), nil
		}
		// Decode and relay the daemon's result.
		var result struct {
			Content []json.RawMessage `json:"content"`
			IsError bool              `json:"isError"`
		}
		if err := json.Unmarshal(out, &result); err != nil {
			return mcpsdk.NewToolResultText(string(out)), nil
		}
		var content []mcpsdk.Content
		for _, raw := range result.Content {
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				continue
			}
			if typ, _ := m["type"].(string); typ == "text" {
				text, _ := m["text"].(string)
				content = append(content, mcpsdk.TextContent{Type: "text", Text: text})
			}
		}
		if len(content) == 0 {
			content = []mcpsdk.Content{mcpsdk.TextContent{Type: "text", Text: string(out)}}
		}
		return &mcpsdk.CallToolResult{Content: content, IsError: result.IsError}, nil
	}
}
