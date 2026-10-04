// BL394 security review (docs/plans/2026-10-03-bl394-security-findings-
// review.md §6) — the proxy sandbox origin. Covers: the main mux's
// /remote/ redirects (never serves proxied content itself anymore), the
// sandbox mux only exposes /remote/ + /api/proxy/ (nothing else), and the
// sandbox CSP's frame-ancestors names the real main origin.

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// TestHandleRemotePWA_StripsUpstreamSecurityHeaders is a regression test
// for a real bug found live-testing the sandbox origin (BL394 §6): a
// proxied remote peer's OWN response already carries its own security
// headers (CSP, X-Frame-Options, etc., describing the REMOTE's own
// origin). handleRemotePWA's header-copy loop was forwarding those
// verbatim, landing a second, conflicting Content-Security-Policy
// alongside the proxy's own -- multiple CSP headers combine as an AND
// per directive, so the stray copy could silently make the *correct*
// policy wrong without either one looking broken in isolation. Confirmed
// live (curl showed two Content-Security-Policy lines, one correct, one
// the remote's own 'self') before being fixed by skipping every security
// header name when copying the upstream response.
func TestHandleRemotePWA_StripsUpstreamSecurityHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'self'")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Permissions-Policy", "camera=(self)")
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>upstream content</body></html>"))
	}))
	defer upstream.Close()

	s := newTestServer(t, nil, nil)
	s.cfg = &config.Config{Servers: []config.RemoteServerConfig{
		{Name: "peer-a", URL: upstream.URL, Enabled: true},
	}}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/remote/peer-a/", nil)
	s.handleRemotePWA(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rr.Code, rr.Body.String())
	}
	csp := rr.Header().Values("Content-Security-Policy")
	if len(csp) != 0 {
		t.Errorf("handleRemotePWA must not forward the upstream's own CSP at all (the caller's own middleware sets it); got %v", csp)
	}
	if v := rr.Header().Get("X-Frame-Options"); v != "" {
		t.Errorf("X-Frame-Options leaked from upstream: %q", v)
	}
	if v := rr.Header().Get("Permissions-Policy"); v != "" {
		t.Errorf("Permissions-Policy leaked from upstream: %q", v)
	}
	if !containsAll(rr.Body.String(), "upstream content") {
		t.Errorf("proxied body missing, got: %s", rr.Body.String())
	}
}

func sandboxTestHTTPServer(t *testing.T, cfg *config.ServerConfig) *HTTPServer {
	t.Helper()
	api := newTestServer(t, nil, nil)
	api.cfg = &config.Config{Server: *cfg}
	return &HTTPServer{cfg: cfg, api: api}
}

func TestRedirectToProxySandbox_301sToSandboxOrigin(t *testing.T) {
	cfg := &config.ServerConfig{Port: 8080, ProxySandboxPort: 8444}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/remote/peer-a/some/path?x=1", nil)
	req.Host = "example.com:8080"
	redirectToProxySandbox(cfg, rr, req)

	if rr.Code != http.StatusMovedPermanently {
		t.Fatalf("status=%d, want 301", rr.Code)
	}
	loc := rr.Header().Get("Location")
	want := "http://example.com:8444/remote/peer-a/some/path?x=1"
	if loc != want {
		t.Errorf("Location=%q, want %q", loc, want)
	}
}

func TestRedirectToProxySandbox_DisabledReturns503(t *testing.T) {
	cfg := &config.ServerConfig{Port: 8080, ProxySandboxPort: 0}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/remote/peer-a/", nil)
	redirectToProxySandbox(cfg, rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503 when disabled", rr.Code)
	}
}

func TestCounterpartOrigin_UsesTLSPortInDualMode(t *testing.T) {
	cfg := &config.ServerConfig{Port: 8080, TLSEnabled: true, TLSPort: 8443}
	req := httptest.NewRequest(http.MethodGet, "https://my-host:8444/x", nil)
	req.Host = "my-host:8444"
	got := counterpartOrigin(cfg, req, effectiveMainPort(cfg))
	want := "https://my-host:8443"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildSandboxCSP_NamesMainOriginNotSelf(t *testing.T) {
	csp := buildSandboxCSP("https://my-host:8443")
	if !containsAll(csp, "frame-ancestors https://my-host:8443") {
		t.Errorf("CSP missing the main-origin frame-ancestors: %s", csp)
	}
	if containsAll(csp, "frame-ancestors 'self'") {
		t.Errorf("sandbox CSP must not use 'self' for frame-ancestors (that's the whole point): %s", csp)
	}
}

func containsAll(s, substr string) bool {
	return len(s) >= len(substr) && (func() bool {
		for i := 0; i+len(substr) <= len(s); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}

func TestProxySandboxMux_ServesRemoteAndProxyOnly(t *testing.T) {
	hs := sandboxTestHTTPServer(t, &config.ServerConfig{Port: 8080, ProxySandboxPort: 8444})
	mux := hs.buildProxySandboxMux()

	// /remote/{name} with no real peer configured still reaches
	// handleRemotePWA (not a 404 from a missing route) -- it should
	// 404 for "server not found", a different failure mode than the
	// mux itself not knowing the path.
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/remote/nonexistent-peer/", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("GET /remote/nonexistent-peer/ status=%d, want 404 (server not found)", rr.Code)
	}
	if rr.Body.Len() == 0 || !containsAll(rr.Body.String(), "not found or disabled") {
		t.Errorf("expected handleRemotePWA's own 'not found or disabled' message, got: %s", rr.Body.String())
	}

	// /api/proxy/{name}/... likewise reaches handleProxy.
	rr2 := httptest.NewRecorder()
	mux.ServeHTTP(rr2, httptest.NewRequest(http.MethodGet, "/api/proxy/nonexistent-peer/x", nil))
	if rr2.Code != http.StatusNotFound {
		t.Fatalf("GET /api/proxy/nonexistent-peer/x status=%d, want 404 (server not found)", rr2.Code)
	}

	// Nothing else is registered -- confirms the sandbox mux's surface
	// is deliberately narrow, not a copy of the full API.
	for _, path := range []string{"/api/sessions", "/api/config", "/healthz", "/"} {
		rr3 := httptest.NewRecorder()
		mux.ServeHTTP(rr3, httptest.NewRequest(http.MethodGet, path, nil))
		if rr3.Code != http.StatusNotFound {
			t.Errorf("GET %s on the sandbox mux status=%d, want 404 (route must not exist here)", path, rr3.Code)
		}
	}
}

func TestProxySandboxMux_SetsSandboxCSPWithMainOriginFrameAncestors(t *testing.T) {
	hs := sandboxTestHTTPServer(t, &config.ServerConfig{Port: 8080, ProxySandboxPort: 8444})
	mux := hs.buildProxySandboxMux()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/remote/nonexistent-peer/", nil)
	req.Host = "my-host:8444"
	mux.ServeHTTP(rr, req)

	csp := rr.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("no Content-Security-Policy header set on the sandbox origin")
	}
	if !containsAll(csp, "frame-ancestors http://my-host:8080") {
		t.Errorf("CSP frame-ancestors should name the main origin (http://my-host:8080): %s", csp)
	}
}
