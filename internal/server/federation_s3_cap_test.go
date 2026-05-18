// BL316 S3 — capability enforcement tests for the systematic REST handler sweep.
//
// Each test verifies that a handler protected in S3 returns:
//   - 200 when the peer token has the required capability
//   - 403 when the peer token lacks it
//   - 200 when the admin token is used

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// fedCapFixture sets up a test server with two federation peers:
//   - "peer-full" has the full-control group (all 50 caps)
//   - "peer-readonly" has only read-only caps
func fedCapFixture(t *testing.T) (s *Server, fullToken, roToken string) {
	t.Helper()
	s, store, _ := newFedTestServer(t)

	if err := store.Add(&multiserver.Entry{
		Name:         "peer-full",
		Token:        "tok-full",
		URL:          "http://peer-full:8080",
		Enabled:      true,
		Federated:    true,
		Capabilities: []string{"full-control"},
	}); err != nil {
		t.Fatalf("add peer-full: %v", err)
	}
	if err := store.Add(&multiserver.Entry{
		Name:         "peer-ro",
		Token:        "tok-ro",
		URL:          "http://peer-ro:8080",
		Enabled:      true,
		Federated:    true,
		Capabilities: []string{"read-only"},
	}); err != nil {
		t.Fatalf("add peer-ro: %v", err)
	}
	return s, "tok-full", "tok-ro"
}

// capTest is a helper that asserts expected status codes for a given handler
// wrapped with fedAuthMiddleware.
func capTest(t *testing.T, s *Server, method, path string, body []byte,
	handler func(http.ResponseWriter, *http.Request),
	adminExpect, fullExpect, roExpect int,
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
	run("tok-full", fullExpect, "full-control peer")
	run("tok-ro", roExpect, "read-only peer")
}

// ─── pipelines ───────────────────────────────────────────────────────────────

func TestFedS3_PipelinesListCap(t *testing.T) {
	s, _, _ := fedCapFixture(t)
	// GET /api/pipelines → CapPipelinesList: admin 503 (nil exec), full 503, ro 503
	// Even with nil pipelineExec it returns 503 NOT 403 for admin/full, but 403 for
	// a token that lacks pipelines:list. ro group has pipelines:list via read-only.
	// So all should return 503 (service unavailable), not 403.
	capTest(t, s,
		http.MethodGet, "/api/pipelines", nil,
		s.handlePipelines,
		http.StatusServiceUnavailable, // admin: no exec wired
		http.StatusServiceUnavailable, // full: has cap, but no exec
		http.StatusServiceUnavailable, // ro: has pipelines:list, no exec
	)
}

func TestFedS3_PipelinesStartNoCap(t *testing.T) {
	s, _, _ := fedCapFixture(t)
	// POST /api/pipelines → CapPipelinesStart: read-only peer lacks this → 403
	// full-control has it but exec is nil → 503
	body := map[string]any{"spec": "t1 -> t2"}
	rb, _ := json.Marshal(body)
	capTest(t, s,
		http.MethodPost, "/api/pipelines", rb,
		s.handlePipelines,
		http.StatusServiceUnavailable, // admin: no exec
		http.StatusServiceUnavailable, // full: has cap, no exec
		http.StatusForbidden,          // ro: lacks pipelines:start
	)
}

// ─── config ──────────────────────────────────────────────────────────────────

func TestFedS3_ConfigReadCap(t *testing.T) {
	s, _, _ := fedCapFixture(t)
	// GET /api/config → CapConfigRead: read-only has it (config:read), full has it.
	// admin → 200 (cfg nil → but handleGetConfig checks cfg)
	// Since cfg is nil in test server, admin returns 503 actually. Let's check
	// whether fedCap comes BEFORE the nil cfg check.
	// The order we added: method check → fedCap → s.handleGetConfig.
	// handleGetConfig has `if s.cfg == nil → 503`.
	// So admin and full should get 503 (cfg nil), ro has config:read → also 503.
	capTest(t, s,
		http.MethodGet, "/api/config", nil,
		s.handleConfig,
		http.StatusServiceUnavailable, // admin: cfg nil
		http.StatusServiceUnavailable, // full: has cap, cfg nil
		http.StatusServiceUnavailable, // ro: has config:read, cfg nil
	)
}

func TestFedS3_ConfigWriteNoCap(t *testing.T) {
	s, _, _ := fedCapFixture(t)
	// PUT /api/config → CapConfigWrite: ro lacks config:write → 403
	body := []byte(`{"session":{"idle_timeout_minutes":30}}`)
	capTest(t, s,
		http.MethodPut, "/api/config", body,
		s.handleConfig,
		http.StatusServiceUnavailable, // admin: cfg nil → 503 (fedCap passes, then nil check)
		http.StatusServiceUnavailable, // full: has cap, cfg nil
		http.StatusForbidden,          // ro: lacks config:write → 403
	)
}

// ─── alerts ──────────────────────────────────────────────────────────────────

func TestFedS3_AlertsListCap(t *testing.T) {
	s, _, _ := fedCapFixture(t)
	// GET /api/alerts → CapAlertsList: alertStore nil → 503 for those with cap.
	// ro group has alerts:list → 503 (no store). peer without cap would get 403.
	// Here both full and ro have alerts:list via their groups.
	capTest(t, s,
		http.MethodGet, "/api/alerts", nil,
		s.handleAlerts,
		http.StatusServiceUnavailable, // admin: store nil
		http.StatusServiceUnavailable, // full: has cap, store nil
		http.StatusServiceUnavailable, // ro: has alerts:list, store nil
	)
}

// ─── federation sessions fan-out (TS-574) ────────────────────────────────────

func TestFedS3_FederationSessionsCap(t *testing.T) {
	s, _, _ := fedCapFixture(t)
	// GET /api/federation/sessions → CapFederationRead: federation-peer group has
	// federation:read. ro group has it too. full has it.
	// No network calls needed since no cfg.Servers or federated peers registered.
	capTest(t, s,
		http.MethodGet, "/api/federation/sessions", nil,
		s.handleFederationSessions,
		http.StatusOK, // admin: always ok
		http.StatusOK, // full: federation:read granted
		http.StatusOK, // ro: federation:read granted via read-only group
	)
}

// ─── analytics ───────────────────────────────────────────────────────────────

func TestFedS3_AnalyticsCap(t *testing.T) {
	s, _, _ := fedCapFixture(t)
	// GET /api/analytics → CapAnalyticsRead: both full and ro have it.
	// handler is in analytics.go — let's test directly.
	// Actually we can't easily test handleAnalytics without setting up the full
	// session manager; skip if it causes issues.
	// Both full and ro have analytics:read. We just need to confirm no 403.
	capTest(t, s,
		http.MethodGet, "/api/analytics", nil,
		s.handleAnalytics,
		http.StatusOK,
		http.StatusOK,
		http.StatusOK,
	)
}

// ─── cross-surface: peer with inference-admin can hit llm list ───────────────

func TestFedS3_LLMListRequiresLLMsCap(t *testing.T) {
	s, store, _ := newFedTestServer(t)
	// Peer with only session-viewer: no llms:list → 403 on GET /api/llms
	if err := store.Add(&multiserver.Entry{
		Name:         "peer-sv",
		Token:        "tok-sv",
		URL:          "http://peer-sv:8080",
		Enabled:      true,
		Federated:    true,
		Capabilities: []string{"session-viewer"},
	}); err != nil {
		t.Fatalf("add peer: %v", err)
	}

	wrapped := s.fedAuthMiddleware(http.HandlerFunc(s.handleLLMs))
	req := httptest.NewRequest(http.MethodGet, "/api/llms", nil)
	req.Header.Set("Authorization", "Bearer tok-sv")
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("session-viewer peer should get 403 on /api/llms, got %d", rr.Code)
	}
}

func TestFedS3_InferenceAdminCanListLLMs(t *testing.T) {
	s, store, _ := newFedTestServer(t)
	// Peer with inference-admin has llms:list.
	if err := store.Add(&multiserver.Entry{
		Name:         "peer-ia",
		Token:        "tok-ia",
		URL:          "http://peer-ia:8080",
		Enabled:      true,
		Federated:    true,
		Capabilities: []string{"inference-admin"},
	}); err != nil {
		t.Fatalf("add peer: %v", err)
	}

	wrapped := s.fedAuthMiddleware(http.HandlerFunc(s.handleLLMs))
	req := httptest.NewRequest(http.MethodGet, "/api/llms", nil)
	req.Header.Set("Authorization", "Bearer tok-ia")
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)
	// handleLLMs with nil llmRegistry → 503 (service unavail), but NOT 403.
	if rr.Code == http.StatusForbidden {
		t.Errorf("inference-admin peer should not get 403 on /api/llms, got %d", rr.Code)
	}
}

// ─── cost summary requires analytics:read ────────────────────────────────────

func TestFedS3_CostSummaryCap(t *testing.T) {
	s, store, _ := newFedTestServer(t)
	// Peer with session-viewer only: no analytics:read → 403 on cost.
	if err := store.Add(&multiserver.Entry{
		Name:         "peer-sv2",
		Token:        "tok-sv2",
		URL:          "http://peer-sv2:8080",
		Enabled:      true,
		Federated:    true,
		Capabilities: []string{"session-viewer"},
	}); err != nil {
		t.Fatalf("add peer: %v", err)
	}

	wrapped := s.fedAuthMiddleware(http.HandlerFunc(s.handleCostSummary))
	req := httptest.NewRequest(http.MethodGet, "/api/cost", nil)
	req.Header.Set("Authorization", "Bearer tok-sv2")
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("session-viewer peer should get 403 on /api/cost, got %d", rr.Code)
	}
}

func TestFedS3_FederationPeerCannotWritePipelines(t *testing.T) {
	s, store, _ := newFedTestServer(t)
	// federation-peer group has no pipelines:start.
	if err := store.Add(&multiserver.Entry{
		Name:         "peer-fp",
		Token:        "tok-fp",
		URL:          "http://peer-fp:8080",
		Enabled:      true,
		Federated:    true,
		Capabilities: []string{"federation-peer"},
	}); err != nil {
		t.Fatalf("add peer: %v", err)
	}

	body := map[string]any{"spec": "t1 -> t2"}
	rb, _ := json.Marshal(body)
	wrapped := s.fedAuthMiddleware(http.HandlerFunc(s.handlePipelines))
	req := httptest.NewRequest(http.MethodPost, "/api/pipelines", bytes.NewReader(rb))
	req.Header.Set("Authorization", "Bearer tok-fp")
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("federation-peer should get 403 on POST /api/pipelines, got %d", rr.Code)
	}
}
