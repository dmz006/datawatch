package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// securityFailureTokenDaemon is a fake token-protected daemon: it answers
// 401 to any request whose Authorization header is not the bearer token,
// the way the REST surface does when server.token is set. It records the
// paths it served so a test can see the request got through.
func securityFailureTokenDaemon(t *testing.T, token, body string) (*httptest.Server, int) {
	t.Helper()
	srv, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		_, _ = w.Write([]byte(body))
	})
	return srv, port
}

// The comm verb reaches a token-protected daemon once the router has the
// token: the list is read, not refused with HTTP 401.
func TestSecurityFailureCommReachesTokenProtectedDaemon(t *testing.T) {
	_, port := securityFailureTokenDaemon(t, "s3cret", securityFailureCommSample)
	r := setupPeerRouter(t, port)
	r.SetWebToken("s3cret")

	got := strings.Join(r.HandleTestMessage("autonomous security-failure"), "\n")
	if strings.Contains(got, "401") || strings.Contains(got, "unauthorized") {
		t.Fatalf("comm verb was refused by the token-protected daemon: %q", got)
	}
	for _, want := range []string{"golang.org/x/net", "GHSA-aaaa-bbbb-cccc", "example.com/old"} {
		if !strings.Contains(got, want) {
			t.Errorf("list lacks %q: %q", want, got)
		}
	}
}

// Without the token the same daemon refuses the read, and the verb says
// so: the failure is reported, not silently answered with an empty list.
func TestSecurityFailureCommWithoutTokenIsRefused(t *testing.T) {
	_, port := securityFailureTokenDaemon(t, "s3cret", securityFailureCommSample)
	r := setupPeerRouter(t, port)

	got := strings.Join(r.HandleTestMessage("autonomous security-failure"), "\n")
	if !strings.Contains(got, "HTTP 401") {
		t.Errorf("reply without a token = %q, want the HTTP 401 refusal", got)
	}
}

// A wrong token is refused too: the router sends the token it was given,
// not a stale or empty one.
func TestSecurityFailureCommWrongTokenIsRefused(t *testing.T) {
	_, port := securityFailureTokenDaemon(t, "s3cret", securityFailureCommSample)
	r := setupPeerRouter(t, port)
	r.SetWebToken("not-the-token")

	got := strings.Join(r.HandleTestMessage("autonomous security-failure"), "\n")
	if !strings.Contains(got, "HTTP 401") {
		t.Errorf("reply with a wrong token = %q, want the HTTP 401 refusal", got)
	}
}

// The write path (commJSON) presents the token too, so a comm write verb
// is not refused on a token-protected daemon either.
func TestCommJSONPresentsWebToken(t *testing.T) {
	_, port := securityFailureTokenDaemon(t, "s3cret", `{"ok":true}`)
	r := setupPeerRouter(t, port)
	r.SetWebToken("s3cret")

	got, err := r.commJSON(http.MethodPost, "/api/autonomous/security-failures", `{}`)
	if err != nil {
		t.Fatalf("commJSON with the token: %v", err)
	}
	if got != `{"ok":true}` {
		t.Errorf("commJSON body = %q, want the daemon reply", got)
	}

	r2 := setupPeerRouter(t, port)
	if _, err := r2.commJSON(http.MethodPost, "/api/autonomous/security-failures", `{}`); err == nil {
		t.Error("commJSON without the token was accepted, want the HTTP 401 refusal")
	}
}
