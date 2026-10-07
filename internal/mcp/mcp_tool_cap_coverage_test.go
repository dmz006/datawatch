// TS-396 (BL317 E2E implementation pass, 2026-10-07) found that
// server_list -- despite being properly AddTool-registered in
// RegisterServerTools (bl312_server_tools.go) and reachable over the
// real stdio/SSE MCP protocol -- was rejected with "unknown tool" by
// POST /api/mcp/call, the REST chokepoint channel bridges and the CLI
// actually use (internal/server/api.go's handleMCPCall). Root cause:
// federation.MCPToolCap (internal/federation/mcp_tool_caps.go) had no
// entry for any of the 6 BL312 server_* tools, and that file's own
// top-of-file comment's claimed safety net --
// "internal/server/route_caps_a3_test.go asserts every tool
// AddTool-registered in internal/mcp/server.go has an entry here" --
// turned out to reference a test file that does not exist anywhere in
// this tree. internal/server/mcp_bridge_cap_test.go, which sounds like
// it might be that test, actually exercises a fakeMCPBridge with a
// hardcoded 2-tool catalog, so it could never have caught a real tool
// missing from the map.
//
// This is the real version of that promised test: it constructs an
// actual mcp.Server via New() (same constructor production uses,
// exercising every unconditionally-registered AddTool call) and
// asserts every tool it returns from ListTools() has a MCPToolCap
// entry. Tool groups wired in only via a later Set*API call (e.g.
// SetMemoryAPI) aren't covered here since this constructs a server
// with every such API left nil -- partial coverage going forward is
// still strictly better than the zero coverage that let this gap ship.
package mcp

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

func TestEveryUnconditionallyRegisteredToolHasAnMCPToolCapEntry(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	mgr.WithFakeTmux()

	s := New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test"})

	tools, err := s.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("expected a non-empty tool catalog from a real New() server")
	}

	var missing []string
	for _, tool := range tools {
		if _, ok := federation.MCPToolCap[tool.Name]; !ok {
			missing = append(missing, tool.Name)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d registered MCP tool(s) have no federation.MCPToolCap entry -- "+
			"POST /api/mcp/call rejects them with 404 \"unknown tool\" even though "+
			"they're properly registered and reachable over stdio/SSE: %v", len(missing), missing)
	}
}
