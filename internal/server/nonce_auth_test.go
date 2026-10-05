// SEC-006 — nonce-aware fedAuthMiddleware + handleAuthNonce end-to-end tests.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newNonceTestServer(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t, nil, nil)
	s.token = "admin-token"
	s.nonces = newNonceStore()
	return s
}

func TestHandleAuthNonce_MintsForAdminCaller(t *testing.T) {
	s := newNonceTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/nonce", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	rr := httptest.NewRecorder()
	s.fedAuthMiddleware(http.HandlerFunc(s.handleAuthNonce)).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Nonce     string `json:"nonce"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Nonce == "" {
		t.Error("expected a non-empty nonce")
	}
}

func TestHandleAuthNonce_RejectsGET(t *testing.T) {
	s := newNonceTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/nonce", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	rr := httptest.NewRecorder()
	s.fedAuthMiddleware(http.HandlerFunc(s.handleAuthNonce)).ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", rr.Code)
	}
}

func TestHandleAuthNonce_EmptyServerToken_NoCallerIdentity(t *testing.T) {
	s := newNonceTestServer(t)
	s.token = "" // SEC-001 posture — everyone passes fedAuthMiddleware with no identity
	req := httptest.NewRequest(http.MethodPost, "/api/auth/nonce", nil)
	rr := httptest.NewRecorder()
	s.fedAuthMiddleware(http.HandlerFunc(s.handleAuthNonce)).ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400 (nothing to mint a nonce for), got %d", rr.Code)
	}
}

// TestNonceAwarePath_EndToEnd is a regression test for SEC-006: mint a
// nonce via the real handler, then use it on a nonce-aware route exactly
// as a browser <a href>/EventSource would — no Authorization header, just
// ?nonce=. Confirms it authenticates successfully, exactly once.
func TestNonceAwarePath_EndToEnd(t *testing.T) {
	s := newNonceTestServer(t)

	// Mint, as an authenticated fetch() call would.
	mintReq := httptest.NewRequest(http.MethodPost, "/api/auth/nonce", nil)
	mintReq.Header.Set("Authorization", "Bearer admin-token")
	mintRR := httptest.NewRecorder()
	s.fedAuthMiddleware(http.HandlerFunc(s.handleAuthNonce)).ServeHTTP(mintRR, mintReq)
	var minted struct{ Nonce string `json:"nonce"` }
	if err := json.NewDecoder(mintRR.Body).Decode(&minted); err != nil {
		t.Fatalf("decode mint response: %v", err)
	}

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	handler := s.fedAuthMiddleware(inner)

	// Use it on a nonce-aware path, no Authorization header at all.
	useReq := httptest.NewRequest(http.MethodGet, "/api/files/download?path=/tmp/x&nonce="+minted.Nonce, nil)
	useRR := httptest.NewRecorder()
	handler.ServeHTTP(useRR, useReq)
	if !called || useRR.Code != http.StatusOK {
		t.Fatalf("nonce should authenticate a nonce-aware GET with no header: status=%d called=%v", useRR.Code, called)
	}

	// Single-use: the same nonce must not work a second time.
	called = false
	useReq2 := httptest.NewRequest(http.MethodGet, "/api/files/download?path=/tmp/x&nonce="+minted.Nonce, nil)
	useRR2 := httptest.NewRecorder()
	handler.ServeHTTP(useRR2, useReq2)
	if called || useRR2.Code != http.StatusUnauthorized {
		t.Fatalf("a consumed nonce must not work twice: status=%d called=%v", useRR2.Code, called)
	}
}

// TestNonceAwarePath_RejectedOnUnrelatedRoute confirms a nonce only works
// on the specific nonce-aware routes, not as a general-purpose bearer
// token replacement for every endpoint.
func TestNonceAwarePath_RejectedOnUnrelatedRoute(t *testing.T) {
	s := newNonceTestServer(t)
	nonce, _ := s.nonces.Mint("admin-token")

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	handler := s.fedAuthMiddleware(inner)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions?nonce="+nonce, nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if called || rr.Code != http.StatusUnauthorized {
		t.Fatalf("a nonce must not authenticate an unrelated route: status=%d called=%v", rr.Code, called)
	}
}

// TestQueryParamToken_Rejected is a regression test for SEC-006: the old
// ?token= fallback (the real admin/peer token in the URL) must be gone
// from REST entirely — only the nonce (on nonce-aware routes) or the
// header works now.
func TestFedAuthMiddleware_QueryParamToken_Rejected(t *testing.T) {
	s := newNonceTestServer(t)
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	handler := s.fedAuthMiddleware(inner)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions?token=admin-token", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if called || rr.Code != http.StatusUnauthorized {
		t.Fatalf("?token= must be rejected (SEC-006), got status=%d called=%v", rr.Code, called)
	}

	// The header still works.
	called = false
	req2 := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req2.Header.Set("Authorization", "Bearer admin-token")
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	if !called {
		t.Fatal("the Authorization header must still work")
	}
}
