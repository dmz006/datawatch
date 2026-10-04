// BL394 security review (docs/plans/2026-10-03-bl394-security-findings-
// review.md §6, iframe-embed follow-up) — the scoped proxy token system.

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

func TestProxyTokenStore_MintAndValidate(t *testing.T) {
	store := &proxyTokenStore{tokens: make(map[string]proxyTokenEntry)}
	tok := store.mint("peer-a")
	if tok == "" {
		t.Fatal("mint returned empty token")
	}
	if !store.valid(tok, "peer-a") {
		t.Error("token should be valid for the peer it was minted for")
	}
	if store.valid(tok, "peer-b") {
		t.Error("token minted for peer-a must not validate for peer-b")
	}
	if store.valid("garbage", "peer-a") {
		t.Error("an unknown token must never validate")
	}
	if store.valid("", "peer-a") {
		t.Error("an empty token must never validate")
	}
}

func TestProxyTokenStore_ExpiredRejectedAndSwept(t *testing.T) {
	store := &proxyTokenStore{tokens: make(map[string]proxyTokenEntry)}
	store.tokens["stale"] = proxyTokenEntry{peerName: "peer-a", expiresAt: time.Now().Add(-time.Minute)}
	if store.valid("stale", "peer-a") {
		t.Error("an expired token must not validate")
	}
	if _, ok := store.tokens["stale"]; ok {
		t.Error("valid() should sweep the expired entry it just rejected")
	}
}

func TestFedAuthMiddleware_ScopedTokenWorksForOwnPeerOnly(t *testing.T) {
	s := newTestServer(t, nil, nil)
	s.token = "admin-token"
	tok := proxyTokens.mint("peer-x")
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if scopedProxyPeerFromContext(r.Context()) != "peer-x" {
			t.Error("expected scoped proxy peer context to be set to peer-x")
		}
	})
	handler := s.fedAuthMiddleware(next)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/proxy/peer-x/api/health", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	handler.ServeHTTP(rr, req)
	if !called || rr.Code != http.StatusOK {
		t.Fatalf("scoped token should pass for its own peer's /api/proxy/ path: status=%d called=%v", rr.Code, called)
	}
}

func TestFedAuthMiddleware_ScopedTokenRejectedForOtherPeer(t *testing.T) {
	s := newTestServer(t, nil, nil)
	s.token = "admin-token"
	tok := proxyTokens.mint("peer-x")
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := s.fedAuthMiddleware(next)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/proxy/peer-y/api/health", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("a token scoped to peer-x must not work for peer-y: status=%d", rr.Code)
	}
}

func TestFedAuthMiddleware_ScopedTokenRejectedForUnrelatedRoute(t *testing.T) {
	s := newTestServer(t, nil, nil)
	s.token = "admin-token"
	tok := proxyTokens.mint("peer-x")
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := s.fedAuthMiddleware(next)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("a scoped proxy token must never work outside /api/proxy/ or /remote/: status=%d", rr.Code)
	}
}

func TestFedAuthMiddleware_ScopedTokenWorksForRemoteAssetPath(t *testing.T) {
	s := newTestServer(t, nil, nil)
	s.token = "admin-token"
	tok := proxyTokens.mint("peer-x")
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := s.fedAuthMiddleware(next)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/remote/peer-x/style.css?v=1", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("scoped token should authorize its own peer's /remote/ asset path: status=%d", rr.Code)
	}
}

func TestAppendProxyToken(t *testing.T) {
	cases := []struct{ url, token, want string }{
		{"/remote/peer/style.css", "tok", "/remote/peer/style.css?token=tok"},
		{"/remote/peer/style.css?v=1", "tok", "/remote/peer/style.css?v=1&token=tok"},
		{"/remote/peer/style.css", "", "/remote/peer/style.css"},
	}
	for _, c := range cases {
		if got := appendProxyToken(c.url, c.token); got != c.want {
			t.Errorf("appendProxyToken(%q, %q) = %q, want %q", c.url, c.token, got, c.want)
		}
	}
}

func TestRewritePWAContent_InjectsBootstrapAndTokensAssets(t *testing.T) {
	html := `<!DOCTYPE html><html><head><title>x</title></head><body><link href="/style.css?v=1"><script src="/app.js?v=1"></script></body></html>`
	out := string(rewritePWAContent([]byte(html), "peer-x", "scoped-tok-123"))

	if !containsAll(out, `localStorage.setItem('cs_token','scoped-tok-123')`) {
		t.Errorf("missing bootstrap token injection: %s", out)
	}
	if !containsAll(out, `href="/remote/peer-x/style.css?v=1&token=scoped-tok-123"`) {
		t.Errorf("rewritten asset href missing the scoped token: %s", out)
	}
	if !containsAll(out, `src="/remote/peer-x/app.js?v=1&token=scoped-tok-123"`) {
		t.Errorf("rewritten asset src missing the scoped token: %s", out)
	}
}

func TestRewritePWAContent_EmptyTokenIsNoOpInjection(t *testing.T) {
	html := `<html><head><title>x</title></head><body></body></html>`
	out := string(rewritePWAContent([]byte(html), "peer-x", ""))
	if containsAll(out, "localStorage.setItem") {
		t.Errorf("empty token must not inject a bootstrap script: %s", out)
	}
}

func TestHandleProxy_DropsIncomingAcceptEncodingBeforeForwarding(t *testing.T) {
	var gotAcceptEncoding string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAcceptEncoding = r.Header.Get("Accept-Encoding")
		w.Write([]byte("ok")) //nolint:errcheck
	}))
	defer upstream.Close()

	s := newTestServer(t, nil, nil)
	s.cfg = &config.Config{Servers: []config.RemoteServerConfig{
		{Name: "peer-a", URL: upstream.URL, Enabled: true},
	}}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/proxy/peer-a/some/path", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	s.handleProxy(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	// Go's own http.Transport adds "Accept-Encoding: gzip" itself (and
	// transparently decodes the response) whenever the caller didn't set
	// one explicitly -- that's fine, expected, and exactly what lets
	// resp.Body come back as plain decoded bytes. What must never happen
	// is the ORIGINAL client header value surviving untouched.
	if gotAcceptEncoding == "gzip, deflate, br" {
		t.Errorf("the client's own Accept-Encoding header must not be forwarded verbatim upstream (defeats Go's transparent gzip decoding): got %q", gotAcceptEncoding)
	}
}

func TestProxySandboxMux_ServesLocalesUnauthenticated(t *testing.T) {
	hs := sandboxTestHTTPServer(t, &config.ServerConfig{Port: 8080, ProxySandboxPort: 8444})
	mux := hs.buildProxySandboxMux()

	rr := httptest.NewRecorder()
	// No Authorization header or token at all.
	req := httptest.NewRequest(http.MethodGet, "/locales/en.json", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code == http.StatusUnauthorized {
		t.Error("/locales/ must be reachable without auth, matching the main origin's own behavior")
	}
}
