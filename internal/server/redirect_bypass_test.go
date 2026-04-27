// v5.18.0 — integration test for the HTTP→HTTPS redirect bypass.
// The MCP channel bridge POSTs /api/channel/ready over plaintext
// HTTP on the loopback interface; the redirect handler must serve
// it directly via the main mux instead of 307'ing to HTTPS where
// the bridge fails on the daemon's self-signed cert.

package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeMainMux returns the server.Handler the redirect bypass falls
// through to. We can't easily construct a full HTTPServer in a unit
// test (it pulls config + manager + hub), so the test installs a
// minimal fake handler on s.srv and asserts that requests routed
// through the bypass land there.
func newTestRedirectServer(t *testing.T, mainHandler http.Handler) *HTTPServer {
	t.Helper()
	s := &HTTPServer{
		srv: &http.Server{Handler: mainHandler},
	}
	return s
}

func TestRedirectToTLSHandler_LoopbackChannelBypass(t *testing.T) {
	mainHits := 0
	mainHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mainHits++
		w.Header().Set("X-Bypass", "yes")
		_, _ = w.Write([]byte(`{"ok":true,"path":"` + r.URL.Path + `"}`))
	})
	s := newTestRedirectServer(t, mainHandler)
	h := s.redirectToTLSHandler(8443)

	cases := []struct {
		name       string
		remote     string
		path       string
		wantStatus int
		wantBypass bool
	}{
		{"loopback v4 + channel/ready bypasses", "127.0.0.1:51234", "/api/channel/ready", http.StatusOK, true},
		{"loopback v4 + channel/notify bypasses", "127.0.0.1:51234", "/api/channel/notify", http.StatusOK, true},
		{"loopback v6 + channel bypasses", "[::1]:51234", "/api/channel/reply", http.StatusOK, true},
		{"loopback IPv4-mapped IPv6 bypasses", "[::ffff:127.0.0.1]:51234", "/api/channel/ready", http.StatusOK, true},
		{"loopback + non-channel path redirects", "127.0.0.1:51234", "/api/sessions", http.StatusTemporaryRedirect, false},
		{"non-loopback + channel path redirects (the safety case)", "10.0.0.42:51234", "/api/channel/ready", http.StatusTemporaryRedirect, false},
		{"non-loopback + non-channel redirects", "10.0.0.42:51234", "/api/sessions", http.StatusTemporaryRedirect, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(`{}`))
			req.RemoteAddr = tc.remote
			req.Host = "datawatch.local"
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)

			if rr.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d (body=%s)", rr.Code, tc.wantStatus, rr.Body.String())
			}
			gotBypass := rr.Header().Get("X-Bypass") == "yes"
			if gotBypass != tc.wantBypass {
				t.Errorf("bypass = %v, want %v", gotBypass, tc.wantBypass)
			}
			if tc.wantStatus == http.StatusTemporaryRedirect {
				loc := rr.Header().Get("Location")
				if !strings.HasPrefix(loc, "https://datawatch.local:8443") {
					t.Errorf("redirect target = %q, want https://datawatch.local:8443/...", loc)
				}
				if !strings.Contains(loc, tc.path) {
					t.Errorf("redirect target %q lost path %q", loc, tc.path)
				}
			}
		})
	}

	if mainHits == 0 {
		t.Fatal("no bypass cases ever reached the main handler — the bypass is not wired")
	}
}

// TestRedirectToTLSHandler_PathPrefixIsExact asserts that we don't
// accidentally bypass paths that *start with* /api/channel but mean
// something else — e.g. a future /api/channels-list endpoint must
// still redirect, since /api/channel/ requires the trailing slash.
func TestRedirectToTLSHandler_PathPrefixIsExact(t *testing.T) {
	mainHits := 0
	main := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mainHits++
	})
	s := newTestRedirectServer(t, main)
	h := s.redirectToTLSHandler(8443)

	for _, p := range []string{"/api/channels", "/api/channels/foo", "/api/channelsfoo"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		req.RemoteAddr = "127.0.0.1:51234"
		req.Host = "h"
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusTemporaryRedirect {
			t.Errorf("loopback %s should redirect, got %d", p, rr.Code)
		}
	}
	if mainHits != 0 {
		t.Errorf("/api/channels* paths must NOT bypass the redirect; main hits = %d", mainHits)
	}
}

// TestRedirectToTLSHandler_BodyForwarded asserts that a POST request
// with a body (the actual notifyReady() shape) survives the bypass —
// i.e. the request body reaches the main handler unchanged. Catches
// future refactors that might consume + re-marshal.
func TestRedirectToTLSHandler_BodyForwarded(t *testing.T) {
	var got string
	main := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
	})
	s := newTestRedirectServer(t, main)
	h := s.redirectToTLSHandler(8443)

	body := `{"session_id":"ralfthewise-eac4","port":41117}`
	req := httptest.NewRequest(http.MethodPost, "/api/channel/ready", strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:51234"
	req.Host = "h"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if got != body {
		t.Fatalf("body lost during bypass: got %q want %q", got, body)
	}
	_ = rr
}
