package multiserver

import (
	"crypto/sha256"
	"crypto/tls"
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

func tlsClientWith(cfg *tls.Config) *http.Client {
	if cfg == nil {
		return http.DefaultClient
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}
}

func TestPeerTLSConfig_Default(t *testing.T) {
	cfg, err := PeerTLSConfig(false, "")
	if err != nil {
		t.Fatalf("PeerTLSConfig: %v", err)
	}
	if cfg != nil {
		t.Fatalf("default mode: cfg = %+v, want nil (default verification)", cfg)
	}
	// nil config must fail against a self-signed test peer — the
	// client keeps full CA-chain verification.
	srv, _ := newTLSPeer(t)
	if _, err := tlsClientWith(nil).Get(srv.URL); err == nil {
		t.Fatal("default config accepted a self-signed certificate")
	}
}

func TestPeerTLSConfig_SkipVerify(t *testing.T) {
	cfg, err := PeerTLSConfig(true, "")
	if err != nil {
		t.Fatalf("PeerTLSConfig: %v", err)
	}
	if cfg == nil || !cfg.InsecureSkipVerify {
		t.Fatalf("skip mode: cfg = %+v, want InsecureSkipVerify", cfg)
	}
	srv, _ := newTLSPeer(t)
	resp, err := tlsClientWith(cfg).Get(srv.URL)
	if err != nil {
		t.Fatalf("skip mode should succeed: %v", err)
	}
	_ = resp.Body.Close()
}

func TestPeerTLSConfig_Pinned(t *testing.T) {
	srv, fp := newTLSPeer(t)
	// Pinned mode works and, per the mode contract, pin wins over
	// skipVerify: pass skipVerify=true with a correct pin.
	cfg, err := PeerTLSConfig(true, fp)
	if err != nil {
		t.Fatalf("PeerTLSConfig: %v", err)
	}
	if cfg == nil || cfg.VerifyPeerCertificate == nil {
		t.Fatalf("pin mode: cfg = %+v, want a fingerprint-verified config (VerifyPeerCertificate set)", cfg)
	}
	resp, err := tlsClientWith(cfg).Get(srv.URL)
	if err != nil {
		t.Fatalf("pinned mode should succeed: %v", err)
	}
	_ = resp.Body.Close()

	// Wrong pin: config builds fine, but the handshake must fail.
	wrong, err := PeerTLSConfig(false, strings.Repeat("cd", 32))
	if err != nil {
		t.Fatalf("PeerTLSConfig(wrong pin): %v", err)
	}
	if _, err := tlsClientWith(wrong).Get(srv.URL); err == nil {
		t.Fatal("wrong pin was accepted")
	}
}

func TestPeerTLSConfig_MalformedPinFailsClosed(t *testing.T) {
	if _, err := PeerTLSConfig(false, "not-a-fingerprint"); err == nil {
		t.Fatal("expected an error for a malformed pin")
	}
}

func TestValidatePeerTLS(t *testing.T) {
	fp := strings.Repeat("a1", 32)
	colon := make([]string, 0, 32)
	for i := 0; i < len(fp); i += 2 {
		colon = append(colon, strings.ToUpper(fp[i:i+2]))
	}
	want := strings.ToLower(fp)
	tests := []struct {
		name       string
		skipVerify bool
		pin        string
		want       string
		wantErr    bool
	}{
		{name: "empty pin ok", pin: "", want: ""},
		{name: "whitespace-only pin ok", pin: "   ", want: ""},
		{name: "lowercase hex", skipVerify: false, pin: want, want: want},
		{name: "uppercase hex normalized", pin: strings.ToUpper(want), want: want},
		{name: "colon-separated normalized", pin: strings.Join(colon, ":"), want: want},
		{name: "spaces stripped", pin: want[:4] + " " + want[4:], want: want},
		{name: "too short", pin: "a1b2", wantErr: true},
		{name: "too long", pin: want + "aa", wantErr: true},
		{name: "non-hex", pin: strings.Repeat("zz", 32), wantErr: true},
		{name: "pin with skipVerify rejected", skipVerify: true, pin: want, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidatePeerTLS(tt.skipVerify, tt.pin)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalized = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTLSWarning(t *testing.T) {
	fp := strings.Repeat("b2", 32)
	tests := []struct {
		name       string
		pairName   string
		skipVerify bool
		pin        string
		wantEmpty  bool
	}{
		{name: "verified peer", pairName: "prod", wantEmpty: true},
		{name: "pinned peer", pairName: "prod", pin: fp, wantEmpty: true},
		{name: "skip-verify peer warned", pairName: "prod", skipVerify: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TLSWarning(tt.pairName, tt.skipVerify, tt.pin)
			if tt.wantEmpty {
				if got != "" {
					t.Fatalf("warning = %q, want empty", got)
				}
				return
			}
			if got == "" {
				t.Fatal("expected a warning, got empty string")
			}
			for _, sub := range []string{tt.pairName, "not verified", "tls_cert_fingerprint"} {
				if !strings.Contains(got, sub) {
					t.Fatalf("warning %q should mention %q", got, sub)
				}
			}
			if strings.Count(got, "\n") != 0 {
				t.Fatalf("warning should be one line: %q", got)
			}
		})
	}
}
