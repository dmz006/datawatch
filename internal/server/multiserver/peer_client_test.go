package multiserver

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func newTLSPeer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(srv.Certificate().Raw)
	return srv, hex.EncodeToString(sum[:])
}

func TestPeerHTTPClient_SkipVerifyTrue(t *testing.T) {
	srv, _ := newTLSPeer(t)
	resp, err := PeerHTTPClient(true, "", 10*time.Second).Get(srv.URL)
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}
	_ = resp.Body.Close()
}

func TestPeerHTTPClient_SkipVerifyFalse(t *testing.T) {
	srv, _ := newTLSPeer(t)
	_, err := PeerHTTPClient(false, "", 10*time.Second).Get(srv.URL)
	if err == nil || !strings.Contains(err.Error(), "certificate signed by unknown authority") {
		t.Fatalf("expected unknown authority error, got %v", err)
	}
}

func TestPeerHTTPClient_PinnedMatch(t *testing.T) {
	srv, fp := newTLSPeer(t)
	var colon []string
	for i := 0; i < len(fp); i += 2 {
		colon = append(colon, strings.ToUpper(fp[i:i+2]))
	}
	for _, pin := range []string{fp, strings.ToUpper(fp), strings.Join(colon, ":")} {
		resp, err := PeerHTTPClient(false, pin, 10*time.Second).Get(srv.URL)
		if err != nil {
			t.Fatalf("pin %q: expected success: %v", pin, err)
		}
		_ = resp.Body.Close()
	}
}

func TestPeerHTTPClient_PinnedMismatch(t *testing.T) {
	srv, _ := newTLSPeer(t)
	wrong := strings.Repeat("ab", 32)
	_, err := PeerHTTPClient(true, wrong, 10*time.Second).Get(srv.URL)
	if err == nil {
		t.Fatal("expected fingerprint mismatch error")
	}
	if !strings.Contains(err.Error(), "fingerprint mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
	// net/http wraps errors in *url.Error with the request URL; the pin
	// error underneath must not mention the peer address.
	var ue *url.Error
	if errors.As(err, &ue) && strings.Contains(ue.Err.Error(), srv.Listener.Addr().String()) {
		t.Fatalf("pin error leaks peer address: %v", ue.Err)
	}
}
