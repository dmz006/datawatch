// Design A3 — per-tool MCP capability gating at the REST chokepoint the
// channel bridge actually uses (POST /api/mcp/call, GET /api/mcp/tools;
// see cmd/datawatch-channel/proxy_tools.go). Before this, handleMCPCall
// gated EVERY tool behind one blanket comm:write check (BL316-followup's
// own "per-tool capability mapping is tracked" note) — this is that
// follow-up.

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/auth"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// fakeMCPBridge is a minimal mcpBridgeAPI stub covering just enough for
// these tests: a fixed two-tool catalog (one session-safe, one
// admin-only, per federation.MCPToolCap) and an echo-back call result.
type fakeMCPBridge struct{}

func (fakeMCPBridge) MCPToolsJSON() ([]byte, error) {
	return json.Marshal([]map[string]string{
		{"name": "get_my_session_id", "description": "session-safe (health:read)"},
		{"name": "secret_set", "description": "admin-only (secrets:write)"},
	})
}

func (fakeMCPBridge) MCPCallJSON(_ context.Context, name string, _ map[string]any) ([]byte, error) {
	return json.Marshal(map[string]any{"ok": true, "tool": name})
}

func (fakeMCPBridge) ResourcesJSON() ([]byte, error) { return []byte("[]"), nil }
func (fakeMCPBridge) ResourceReadJSON(context.Context, string) ([]byte, error) {
	return []byte("{}"), nil
}
func (fakeMCPBridge) ResourceTemplatesJSON() ([]byte, error) { return []byte("[]"), nil }
func (fakeMCPBridge) PromptsListJSON() []byte                { return []byte("[]") }
func (fakeMCPBridge) PromptsGetJSON(context.Context, string, map[string]string) ([]byte, error) {
	return []byte("{}"), nil
}

func newMCPCapTestServer(t *testing.T) (*Server, *multiserver.Store) {
	t.Helper()
	s, store, _ := newFedTestServer(t)
	s.mcpBridge = fakeMCPBridge{}
	store2, err := auth.NewSessionTokenStore("")
	if err != nil {
		t.Fatalf("session token store: %v", err)
	}
	s.sessionTokens = store2
	return s, store
}

func callMCP(s *Server, method, path, token, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	var handler http.Handler
	if path == "/api/mcp/tools" {
		handler = s.fedAuthMiddleware(http.HandlerFunc(s.handleMCPTools))
	} else {
		handler = s.fedAuthMiddleware(http.HandlerFunc(s.handleMCPCall))
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestMCPCall_Admin_CanCallAnyTool(t *testing.T) {
	s, _ := newMCPCapTestServer(t)
	rr := callMCP(s, http.MethodPost, "/api/mcp/call", "admin-token", `{"tool":"secret_set","args":{}}`)
	if rr.Code != http.StatusOK {
		t.Errorf("admin calling an admin-only tool should be 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMCPCall_SessionToken_SessionSafeToolAllowed(t *testing.T) {
	s, _ := newMCPCapTestServer(t)
	tok, err := s.sessionTokens.Mint("sess-1", federation.Resolve([]string{"session-default"}, nil))
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	rr := callMCP(s, http.MethodPost, "/api/mcp/call", tok, `{"tool":"get_my_session_id","args":{}}`)
	if rr.Code != http.StatusOK {
		t.Errorf("session token calling a session-safe tool should be 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMCPCall_SessionToken_AdminOnlyToolRejected(t *testing.T) {
	s, _ := newMCPCapTestServer(t)
	tok, err := s.sessionTokens.Mint("sess-1", federation.Resolve([]string{"session-default"}, nil))
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	rr := callMCP(s, http.MethodPost, "/api/mcp/call", tok, `{"tool":"secret_set","args":{}}`)
	if rr.Code != http.StatusForbidden {
		t.Errorf("session token calling secret_set (admin-only) should be 403, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMCPCall_UnknownTool_404RegardlessOfCaller(t *testing.T) {
	s, _ := newMCPCapTestServer(t)
	rr := callMCP(s, http.MethodPost, "/api/mcp/call", "admin-token", `{"tool":"this_tool_does_not_exist","args":{}}`)
	if rr.Code != http.StatusNotFound {
		t.Errorf("an unknown tool name should be 404 even for admin, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMCPTools_SessionToken_ListIsFilteredToItsCapabilities(t *testing.T) {
	s, _ := newMCPCapTestServer(t)
	tok, err := s.sessionTokens.Mint("sess-1", federation.Resolve([]string{"session-default"}, nil))
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	rr := callMCP(s, http.MethodGet, "/api/mcp/tools", tok, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var tools []map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &tools); err != nil {
		t.Fatalf("decode: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool["name"]] = true
	}
	if !names["get_my_session_id"] {
		t.Error("expected get_my_session_id (session-safe) to be listed")
	}
	if names["secret_set"] {
		t.Error("expected secret_set (admin-only) to be filtered OUT of a session token's tool list")
	}
}

func TestMCPTools_Admin_ListIsUnfiltered(t *testing.T) {
	s, _ := newMCPCapTestServer(t)
	rr := callMCP(s, http.MethodGet, "/api/mcp/tools", "admin-token", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var tools []map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &tools); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tools) != 2 {
		t.Errorf("expected the admin to see the full unfiltered 2-tool catalog, got %d", len(tools))
	}
}
