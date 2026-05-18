// BL317 — tests for MCP SSE federated authentication + capability enforcement.

package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// fakePeerStore implements FedPeerStore for tests.
type fakePeerStore struct {
	peers map[string]*multiserver.Entry
}

func (f *fakePeerStore) GetByToken(tok string) (*multiserver.Entry, bool) {
	e, ok := f.peers[tok]
	return e, ok
}

func newFakeStore(entries ...*multiserver.Entry) *fakePeerStore {
	m := make(map[string]*multiserver.Entry, len(entries))
	for _, e := range entries {
		m[e.Token] = e
	}
	return &fakePeerStore{peers: m}
}

// newTestMCPServer creates a minimal Server for testing mcpFedAuthMiddleware.
func newTestMCPServer(adminToken string, store FedPeerStore) *Server {
	s := &Server{
		cfg:          &config.MCPConfig{Token: adminToken},
		fedPeerStore: store,
	}
	// MCPConfig.Token is used by the middleware; mirror into s.cfg.
	return s
}

// ── mcpFedAuthMiddleware tests ────────────────────────────────────────────────

func TestMCPFedAuth_AdminToken_PassThrough(t *testing.T) {
	s := newTestMCPServer("secret", nil)
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := s.mcpFedAuthMiddleware(inner)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Fatal("inner handler not called for admin token")
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
}

func TestMCPFedAuth_UnknownToken_Rejected(t *testing.T) {
	s := newTestMCPServer("secret", newFakeStore())
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler should not be called for unknown token")
	})
	handler := s.mcpFedAuthMiddleware(inner)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer bad-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rr.Code)
	}
}

func TestMCPFedAuth_FedPeerToken_PassThrough(t *testing.T) {
	peer := &multiserver.Entry{
		Name:      "peer-a",
		Token:     "peer-token",
		Federated: true,
		Capabilities: []string{federation.CapSessionsList},
	}
	store := newFakeStore(peer)
	s := newTestMCPServer("admin-secret", store)

	var gotPeer *multiserver.Entry
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPeer = mcpPeerFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	handler := s.mcpFedAuthMiddleware(inner)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer peer-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	if gotPeer == nil {
		t.Fatal("peer not tagged in context")
	}
	if gotPeer.Name != "peer-a" {
		t.Fatalf("context peer name = %q, want %q", gotPeer.Name, "peer-a")
	}
}

func TestMCPFedAuth_NonFederatedPeer_Rejected(t *testing.T) {
	// A token that exists in the store but Federated=false should be rejected.
	peer := &multiserver.Entry{
		Name:      "plain-server",
		Token:     "plain-token",
		Federated: false,
	}
	store := newFakeStore(peer)
	s := newTestMCPServer("admin-secret", store)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner should not be called for non-federated token")
	})
	handler := s.mcpFedAuthMiddleware(inner)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer plain-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rr.Code)
	}
}

func TestMCPFedAuth_QueryParamToken(t *testing.T) {
	s := newTestMCPServer("secret", nil)
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := s.mcpFedAuthMiddleware(inner)

	req := httptest.NewRequest("GET", "/?token=secret", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Fatal("query-param admin token not accepted")
	}
}

func TestMCPFedAuth_NoAuthConfigured_Open(t *testing.T) {
	// When no admin token and no peer store, access is open.
	s := newTestMCPServer("", nil)
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := s.mcpFedAuthMiddleware(inner)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Fatal("open server should let unauthenticated requests through")
	}
}

// ── mcpFedCap tests ───────────────────────────────────────────────────────────

func TestMCPFedCap_Admin_AlwaysPasses(t *testing.T) {
	// No peer in context → admin → must pass.
	ctx := context.Background()
	if deny := mcpFedCap(ctx, federation.CapSessionsKill); deny != nil {
		t.Fatal("admin should always pass fedCap check")
	}
}

func TestMCPFedCap_PeerWithCap_Passes(t *testing.T) {
	peer := &multiserver.Entry{
		Name:         "peer-b",
		Federated:    true,
		Capabilities: []string{federation.CapSessionsList, federation.CapSessionsRead},
	}
	ctx := context.WithValue(context.Background(), mcpFedPeerKey, peer)
	if deny := mcpFedCap(ctx, federation.CapSessionsList); deny != nil {
		t.Fatalf("peer with cap should pass, got deny: %v", deny)
	}
}

func TestMCPFedCap_PeerWithoutCap_Denied(t *testing.T) {
	peer := &multiserver.Entry{
		Name:         "peer-c",
		Federated:    true,
		Capabilities: []string{federation.CapSessionsList}, // no kill
	}
	ctx := context.WithValue(context.Background(), mcpFedPeerKey, peer)
	deny := mcpFedCap(ctx, federation.CapSessionsKill)
	if deny == nil {
		t.Fatal("peer without cap should be denied")
	}
	if !deny.IsError {
		t.Fatal("denied result should have IsError=true")
	}
	if len(deny.Content) == 0 {
		t.Fatal("denied result should have non-empty content")
	}
	tc, ok := deny.Content[0].(mcpsdk.TextContent)
	if !ok {
		t.Fatal("denied result content should be TextContent")
	}
	if !strings.Contains(tc.Text, "sessions:kill") {
		t.Errorf("denied message should mention required cap, got: %s", tc.Text)
	}
}

func TestMCPFedCap_PeerWithGroupCap_Passes(t *testing.T) {
	// "read-only" group expands to multiple caps including sessions:list.
	peer := &multiserver.Entry{
		Name:         "peer-d",
		Federated:    true,
		Capabilities: []string{"read-only"}, // built-in group
	}
	ctx := context.WithValue(context.Background(), mcpFedPeerKey, peer)
	if deny := mcpFedCap(ctx, federation.CapSessionsList); deny != nil {
		t.Fatalf("peer with read-only group should pass sessions:list check, got deny: %v", deny)
	}
}

// ── handler-level fedCap integration ─────────────────────────────────────────

func TestHandleStartSession_FedCap_Denied(t *testing.T) {
	peer := &multiserver.Entry{
		Name:         "reader",
		Federated:    true,
		Capabilities: []string{federation.CapSessionsList}, // no write
	}
	s := &Server{cfg: &config.MCPConfig{}}
	ctx := context.WithValue(context.Background(), mcpFedPeerKey, peer)
	result, err := s.handleStartSession(ctx, mcpsdk.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("start_session should be denied for peer without sessions:write")
	}
}

func TestHandleKillSession_FedCap_Denied(t *testing.T) {
	peer := &multiserver.Entry{
		Name:         "reader",
		Federated:    true,
		Capabilities: []string{federation.CapSessionsList},
	}
	s := &Server{cfg: &config.MCPConfig{}}
	ctx := context.WithValue(context.Background(), mcpFedPeerKey, peer)
	result, err := s.handleKillSession(ctx, mcpsdk.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("kill_session should be denied for peer without sessions:kill")
	}
}

func TestHandleRestartDaemon_FedCap_Denied(t *testing.T) {
	peer := &multiserver.Entry{
		Name:         "reader",
		Federated:    true,
		Capabilities: []string{federation.CapSessionsList},
	}
	s := &Server{cfg: &config.MCPConfig{}}
	ctx := context.WithValue(context.Background(), mcpFedPeerKey, peer)
	result, err := s.handleRestartDaemon(ctx, mcpsdk.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("restart_daemon should be denied for peer without config:write")
	}
}
