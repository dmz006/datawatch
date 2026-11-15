package server

// The proxy WebSocket dialer must apply the peer's TLS policy through
// the same shared builder as every other peer call: the pinned SHA-256
// certificate fingerprint (when set) wins over TLSSkipVerify, and with
// neither set the dialer keeps default CA-chain verification. A
// malformed pin is refused before the dial instead of dialing with a
// guessed policy.

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/gorilla/websocket"
)

// newTLSPeerWS starts a self-signed peer whose /ws endpoint upgrades
// and echoes one text message back.
func newTLSPeerWS(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	up := websocket.Upgrader{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" {
			http.NotFound(w, r)
			return
		}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return
		}
	}))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(srv.Certificate().Raw)
	return srv, hex.EncodeToString(sum[:])
}

// proxyWSEcho serves handleProxyWS against a Server seeded with one
// remote entry and echoes one text frame end-to-end. It returns the
// relayed frame on success, and the non-2xx dial/upgrade error (status
// plus body) when the handler refuses before the client upgrade.
func proxyWSEcho(t *testing.T, remote config.RemoteServerConfig) (string, error) {
	t.Helper()
	remote.Name = "tlspeer"
	remote.Enabled = true
	s := newTestServer(t, nil, nil)
	s.cfg = &config.Config{Servers: []config.RemoteServerConfig{remote}}
	h := httptest.NewServer(http.HandlerFunc(s.handleProxyWS))
	t.Cleanup(h.Close)

	dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	wsURL := strings.Replace(h.URL, "http://", "ws://", 1) + "/api/proxy/tlspeer/ws"
	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		var body string
		if resp != nil {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			_ = resp.Body.Close()
			body = string(b)
		}
		return "", &proxyWSStatusErr{status: resp, body: body}
	}
	defer func() { _ = conn.Close() }()
	if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatalf("client write: %v", err)
	}
	_, data, err := conn.ReadMessage()
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// proxyWSStatusErr carries a refused-handshake response so a test can
// inspect both status code and body.
type proxyWSStatusErr struct {
	status *http.Response
	body   string
}

func (e *proxyWSStatusErr) Error() string {
	code := 0
	if e.status != nil {
		code = e.status.StatusCode
	}
	return "dial refused (HTTP " + strings.TrimSpace(http.StatusText(code)) + "): " + strings.TrimSpace(e.body)
}

func TestHandleProxyWS_PeerTLSPolicy(t *testing.T) {
	peer, pin := newTLSPeerWS(t)

	// Default verification: the self-signed peer must be refused.
	if _, err := proxyWSEcho(t, config.RemoteServerConfig{URL: peer.URL}); err == nil {
		t.Errorf("default (no pin, no skip) relayed a frame from a self-signed peer")
	} else if !strings.Contains(err.Error(), "unknown authority") {
		t.Errorf("default refusal = %v, want an unknown-authority error", err)
	}

	// Correct pin: the frame is relayed end-to-end.
	got, err := proxyWSEcho(t, config.RemoteServerConfig{URL: peer.URL, TLSCertFingerprint: pin})
	if err != nil || got != "ping" {
		t.Errorf("pinned relay = %q, %v; want %q, nil", got, err, "ping")
	}

	// Wrong pin: the handshake must be refused.
	wrong := strings.Repeat("ab", 32)
	if _, err := proxyWSEcho(t, config.RemoteServerConfig{URL: peer.URL, TLSCertFingerprint: wrong}); err == nil {
		t.Errorf("wrong-pin relay succeeded against a self-signed peer")
	}

	// Skip-verify (no pin) still works.
	got, err = proxyWSEcho(t, config.RemoteServerConfig{URL: peer.URL, TLSSkipVerify: true})
	if err != nil || got != "ping" {
		t.Errorf("skip-verify relay = %q, %v; want %q, nil", got, err, "ping")
	}

	// Malformed pin: refused by the handler before any dial (500),
	// never silently downgraded to default verification.
	if _, err := proxyWSEcho(t, config.RemoteServerConfig{URL: peer.URL, TLSCertFingerprint: "not-a-fingerprint"}); err == nil {
		t.Errorf("malformed-pin relay succeeded")
	}
}
