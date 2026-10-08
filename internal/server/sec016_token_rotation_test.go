// SEC-016 — POST /api/auth/rotate-token hot-swaps the live admin bearer
// token with a grace window on the previous one, and server.token/mcp.token
// are no longer settable through the generic PUT /api/config patch path.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

func newRotationTestServer(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t, nil, nil)
	s.token = "admin-token"
	cfg := config.DefaultConfig()
	cfg.Server.Token = "admin-token"
	s.cfg = cfg
	s.cfgPath = filepath.Join(t.TempDir(), "config.yaml")
	return s
}

func TestHandleRotateToken_AdminGetsNewTokenImmediately(t *testing.T) {
	s := newRotationTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/rotate-token", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	rr := httptest.NewRecorder()
	s.fedAuthMiddleware(http.HandlerFunc(s.handleRotateToken)).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Token == "" || body.Token == "admin-token" {
		t.Fatalf("expected a freshly generated token, got %q", body.Token)
	}

	// The new token must work immediately -- no restart needed.
	req2 := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req2.Header.Set("Authorization", "Bearer "+body.Token)
	rr2 := httptest.NewRecorder()
	s.fedAuthMiddleware(http.HandlerFunc(s.handleHealthz)).ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("new token should authenticate immediately, got %d", rr2.Code)
	}

	// Config on disk must reflect the new token.
	saved, err := config.Load(s.cfgPath)
	if err != nil {
		t.Fatalf("load saved config: %v", err)
	}
	if saved.Server.Token != body.Token {
		t.Fatalf("saved config token=%q, want %q", saved.Server.Token, body.Token)
	}
}

func TestHandleRotateToken_OldTokenValidDuringGraceWindow(t *testing.T) {
	s := newRotationTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/rotate-token", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	rr := httptest.NewRecorder()
	s.fedAuthMiddleware(http.HandlerFunc(s.handleRotateToken)).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("rotate: want 200, got %d", rr.Code)
	}

	// The OLD token must still work right after rotation (grace window).
	req2 := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req2.Header.Set("Authorization", "Bearer admin-token")
	rr2 := httptest.NewRecorder()
	s.fedAuthMiddleware(http.HandlerFunc(s.handleHealthz)).ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("old token should still work inside the grace window, got %d", rr2.Code)
	}
}

func TestHandleRotateToken_OldTokenRevokedAfterGraceWindow(t *testing.T) {
	s := newRotationTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/rotate-token", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	rr := httptest.NewRecorder()
	s.fedAuthMiddleware(http.HandlerFunc(s.handleRotateToken)).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("rotate: want 200, got %d", rr.Code)
	}

	// Force the grace window to have already expired.
	s.tokenMu.Lock()
	s.oldTokenExpiry = time.Now().Add(-time.Second)
	s.tokenMu.Unlock()

	req2 := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req2.Header.Set("Authorization", "Bearer admin-token")
	rr2 := httptest.NewRecorder()
	s.fedAuthMiddleware(http.HandlerFunc(s.handleHealthz)).ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusUnauthorized {
		t.Fatalf("old token must be hard-revoked after the grace window, got %d", rr2.Code)
	}
}

func TestHandleRotateToken_OperatorSuppliedToken(t *testing.T) {
	s := newRotationTestServer(t)
	body, _ := json.Marshal(map[string]string{"new_token": "a-very-specific-operator-chosen-token"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/rotate-token", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer admin-token")
	rr := httptest.NewRecorder()
	s.fedAuthMiddleware(http.HandlerFunc(s.handleRotateToken)).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Token != "a-very-specific-operator-chosen-token" {
		t.Fatalf("got %q, want the operator-supplied token", resp.Token)
	}
}

func TestHandleRotateToken_TooShortOperatorTokenRejected(t *testing.T) {
	s := newRotationTestServer(t)
	body, _ := json.Marshal(map[string]string{"new_token": "short"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/rotate-token", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer admin-token")
	rr := httptest.NewRecorder()
	s.fedAuthMiddleware(http.HandlerFunc(s.handleRotateToken)).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for a too-short token, got %d", rr.Code)
	}
	// The live token must be unchanged.
	req2 := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req2.Header.Set("Authorization", "Bearer admin-token")
	rr2 := httptest.NewRecorder()
	s.fedAuthMiddleware(http.HandlerFunc(s.handleHealthz)).ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("original token should still work after a rejected rotation, got %d", rr2.Code)
	}
}

func TestHandleRotateToken_FederationPeerForbidden(t *testing.T) {
	s := newRotationTestServer(t)
	store, err := multiserver.NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	s.serverStore = store
	if err := s.serverStore.Add(&multiserver.Entry{
		Name: "peer1", URL: "https://peer1.example:8443", Token: "peer-token", Federated: true,
		Capabilities: []string{"full-control"},
	}); err != nil {
		t.Fatalf("add peer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/rotate-token", nil)
	req.Header.Set("Authorization", "Bearer peer-token")
	rr := httptest.NewRecorder()
	s.fedAuthMiddleware(http.HandlerFunc(s.handleRotateToken)).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("a federation peer must never rotate the admin token, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandlePutConfig_ServerTokenAndMCPTokenSkipped(t *testing.T) {
	s := newRotationTestServer(t)
	patch, _ := json.Marshal(map[string]string{
		"server.token": "sneaky-new-token",
		"mcp.token":    "sneaky-mcp-token",
		"server.host":  "0.0.0.0",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(patch))
	req.Header.Set("Authorization", "Bearer admin-token")
	rr := httptest.NewRecorder()
	s.fedAuthMiddleware(http.HandlerFunc(s.handlePutConfig)).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if s.cfg.Server.Token != "admin-token" {
		t.Errorf("server.token must not be changeable via PUT /api/config, got %q", s.cfg.Server.Token)
	}
	if s.cfg.MCP.Token != "" {
		t.Errorf("mcp.token must not be changeable via PUT /api/config, got %q", s.cfg.MCP.Token)
	}
	if s.cfg.Server.Host != "0.0.0.0" {
		t.Errorf("unrelated fields in the same patch must still apply, server.host=%q", s.cfg.Server.Host)
	}
	var resp struct {
		Skipped []string `json:"skipped"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Skipped) != 2 {
		t.Errorf("expected both skipped keys reported, got %v", resp.Skipped)
	}
}
