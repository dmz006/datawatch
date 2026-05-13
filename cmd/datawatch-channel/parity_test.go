// parity_test.go — integration test for dynamic tool discovery.
//
// The channel bridge no longer maintains static tool stubs; instead it
// discovers all daemon tools at startup via GET /api/mcp/tools and
// registers generic forwarding handlers. This test verifies that discovery
// works against a mock HTTP server.
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

// TestDynamicDiscovery starts a mock daemon, registers two tools via
// discoverTools, and checks that both appeared in the MCP server.
func TestDynamicDiscovery(t *testing.T) {
	mockTools := []daemonTool{
		{
			Name:        "memory_recall",
			Description: "Semantic search across episodic memory.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
		},
		{
			Name:        "list_sessions",
			Description: "List all sessions on this host.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/mcp/tools" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockTools) //nolint:errcheck
	}))
	defer srv.Close()

	mcpSrv := server.NewMCPServer(bridgeName, bridgeVersion, server.WithToolCapabilities(true))
	b := &bridge{
		cfg: config{apiURL: srv.URL},
		srv: mcpSrv,
	}

	tools, err := b.discoverTools()
	if err != nil {
		t.Fatalf("discoverTools: %v", err)
	}
	if len(tools) != len(mockTools) {
		t.Fatalf("got %d tools, want %d", len(tools), len(mockTools))
	}

	for _, tool := range tools {
		mcpSrv.AddTool(tool.asMCPTool(), b.makeForwarder(tool.Name))
	}

	registered := mcpSrv.ListTools()
	for _, want := range mockTools {
		if _, ok := registered[want.Name]; !ok {
			t.Errorf("tool %q not registered after discovery", want.Name)
		}
	}
}
