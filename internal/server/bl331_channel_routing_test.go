// BL331 — unit tests for channel-address federation via comms.
//
// Tests cover:
//  1. GET /api/channel/routing with no config → empty rules
//  2. PUT then GET /api/channel/routing round-trips a rule correctly
//  3. Federation peer add with channel_identity persists the field
//  4. Session.OwnerPeer field survives JSON marshal/unmarshal

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/server/multiserver"
	"github.com/dmz006/datawatch/internal/session"
)

// testServerWithDir returns a minimal Server whose channel routing config
// lives in a temp dir, and a cleanup function.
func testServerWithDir(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	// Override UserHomeDir resolution by pointing the routing path at dir.
	// We do this by writing the file ourselves and using handleChannelRouting
	// indirectly via the path helper — no patch needed since channelRoutingPath
	// uses os.UserHomeDir. We mount a sub-dir here and override below.
	s := &Server{token: "admin-token"}
	// Monkey-patch: set the home via env so os.UserHomeDir returns dir.
	t.Setenv("HOME", dir)
	return s, dir
}

// TestChannelRouting_EmptyConfig: GET with no backing file returns empty rules.
func TestChannelRouting_EmptyConfig(t *testing.T) {
	s, _ := testServerWithDir(t)

	req := httptest.NewRequest(http.MethodGet, "/api/channel/routing", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	rr := httptest.NewRecorder()
	s.handleChannelRouting(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var cfg channelRoutingConfig
	if err := json.NewDecoder(rr.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cfg.Rules == nil || len(cfg.Rules) != 0 {
		t.Errorf("expected empty rules slice, got %v", cfg.Rules)
	}
}

// TestChannelRouting_PutAndGet: PUT a rule then GET returns it.
func TestChannelRouting_PutAndGet(t *testing.T) {
	s, _ := testServerWithDir(t)

	rule := channelRoutingConfig{
		Rules: []channelRoutingRule{
			{
				ChannelPattern:   "alerts-*",
				PeerName:         "peer-alpha",
				AutomataType:     "operational",
				DefaultProjectDir: "/home/dmz/ops",
			},
		},
	}
	body, _ := json.Marshal(rule)

	// PUT
	req := httptest.NewRequest(http.MethodPut, "/api/channel/routing", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer admin-token")
	rr := httptest.NewRecorder()
	s.handleChannelRouting(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	// GET
	req = httptest.NewRequest(http.MethodGet, "/api/channel/routing", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	rr = httptest.NewRecorder()
	s.handleChannelRouting(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got channelRoutingConfig
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if len(got.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(got.Rules))
	}
	r := got.Rules[0]
	if r.ChannelPattern != "alerts-*" {
		t.Errorf("channel_pattern: got %q", r.ChannelPattern)
	}
	if r.PeerName != "peer-alpha" {
		t.Errorf("peer_name: got %q", r.PeerName)
	}
	if r.AutomataType != "operational" {
		t.Errorf("automata_type: got %q", r.AutomataType)
	}
}

// TestFedPeer_ChannelIdentity: add peer with channel_identity, verify round-trip.
func TestFedPeer_ChannelIdentity(t *testing.T) {
	s, _, _ := newFedTestServer(t)

	body := map[string]any{
		"name":             "peer-chan",
		"url":              "http://peer-chan:8080",
		"token":            "chan-token",
		"enabled":          true,
		"channel_identity": []string{"alerts-prod", "ops-channel"},
	}
	rb, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/federation/peers", bytes.NewReader(rb))
	req.Header.Set("Authorization", "Bearer admin-token")
	rr := httptest.NewRecorder()
	s.handleFederationPeers(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("add peer: expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}

	var created multiserver.Entry
	if err := json.NewDecoder(rr.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(created.ChannelIdentity) != 2 {
		t.Fatalf("channel_identity: expected 2, got %v", created.ChannelIdentity)
	}
	if created.ChannelIdentity[0] != "alerts-prod" || created.ChannelIdentity[1] != "ops-channel" {
		t.Errorf("channel_identity values: %v", created.ChannelIdentity)
	}

	// GET the peer back and verify.
	getReq := httptest.NewRequest(http.MethodGet, "/api/federation/peers/peer-chan", nil)
	getReq.URL.Path = "/api/federation/peers/peer-chan"
	getReq.Header.Set("Authorization", "Bearer admin-token")
	rrGet := httptest.NewRecorder()
	s.handleFederationPeers(rrGet, getReq)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("get peer: expected 200, got %d", rrGet.Code)
	}
	var fetched multiserver.Entry
	if err := json.NewDecoder(rrGet.Body).Decode(&fetched); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if len(fetched.ChannelIdentity) != 2 {
		t.Errorf("GET channel_identity: expected 2, got %v", fetched.ChannelIdentity)
	}
}

// TestSession_OwnerPeer: Session.OwnerPeer survives JSON round-trip on disk.
func TestSession_OwnerPeer(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("test-host", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("manager init: %v", err)
	}
	_ = mgr // ensure manager wires up the store

	sess := &session.Session{
		ID:         "aabb",
		FullID:     "test-host-aabb",
		Task:       "do something",
		ProjectDir: "/tmp",
		OwnerPeer:  "peer-origin",
	}

	// Write to disk and read back to verify JSON field persists.
	path := filepath.Join(dir, "owner_peer_test.json")
	data, err := json.Marshal(sess)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var loaded session.Session
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if loaded.OwnerPeer != "peer-origin" {
		t.Errorf("OwnerPeer: got %q, want %q", loaded.OwnerPeer, "peer-origin")
	}
}
