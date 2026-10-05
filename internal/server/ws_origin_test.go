// SEC-007 — WS upgrade same-origin check.

package server

import (
	"net/http/httptest"
	"testing"
)

func TestWSCheckOrigin_NoOriginHeader_Allowed(t *testing.T) {
	r := httptest.NewRequest("GET", "/ws", nil)
	r.Host = "localhost:8443"
	// No Origin set — a non-browser client (CLI, mobile app, websocat).
	if !wsCheckOrigin(r) {
		t.Error("a request with no Origin header must be allowed — non-browser clients aren't guaranteed to send one, and the bearer token is the real gate")
	}
}

func TestWSCheckOrigin_MatchingOrigin_Allowed(t *testing.T) {
	r := httptest.NewRequest("GET", "/ws", nil)
	r.Host = "localhost:8443"
	r.Header.Set("Origin", "https://localhost:8443")
	if !wsCheckOrigin(r) {
		t.Error("a same-origin browser request must be allowed")
	}
}

func TestWSCheckOrigin_MismatchedOrigin_Rejected(t *testing.T) {
	r := httptest.NewRequest("GET", "/ws", nil)
	r.Host = "localhost:8443"
	r.Header.Set("Origin", "https://evil.example")
	if wsCheckOrigin(r) {
		t.Error("a present, mismatched Origin (a browser tab on a different site) must be rejected")
	}
}

func TestWSCheckOrigin_MalformedOrigin_Rejected(t *testing.T) {
	r := httptest.NewRequest("GET", "/ws", nil)
	r.Host = "localhost:8443"
	r.Header.Set("Origin", "not a valid url \x7f")
	if wsCheckOrigin(r) {
		t.Error("a malformed Origin must be rejected, not treated as a parse-error pass-through")
	}
}
