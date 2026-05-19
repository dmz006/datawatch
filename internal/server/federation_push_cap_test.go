// BL330 federation capability tests for push endpoints.
//
// Tests verify that:
//   - POST /api/push/<topic> (publish) requires comm:write
//   - GET  /api/push/register (list)   requires comm:read
//   - POST /api/push/register          requires comm:write
//   - DELETE /api/push/unregister      requires comm:write
//   - POST /api/push/notify            requires comm:write
//
// The read-only peer has comm:read but not comm:write, so write endpoints must
// return 403 for that peer.
//
// A monitor-only peer (health:read only) lacks even comm:read, so the register
// list endpoint must also return 403.

package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// fedPushFixture creates a test server with three peers:
//   - "peer-full"    full-control (all caps)
//   - "peer-ro"      read-only group (comm:read but not comm:write)
//   - "peer-monitor" monitor group (no comm caps at all)
func fedPushFixture(t *testing.T) (s *Server, fullToken, roToken, monToken string) {
	t.Helper()
	s, store, _ := newFedTestServer(t)

	peers := []struct {
		name  string
		token string
		cap   string
	}{
		{"push-full", "ptok-full", "full-control"},
		{"push-ro", "ptok-ro", "read-only"},
		{"push-mon", "ptok-mon", "monitor"},
	}
	for _, p := range peers {
		if err := store.Add(&multiserver.Entry{
			Name:         p.name,
			Token:        p.token,
			URL:          "http://" + p.name + ":8080",
			Enabled:      true,
			Federated:    true,
			Capabilities: []string{p.cap},
		}); err != nil {
			t.Fatalf("add %s: %v", p.name, err)
		}
	}
	return s, "ptok-full", "ptok-ro", "ptok-mon"
}

func pushCapTest(t *testing.T, s *Server, method, path string, body []byte,
	handler func(http.ResponseWriter, *http.Request),
	adminExpect, fullExpect, roExpect, monExpect int,
) {
	t.Helper()
	wrapped := s.fedAuthMiddleware(http.HandlerFunc(handler))

	run := func(token string, expect int, label string) {
		t.Helper()
		var req *http.Request
		if body != nil {
			req = httptest.NewRequest(method, path, bytes.NewReader(body))
		} else {
			req = httptest.NewRequest(method, path, nil)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)
		if rr.Code != expect {
			t.Errorf("%s: expected %d, got %d (body=%s)", label, expect, rr.Code, rr.Body.String())
		}
	}

	run("admin-token", adminExpect, "admin")
	run("ptok-full", fullExpect, "full-control")
	run("ptok-ro", roExpect, "read-only")
	run("ptok-mon", monExpect, "monitor-only")
}

// TestFedPush_TopicPublishRequiresCommWrite — POST /api/push/<topic>
// ro lacks comm:write → 403; monitor lacks comm:write → 403
func TestFedPush_TopicPublishRequiresCommWrite(t *testing.T) {
	s, _, _, _ := fedPushFixture(t)
	body := mustMarshal(map[string]any{"title": "t", "message": "m"})
	pushCapTest(t, s,
		http.MethodPost, "/api/push/alerts", body,
		s.handlePushTopic,
		http.StatusOK,        // admin: passes, publish succeeds
		http.StatusOK,        // full: has comm:write
		http.StatusForbidden, // ro: lacks comm:write
		http.StatusForbidden, // monitor: lacks comm:write
	)
}

// TestFedPush_RegisterListRequiresCommRead — GET /api/push/register
// ro has comm:read → 200; monitor lacks comm:read → 403
func TestFedPush_RegisterListRequiresCommRead(t *testing.T) {
	s, _, _, _ := fedPushFixture(t)
	pushCapTest(t, s,
		http.MethodGet, "/api/push/register", nil,
		s.handlePushRegister,
		http.StatusOK, // admin: 200
		http.StatusOK, // full: 200
		http.StatusOK, // ro: has comm:read → 200
		http.StatusForbidden, // monitor: lacks comm:read → 403
	)
}

// TestFedPush_RegisterPostRequiresCommWrite — POST /api/push/register
// ro lacks comm:write → 403
func TestFedPush_RegisterPostRequiresCommWrite(t *testing.T) {
	s, _, _, _ := fedPushFixture(t)
	body := mustMarshal(map[string]any{"endpoint": "https://up.example.com/UP"})
	pushCapTest(t, s,
		http.MethodPost, "/api/push/register", body,
		s.handlePushRegister,
		http.StatusOK,        // admin: endpoint registered
		http.StatusOK,        // full: has comm:write
		http.StatusForbidden, // ro: lacks comm:write
		http.StatusForbidden, // monitor: lacks comm:write
	)
}

// TestFedPush_UnregisterRequiresCommWrite — DELETE /api/push/unregister
// ro lacks comm:write → 403
func TestFedPush_UnregisterRequiresCommWrite(t *testing.T) {
	s, _, _, _ := fedPushFixture(t)
	body := mustMarshal(map[string]any{"endpoint": "https://nonexistent.example.com/UP"})
	pushCapTest(t, s,
		http.MethodDelete, "/api/push/unregister", body,
		s.handlePushUnregister,
		http.StatusOK,        // admin: ok (removed 0)
		http.StatusOK,        // full: ok
		http.StatusForbidden, // ro: lacks comm:write
		http.StatusForbidden, // monitor: lacks comm:write
	)
}

// TestFedPush_NotifyRequiresCommWrite — POST /api/push/notify
// ro lacks comm:write → 403
func TestFedPush_NotifyRequiresCommWrite(t *testing.T) {
	s, _, _, _ := fedPushFixture(t)
	body := mustMarshal(map[string]any{"title": "t", "message": "m"})
	pushCapTest(t, s,
		http.MethodPost, "/api/push/notify", body,
		s.handlePushNotify,
		http.StatusAccepted,  // admin: 202
		http.StatusAccepted,  // full: 202
		http.StatusForbidden, // ro: lacks comm:write
		http.StatusForbidden, // monitor: lacks comm:write
	)
}

// TestFedDecompose_AsyncRequiresAutonomousWrite — POST .../decompose
// Tests that the decompose 202 endpoint enforces CapAutonomousWrite.
func TestFedDecompose_AsyncRequiresAutonomousWrite(t *testing.T) {
	s, _, _, _ := fedPushFixture(t)
	// read-only has autonomous:read but not autonomous:write
	// monitor lacks all autonomous caps
	wrapped := s.fedAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.handleDecomposeAsync(w, r, "prd-test-id")
	}))

	run := func(token string, expect int, label string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/prd-test-id/decompose", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)
		if rr.Code != expect {
			t.Errorf("%s: expected %d, got %d (body=%s)", label, expect, rr.Code, rr.Body.String())
		}
	}

	run("admin-token", http.StatusServiceUnavailable, "admin") // autonomousMgr nil → 503
	run("ptok-full", http.StatusServiceUnavailable, "full")   // has cap, mgr nil → 503
	run("ptok-ro", http.StatusForbidden, "read-only")         // lacks autonomous:write → 403
	run("ptok-mon", http.StatusForbidden, "monitor")          // lacks autonomous:write → 403
}

// TestFedDecompose_StatusRequiresAutonomousRead — GET .../decompose/status
func TestFedDecompose_StatusRequiresAutonomousRead(t *testing.T) {
	s, _, _, _ := fedPushFixture(t)
	wrapped := s.fedAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.handleDecomposeStatus(w, r, "prd-noexist")
	}))

	run := func(token string, expect int, label string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/prd-noexist/decompose/status", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)
		if rr.Code != expect {
			t.Errorf("%s: expected %d, got %d (body=%s)", label, expect, rr.Code, rr.Body.String())
		}
	}

	run("admin-token", http.StatusServiceUnavailable, "admin") // autonomousMgr nil → 503
	run("ptok-full", http.StatusServiceUnavailable, "full")    // has cap, mgr nil → 503
	run("ptok-ro", http.StatusServiceUnavailable, "read-only") // has autonomous:read → 503 (mgr nil)
	run("ptok-mon", http.StatusForbidden, "monitor")           // lacks autonomous:read → 403
}

