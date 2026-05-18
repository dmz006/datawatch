// BL316 — integration tests for /api/federation/peers and /api/federation/groups.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/server/multiserver"
	"github.com/dmz006/datawatch/internal/session"
)

func newFedTestServer(t *testing.T) (*Server, *multiserver.Store, *federation.GroupStore) {
	t.Helper()
	dir := t.TempDir()
	store, err := multiserver.NewStore(dir, nil)
	if err != nil {
		t.Fatalf("store init: %v", err)
	}
	gs, err := federation.NewGroupStore(dir)
	if err != nil {
		t.Fatalf("group store init: %v", err)
	}
	mgr, err := session.NewManager("test-host", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("manager init: %v", err)
	}
	s := &Server{
		manager:       mgr,
		hostname:      "test-host",
		token:         "admin-token",
		serverStore:   store,
		fedGroupStore: gs,
	}
	return s, store, gs
}

// ─── peer list/add/get/delete ────────────────────────────────────────────────

func TestFedPeer_AddListGetDelete(t *testing.T) {
	s, _, _ := newFedTestServer(t)
	adminReq := func(method, path string, body []byte) *http.Request {
		var r *http.Request
		if body != nil {
			r = httptest.NewRequest(method, path, bytes.NewReader(body))
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		r.Header.Set("Authorization", "Bearer admin-token")
		return r
	}

	// Add a peer.
	body := map[string]any{
		"name":         "peer-alpha",
		"url":          "http://peer-alpha:8080",
		"token":        "peer-alpha-token",
		"enabled":      true,
		"capabilities": []string{"federation-peer"},
	}
	rb, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	s.handleFederationPeers(rr, adminReq(http.MethodPost, "/api/federation/peers", rb))
	if rr.Code != http.StatusCreated {
		t.Fatalf("add peer: expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}

	var created multiserver.Entry
	if err := json.NewDecoder(rr.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if !created.Federated {
		t.Error("created entry should have Federated=true")
	}
	if created.Name != "peer-alpha" {
		t.Errorf("expected name peer-alpha, got %s", created.Name)
	}
	if created.AuthType != "token" {
		t.Errorf("expected default auth_type=token, got %s", created.AuthType)
	}

	// List peers.
	rr = httptest.NewRecorder()
	s.handleFederationPeers(rr, adminReq(http.MethodGet, "/api/federation/peers", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rr.Code)
	}
	var list []*multiserver.Entry
	if err := json.NewDecoder(rr.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 || list[0].Name != "peer-alpha" {
		t.Errorf("expected 1 peer named peer-alpha, got %v", list)
	}

	// Get by name.
	rr = httptest.NewRecorder()
	req := adminReq(http.MethodGet, "/api/federation/peers/peer-alpha", nil)
	req.URL.Path = "/api/federation/peers/peer-alpha"
	s.handleFederationPeers(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", rr.Code)
	}

	// Delete.
	rr = httptest.NewRecorder()
	req = adminReq(http.MethodDelete, "/api/federation/peers/peer-alpha", nil)
	req.URL.Path = "/api/federation/peers/peer-alpha"
	s.handleFederationPeers(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", rr.Code)
	}

	// List again — should be empty.
	rr = httptest.NewRecorder()
	s.handleFederationPeers(rr, adminReq(http.MethodGet, "/api/federation/peers", nil))
	var list2 []*multiserver.Entry
	json.NewDecoder(rr.Body).Decode(&list2) //nolint:errcheck
	if len(list2) != 0 {
		t.Errorf("expected 0 peers after delete, got %d", len(list2))
	}
}

func TestFedPeer_DefaultCapabilities(t *testing.T) {
	s, _, _ := newFedTestServer(t)

	body := map[string]any{"name": "peer-defaults", "url": "http://peer:8080", "token": "tok", "enabled": true}
	rb, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/federation/peers", bytes.NewReader(rb))
	req.Header.Set("Authorization", "Bearer admin-token")
	rr := httptest.NewRecorder()
	s.handleFederationPeers(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("add: expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	var created multiserver.Entry
	json.NewDecoder(rr.Body).Decode(&created) //nolint:errcheck
	if len(created.Capabilities) == 0 {
		t.Error("expected default capabilities to be set")
	}
	if created.Capabilities[0] != "federation-peer" {
		t.Errorf("expected default cap 'federation-peer', got %v", created.Capabilities)
	}
}

func TestFedPeer_Conflict(t *testing.T) {
	s, _, _ := newFedTestServer(t)
	add := func() int {
		body := map[string]any{"name": "dup", "url": "http://x:8080", "token": "t", "enabled": true}
		rb, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/federation/peers", bytes.NewReader(rb))
		req.Header.Set("Authorization", "Bearer admin-token")
		rr := httptest.NewRecorder()
		s.handleFederationPeers(rr, req)
		return rr.Code
	}
	if add() != http.StatusCreated {
		t.Fatal("first add should succeed")
	}
	if add() != http.StatusConflict {
		t.Fatal("second add should return 409 conflict")
	}
}

// ─── capability enforcement ──────────────────────────────────────────────────

func TestFedCap_PeerTokenAccepted(t *testing.T) {
	s, store, _ := newFedTestServer(t)

	if err := store.Add(&multiserver.Entry{
		Name:         "peer-b",
		URL:          "http://peer-b:8080",
		Token:        "peer-b-token",
		Enabled:      true,
		Federated:    true,
		Capabilities: []string{"federation-peer"},
	}); err != nil {
		t.Fatalf("add peer: %v", err)
	}

	// sessions:list is in federation-peer — should get 200.
	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer peer-b-token")

	handler := s.fedAuthMiddleware(http.HandlerFunc(s.handleSessions))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("peer with sessions:list cap should get 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestFedCap_PeerLacksWrite(t *testing.T) {
	s, store, _ := newFedTestServer(t)

	if err := store.Add(&multiserver.Entry{
		Name:         "peer-c",
		URL:          "http://peer-c:8080",
		Token:        "peer-c-token",
		Enabled:      true,
		Federated:    true,
		Capabilities: []string{"federation-peer"}, // no sessions:write
	}); err != nil {
		t.Fatalf("add peer: %v", err)
	}

	body := map[string]any{"task": "hello", "project_dir": "/tmp"}
	rb, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/start", bytes.NewReader(rb))
	req.Header.Set("Authorization", "Bearer peer-c-token")

	handler := s.fedAuthMiddleware(http.HandlerFunc(s.handleStartSession))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("peer without sessions:write should get 403, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestFedCap_UnknownTokenRejected(t *testing.T) {
	s, _, _ := newFedTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer unknown-token")

	handler := s.fedAuthMiddleware(http.HandlerFunc(s.handleSessions))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("unknown token should get 401, got %d", rr.Code)
	}
}

func TestFedCap_AdminTokenBypasses(t *testing.T) {
	s, _, _ := newFedTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer admin-token")

	handler := s.fedAuthMiddleware(http.HandlerFunc(s.handleSessions))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("admin token should get 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// ─── groups ─────────────────────────────────────────────────────────────────

func TestFedGroups_ListBuiltins(t *testing.T) {
	s, _, _ := newFedTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/federation/groups", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	rr := httptest.NewRecorder()
	s.handleFederationGroups(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list groups: expected 200, got %d", rr.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	builtins, ok := resp["builtins"].([]any)
	if !ok || len(builtins) == 0 {
		t.Error("expected non-empty builtins list")
	}
}

func TestFedGroups_CustomCRUD(t *testing.T) {
	s, _, _ := newFedTestServer(t)
	adminReq := func(method, path string, body []byte) *http.Request {
		var r *http.Request
		if body != nil {
			r = httptest.NewRequest(method, path, bytes.NewReader(body))
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		r.Header.Set("Authorization", "Bearer admin-token")
		return r
	}

	g := federation.CapabilityGroup{
		Name: "ops-team",
		Caps: []string{federation.CapSessionsList, federation.CapSessionsInput},
	}
	rb, _ := json.Marshal(g)
	rr := httptest.NewRecorder()
	s.handleFederationGroups(rr, adminReq(http.MethodPost, "/api/federation/groups", rb))
	if rr.Code != http.StatusCreated {
		t.Fatalf("add custom group: expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}

	// Get it back.
	req := adminReq(http.MethodGet, "/api/federation/groups/ops-team", nil)
	req.URL.Path = "/api/federation/groups/ops-team"
	rr = httptest.NewRecorder()
	s.handleFederationGroups(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get group: expected 200, got %d", rr.Code)
	}

	// Delete it.
	req = adminReq(http.MethodDelete, "/api/federation/groups/ops-team", nil)
	req.URL.Path = "/api/federation/groups/ops-team"
	rr = httptest.NewRecorder()
	s.handleFederationGroups(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete group: expected 204, got %d", rr.Code)
	}
}

func TestFedGroups_CannotDeleteBuiltin(t *testing.T) {
	s, _, _ := newFedTestServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/federation/groups/monitor", nil)
	req.URL.Path = "/api/federation/groups/monitor"
	req.Header.Set("Authorization", "Bearer admin-token")
	rr := httptest.NewRecorder()
	s.handleFederationGroups(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("deleting builtin group should return 403, got %d", rr.Code)
	}
}

// ─── persistence ─────────────────────────────────────────────────────────────

func TestFedPeer_Persistence(t *testing.T) {
	dir := t.TempDir()
	store1, err := multiserver.NewStore(dir, nil)
	if err != nil {
		t.Fatalf("store1: %v", err)
	}
	if err := store1.Add(&multiserver.Entry{
		Name:      "persist-peer",
		URL:       "http://x:8080",
		Token:     "tok",
		Enabled:   true,
		Federated: true,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	store2, err := multiserver.NewStore(dir, nil)
	if err != nil {
		t.Fatalf("store2: %v", err)
	}
	peers := store2.ListFederated()
	if len(peers) != 1 || peers[0].Name != "persist-peer" {
		t.Errorf("expected persist-peer after reload, got %v", peers)
	}
}

func TestFedGroupStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	gs1, err := federation.NewGroupStore(dir)
	if err != nil {
		t.Fatalf("gs1: %v", err)
	}
	if err := gs1.Add(&federation.CapabilityGroup{
		Name: "custom-a",
		Caps: []string{federation.CapHealthRead},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	gs2, err := federation.NewGroupStore(dir)
	if err != nil {
		t.Fatalf("gs2: %v", err)
	}
	g, err := gs2.Get("custom-a")
	if err != nil {
		t.Fatalf("get after reload: %v", err)
	}
	if len(g.Caps) != 1 || g.Caps[0] != federation.CapHealthRead {
		t.Errorf("expected [health:read], got %v", g.Caps)
	}
}
