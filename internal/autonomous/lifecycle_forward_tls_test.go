package autonomous

// The autonomous peer-forward NewClient seam must carry the peer's TLS
// settings straight into the same shared builder every other peer call
// uses (the daemon passes multiserver.PeerHTTPClient). This test wires
// that real factory through the seam against a self-signed peer: default
// verification is refused, the correct pin is accepted, a wrong pin is
// refused, and skip-verify still works.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/server/multiserver"
)

func TestPeerForwarderNewClientSeamUsesTLSPin(t *testing.T) {
	token := "tok"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ForwardedEventsPath {
			http.NotFound(w, r)
			return
		}
		if got, want := r.Header.Get("Authorization"), "Bearer "+token; got != want {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(srv.Certificate().Raw)
	pin := hex.EncodeToString(sum[:])

	f := NewPeerForwarder(PeerForwarderConfig{
		NewClient: multiserver.PeerHTTPClient,
	})
	set := ForwardSettings{Timeout: 5 * time.Second}
	base := PeerRef{Name: "peer", URL: srv.URL, Token: token}

	try := func(peer PeerRef) bool {
		client := f.client(peer, set)
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+ForwardedEventsPath, strings.NewReader("{}"))
		if err != nil {
			return false
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}

	if try(base) {
		t.Errorf("default (no pin, no skip) accepted a self-signed peer")
	}
	if !try(PeerRef{Name: base.Name, URL: base.URL, Token: base.Token, TLSCertFingerprint: pin}) {
		t.Errorf("correct pin was refused")
	}
	if try(PeerRef{Name: base.Name, URL: base.URL, Token: base.Token, TLSCertFingerprint: strings.Repeat("ab", 32)}) {
		t.Errorf("wrong pin was accepted")
	}
	if !try(PeerRef{Name: base.Name, URL: base.URL, Token: base.Token, TLSSkipVerify: true}) {
		t.Errorf("skip-verify was refused")
	}
}
