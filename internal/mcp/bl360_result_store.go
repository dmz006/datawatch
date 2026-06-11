package mcp

// BL360 — structured agent result store MCP tools.
//
// Tools exposed:
//   result_put    — store a named result payload (upsert)
//   result_get    — retrieve a named result
//   result_list   — list results, optionally filtered by prefix
//   result_delete — delete a named result

import (
	"context"
	"encoding/json"
	"fmt"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// toolResultPut returns the result_put tool definition.
func (s *Server) toolResultPut() mcpsdk.Tool {
	return mcpsdk.NewTool("result_put",
		mcpsdk.WithDescription("Store a named result payload in the structured agent result store (BL360). Upserts by name."),
		mcpsdk.WithString("name",
			mcpsdk.Required(),
			mcpsdk.Description("Unique name / key for this result entry."),
		),
		mcpsdk.WithString("payload",
			mcpsdk.Required(),
			mcpsdk.Description("JSON object payload to store. Example: {\"output\":\"done\",\"count\":3}"),
		),
		mcpsdk.WithNumber("ttl_seconds",
			mcpsdk.Description("Time-to-live in seconds. 0 (default) = no expiry."),
		),
	)
}

func (s *Server) handleResultPut(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.resultStore == nil {
		return mcpsdk.NewToolResultText("Result store not available. Start the daemon first."), nil
	}
	name := req.GetString("name", "")
	if name == "" {
		return mcpsdk.NewToolResultText("Error: name is required"), nil
	}
	payloadStr := req.GetString("payload", "")
	if payloadStr == "" {
		return mcpsdk.NewToolResultText("Error: payload is required"), nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadStr), &payload); err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error: invalid payload JSON: %v", err)), nil
	}
	ttlSeconds := int(req.GetFloat("ttl_seconds", 0))
	entry, err := s.resultStore.Put(name, payload, ttlSeconds)
	if err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error: put failed: %v", err)), nil
	}
	b, _ := json.MarshalIndent(entry, "", "  ")
	return mcpsdk.NewToolResultText(string(b)), nil
}

// toolResultGet returns the result_get tool definition.
func (s *Server) toolResultGet() mcpsdk.Tool {
	return mcpsdk.NewTool("result_get",
		mcpsdk.WithDescription("Retrieve a named result from the structured agent result store (BL360)."),
		mcpsdk.WithString("name",
			mcpsdk.Required(),
			mcpsdk.Description("Name / key of the result to retrieve."),
		),
	)
}

func (s *Server) handleResultGet(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.resultStore == nil {
		return mcpsdk.NewToolResultText("Result store not available. Start the daemon first."), nil
	}
	name := req.GetString("name", "")
	if name == "" {
		return mcpsdk.NewToolResultText("Error: name is required"), nil
	}
	entry, ok := s.resultStore.Get(name)
	if !ok {
		return mcpsdk.NewToolResultText("Not found: " + name), nil
	}
	b, _ := json.MarshalIndent(entry, "", "  ")
	return mcpsdk.NewToolResultText(string(b)), nil
}

// toolResultList returns the result_list tool definition.
func (s *Server) toolResultList() mcpsdk.Tool {
	return mcpsdk.NewTool("result_list",
		mcpsdk.WithDescription("List results in the structured agent result store (BL360). Optionally filter by name prefix."),
		mcpsdk.WithString("prefix",
			mcpsdk.Description("Filter results whose name starts with this prefix. Empty = all."),
		),
	)
}

func (s *Server) handleResultList(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.resultStore == nil {
		return mcpsdk.NewToolResultText("Result store not available. Start the daemon first."), nil
	}
	prefix := req.GetString("prefix", "")
	entries := s.resultStore.List(prefix)
	if len(entries) == 0 {
		return mcpsdk.NewToolResultText("No result entries found."), nil
	}
	b, _ := json.MarshalIndent(entries, "", "  ")
	return mcpsdk.NewToolResultText(string(b)), nil
}

// toolResultDelete returns the result_delete tool definition.
func (s *Server) toolResultDelete() mcpsdk.Tool {
	return mcpsdk.NewTool("result_delete",
		mcpsdk.WithDescription("Delete a named result from the structured agent result store (BL360)."),
		mcpsdk.WithString("name",
			mcpsdk.Required(),
			mcpsdk.Description("Name / key of the result to delete."),
		),
	)
}

func (s *Server) handleResultDelete(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if s.resultStore == nil {
		return mcpsdk.NewToolResultText("Result store not available. Start the daemon first."), nil
	}
	name := req.GetString("name", "")
	if name == "" {
		return mcpsdk.NewToolResultText("Error: name is required"), nil
	}
	if err := s.resultStore.Delete(name); err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error: %v", err)), nil
	}
	return mcpsdk.NewToolResultText("Deleted: " + name), nil
}
