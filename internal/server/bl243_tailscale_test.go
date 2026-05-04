// BL243 Phase 1 — REST handler tests for Tailscale k8s sidecar.

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/tailscale"
)

// mockTailscaleClient implements tailscaleClient for tests.
type mockTailscaleClient struct {
	status    *tailscale.StatusResponse
	nodes     []tailscale.NodeInfo
	pushErr   error
	authKey   *tailscale.PreAuthKeyResult
	authKeyErr error
}

func (m *mockTailscaleClient) Status(_ context.Context) (*tailscale.StatusResponse, error) {
	return m.status, nil
}
func (m *mockTailscaleClient) Nodes(_ context.Context) ([]tailscale.NodeInfo, error) {
	return m.nodes, nil
}
func (m *mockTailscaleClient) PushACL(_ context.Context, _ string) error {
	return m.pushErr
}
func (m *mockTailscaleClient) GeneratePreAuthKey(_ context.Context, _ tailscale.PreAuthKeyOptions) (*tailscale.PreAuthKeyResult, error) {
	return m.authKey, m.authKeyErr
}

func newTailscaleTestServer(client tailscaleClient) *Server {
	s := &Server{}
	s.tailscaleClient = client
	return s
}

func TestTailscaleStatus_NoClient(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest(http.MethodGet, "/api/tailscale/status", nil)
	w := httptest.NewRecorder()
	s.handleTailscaleStatus(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestTailscaleStatus_OK(t *testing.T) {
	mock := &mockTailscaleClient{
		status: &tailscale.StatusResponse{
			Enabled:   true,
			Backend:   "headscale",
			NodeCount: 2,
			Nodes: []tailscale.NodeInfo{
				{ID: "1", Name: "agent-01", IP: "100.64.0.1", Online: true, Tags: []string{"tag:dw-agent"}},
				{ID: "2", Name: "agent-02", IP: "100.64.0.2", Online: false},
			},
		},
	}
	s := newTailscaleTestServer(mock)
	r := httptest.NewRequest(http.MethodGet, "/api/tailscale/status", nil)
	w := httptest.NewRecorder()
	s.handleTailscaleStatus(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp tailscale.StatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Backend != "headscale" {
		t.Errorf("expected backend=headscale, got %q", resp.Backend)
	}
	if len(resp.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(resp.Nodes))
	}
}

func TestTailscaleStatus_MethodNotAllowed(t *testing.T) {
	s := newTailscaleTestServer(&mockTailscaleClient{status: &tailscale.StatusResponse{}})
	r := httptest.NewRequest(http.MethodPost, "/api/tailscale/status", nil)
	w := httptest.NewRecorder()
	s.handleTailscaleStatus(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestTailscaleNodes_OK(t *testing.T) {
	mock := &mockTailscaleClient{
		nodes: []tailscale.NodeInfo{
			{ID: "n1", Name: "node-a", IP: "100.64.0.10", Online: true},
		},
	}
	s := newTailscaleTestServer(mock)
	r := httptest.NewRequest(http.MethodGet, "/api/tailscale/nodes", nil)
	w := httptest.NewRecorder()
	s.handleTailscaleNodes(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string][]tailscale.NodeInfo
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp["nodes"]) != 1 {
		t.Errorf("expected 1 node, got %d", len(resp["nodes"]))
	}
}

func TestTailscaleACLPush_RawPolicy(t *testing.T) {
	mock := &mockTailscaleClient{}
	s := newTailscaleTestServer(mock)
	policy := `{"acls": [{"action": "accept", "src": ["tag:dw-agent"], "dst": ["*:*"]}]}`
	r := httptest.NewRequest(http.MethodPost, "/api/tailscale/acl/push", strings.NewReader(policy))
	w := httptest.NewRecorder()
	s.handleTailscaleACLPush(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTailscaleACLPush_JSONWrapper(t *testing.T) {
	mock := &mockTailscaleClient{}
	s := newTailscaleTestServer(mock)
	body := `{"policy": "{\"acls\": []}"}`
	r := httptest.NewRequest(http.MethodPost, "/api/tailscale/acl/push", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.handleTailscaleACLPush(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTailscaleACLPush_EmptyBody(t *testing.T) {
	mock := &mockTailscaleClient{}
	s := newTailscaleTestServer(mock)
	r := httptest.NewRequest(http.MethodPost, "/api/tailscale/acl/push", strings.NewReader(""))
	w := httptest.NewRecorder()
	s.handleTailscaleACLPush(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestTailscaleACLPush_NoClient(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest(http.MethodPost, "/api/tailscale/acl/push", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	s.handleTailscaleACLPush(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestTailscaleAuthKey_OK(t *testing.T) {
	mock := &mockTailscaleClient{
		authKey: &tailscale.PreAuthKeyResult{
			Key:       "abc123",
			Reusable:  false,
			Ephemeral: false,
		},
	}
	s := newTailscaleTestServer(mock)
	body := `{"reusable":false,"ephemeral":false,"expiry_hours":24}`
	r := httptest.NewRequest(http.MethodPost, "/api/tailscale/auth/key", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.handleTailscaleAuthKey(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp tailscale.PreAuthKeyResult
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Key != "abc123" {
		t.Errorf("expected key=abc123, got %q", resp.Key)
	}
}

func TestTailscaleAuthKey_NoClient(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest(http.MethodPost, "/api/tailscale/auth/key", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	s.handleTailscaleAuthKey(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}
