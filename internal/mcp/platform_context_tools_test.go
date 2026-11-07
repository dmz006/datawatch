package mcp

import (
	"context"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/federation"
)

func TestPlatformContextStatusToolSchema(t *testing.T) {
	s := &Server{}
	tool := s.toolPlatformContextStatus()
	if tool.Name != "platform_context_status" {
		t.Errorf("name = %q", tool.Name)
	}
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("tool must be read-only")
	}
	if federation.MCPToolCap[tool.Name] != federation.CapConfigRead {
		t.Errorf("capability = %q", federation.MCPToolCap[tool.Name])
	}
	if len(PlatformContextToolNames()) != 1 {
		t.Error("PlatformContextToolNames must list the tool")
	}
}

func TestPlatformContextStatusNoManager(t *testing.T) {
	s := &Server{}
	res, err := s.handlePlatformContextStatus(context.Background(), mcpsdk.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("want an error result when no manager is set")
	}
}
