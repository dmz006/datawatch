package fingerprint

// The fingerprint-sync and peer-usage peer pulls must route through the
// SAME shared TLS policy as every other peer call: the pinned SHA-256
// certificate fingerprint (when set) wins over TLSSkipVerify, and with
// neither set the client keeps default CA-chain verification. These
// tests exercise the per-peer clientFor path (no injected Client)
// against a real self-signed TLS peer: default is refused, the correct
// pin succeeds, a wrong pin fails, and skip-verify still works.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// tlsPeerSrv starts a self-signed peer answering the capacity and
// fingerprint endpoints and returns its pin (lowercase hex SHA-256 of
// the leaf certificate, the same shape every config surface stores).
func tlsPeerSrv(t *testing.T, token string, exp *Exporter) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Bearer "+token; got != want {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/api/capacity":
			serveCapacity(3)(w, r)
		default:
			key, err := PairKey(token)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if err := json.NewEncoder(w).Encode(exp.Export(key)); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
			}
		}
	}))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(srv.Certificate().Raw)
	return srv, hex.EncodeToString(sum[:])
}

func TestSyncPeerPullUsesTLSPin(t *testing.T) {
	token := "token-tls"
	srv, pin := tlsPeerSrv(t, token, nodeExporter("peer-node", "uuid-9"))
	local := nodeExporter("local-node", "uuid-9")

	syncOnce := func(e multiserver.Entry) (records int) {
		e.Name = "peer"
		e.URL = srv.URL
		e.Token = token
		e.FingerprintSync = true
		store := testStore(t)
		s := testSyncer([]multiserver.Entry{e}, local, store)
		s.SyncOnce(context.Background())
		return len(store.List(""))
	}

	// Default verification: a self-signed peer is refused — no match.
	if got := syncOnce(multiserver.Entry{}); got != 0 {
		t.Errorf("default (no pin, no skip) = %d match records, want 0", got)
	}
	// Correct pin: the pull succeeds and the shared node is matched.
	if got := syncOnce(multiserver.Entry{TLSCertFingerprint: pin}); got != 1 {
		t.Errorf("pinned peer = %d match records, want 1", got)
	}
	// Wrong pin: the handshake is refused — no match.
	if got := syncOnce(multiserver.Entry{TLSCertFingerprint: strings.Repeat("ab", 32)}); got != 0 {
		t.Errorf("wrong-pin peer = %d match records, want 0", got)
	}
	// Skip-verify (no pin) still works.
	if got := syncOnce(multiserver.Entry{TLSSkipVerify: true}); got != 1 {
		t.Errorf("skip-verify peer = %d match records, want 1", got)
	}
}

func TestPeerUsageFetchUsesTLSPin(t *testing.T) {
	// "tok" is the bearer that serveCapacity (gate_test.go) expects.
	srv, pin := tlsPeerSrv(t, "tok", nodeExporter("peer-node", "uuid-9"))
	base := PeerRef{Name: "peer-a", URL: srv.URL, Token: "tok", OptedIn: true}

	// Default verification: refresh fails soft, nothing cached.
	c := &PeerUsageCache{}
	if err := c.Refresh(context.Background(), base); err == nil {
		t.Errorf("default (no pin, no skip) refresh succeeded against a self-signed peer")
	}
	if n, ok := c.Held("peer-a", "node:peer-node"); ok || n != 0 {
		t.Errorf("default Held = %d,%v, want 0,false", n, ok)
	}

	// Correct pin: refresh succeeds and the held count is cached.
	pinned := base
	pinned.TLSCertFingerprint = pin
	c = &PeerUsageCache{}
	if err := c.Refresh(context.Background(), pinned); err != nil {
		t.Fatalf("pinned refresh: %v", err)
	}
	if n, ok := c.Held("peer-a", "node:peer-node"); !ok || n != 3 {
		t.Errorf("pinned Held = %d,%v, want 3,true", n, ok)
	}

	// Wrong pin: refused.
	wrong := base
	wrong.TLSCertFingerprint = strings.Repeat("ab", 32)
	c = &PeerUsageCache{}
	if err := c.Refresh(context.Background(), wrong); err == nil {
		t.Errorf("wrong-pin refresh succeeded against a self-signed peer")
	}

	// Skip-verify (no pin) still works.
	skip := base
	skip.TLSSkipVerify = true
	c = &PeerUsageCache{}
	if err := c.Refresh(context.Background(), skip); err != nil {
		t.Fatalf("skip-verify refresh: %v", err)
	}
	if n, ok := c.Held("peer-a", "node:peer-node"); !ok || n != 3 {
		t.Errorf("skip-verify Held = %d,%v, want 3,true", n, ok)
	}
}

// TestPeerUsageClientRebuildsWhenPinChanges — the per-peer client is
// cached, but a change to either TLS setting must invalidate it so the
// new policy actually takes effect on the next fetch.
func TestPeerUsageClientRebuildsWhenPinChanges(t *testing.T) {
	token := "token-tls"
	srv, pin := tlsPeerSrv(t, token, nodeExporter("peer-node", "uuid-9"))
	base := PeerRef{Name: "peer-a", URL: srv.URL, Token: token, OptedIn: true}

	c := &PeerUsageCache{}
	first := c.clientFor(base)
	// Same settings: still the same client.
	if got := c.clientFor(base); got != first {
		t.Errorf("unchanged settings rebuilt the client")
	}
	// Pin change: new client.
	pinned := base
	pinned.TLSCertFingerprint = pin
	if got := c.clientFor(pinned); got == first {
		t.Errorf("pin change did not rebuild the client")
	}
	// Skip-verify change: new client again.
	skip := base
	skip.TLSSkipVerify = true
	if got := c.clientFor(skip); got == first {
		t.Errorf("skip-verify change did not rebuild the client")
	}
}
