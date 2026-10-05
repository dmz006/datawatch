// SEC-006 — the main WS connection authenticates via Sec-WebSocket-
// Protocol (new WebSocket(url, [token])) instead of the removed ?token=
// query param, since browser JS can't set a custom Authorization header
// on a WS handshake.

package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gorilla/websocket"
)

func newWSTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	s := newTestServer(t, nil, nil)
	s.token = "admin-token"
	s.hub = NewHub()
	go s.hub.Run()
	srv := httptest.NewServer(s.fedAuthMiddleware(http.HandlerFunc(s.handleWS)))
	t.Cleanup(srv.Close)
	return s, srv
}

func TestWS_SecWebSocketProtocol_AuthenticatesAndEchoes(t *testing.T) {
	_, srv := newWSTestServer(t)
	wsURL := "ws://" + mustHost(t, srv.URL) + "/ws"

	dialer := websocket.Dialer{Subprotocols: []string{"admin-token"}}
	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial with token subprotocol should succeed: %v", err)
	}
	defer conn.Close() //nolint:errcheck
	if resp.Header.Get("Sec-WebSocket-Protocol") != "admin-token" {
		t.Errorf("server must echo the accepted subprotocol back; got %q", resp.Header.Get("Sec-WebSocket-Protocol"))
	}
	// gorilla's own dialer already validates the echoed subprotocol is
	// one it offered (it would have errored above otherwise) -- this is
	// the real client-side RFC 6455 check, not just a server-side claim.
}

func TestWS_SecWebSocketProtocol_WrongTokenRejected(t *testing.T) {
	_, srv := newWSTestServer(t)
	wsURL := "ws://" + mustHost(t, srv.URL) + "/ws"

	dialer := websocket.Dialer{Subprotocols: []string{"wrong-token"}}
	_, _, err := dialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("dial with a wrong token subprotocol must fail")
	}
}

func TestWS_QueryParamToken_Rejected(t *testing.T) {
	_, srv := newWSTestServer(t)
	wsURL := "ws://" + mustHost(t, srv.URL) + "/ws?token=admin-token"

	_, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("?token= must be rejected (SEC-006) — only the header or Sec-WebSocket-Protocol work")
	}
}

func mustHost(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %q: %v", rawURL, err)
	}
	return u.Host
}
