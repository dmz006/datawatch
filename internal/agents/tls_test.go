// F10 sprint 4 (S4.3) — TLS pinning tests.

package agents

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// makeSelfSignedPEM generates a tiny self-signed cert (P-256, 1y),
// returns the certificate's PEM bytes, the *x509.Certificate, and a
// tls.Certificate ready to attach to httptest.
func makeSelfSignedPEM(t *testing.T, cn string) ([]byte, *x509.Certificate, tls.Certificate) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, _ := x509.MarshalECPrivateKey(priv)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := x509.ParseCertificate(der)
	return certPEM, parsed, tlsCert
}

// FingerprintFromPEM matches the SHA-256 of the cert's DER bytes.
func TestFingerprintFromPEM_MatchesSHA256(t *testing.T) {
	certPEM, parsed, _ := makeSelfSignedPEM(t, "fp-test")
	got, err := FingerprintFromPEM(certPEM)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(parsed.Raw)
	wantHex := hex.EncodeToString(want[:])
	if got != wantHex {
		t.Errorf("fingerprint=%s want %s", got, wantHex)
	}
}

// FingerprintFromPEMFile reads the file and delegates.
func TestFingerprintFromPEMFile(t *testing.T) {
	certPEM, _, _ := makeSelfSignedPEM(t, "fp-file")
	dir := t.TempDir()
	path := filepath.Join(dir, "cert.pem")
	if err := os.WriteFile(path, certPEM, 0644); err != nil {
		t.Fatal(err)
	}
	got, err := FingerprintFromPEMFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 64 {
		t.Errorf("fingerprint length=%d want 64 hex chars", len(got))
	}
}

// FingerprintFromPEM rejects PEM blobs without a CERTIFICATE block.
func TestFingerprintFromPEM_NoCertBlock(t *testing.T) {
	_, err := FingerprintFromPEM([]byte("---not a cert---"))
	if err == nil {
		t.Error("expected error on malformed PEM")
	}
	// Real PEM but wrong type
	other := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte{1, 2, 3}})
	_, err = FingerprintFromPEM(other)
	if err == nil {
		t.Error("expected error when no CERTIFICATE block present")
	}
}

// PinnedTLSConfig with a matching fingerprint accepts the connection;
// a mismatching fingerprint rejects it. Exercises the full TLS
// handshake against an httptest TLS server.
func TestPinnedTLSConfig_RoundTrip(t *testing.T) {
	certPEM, parsed, tlsCert := makeSelfSignedPEM(t, "pin-rt")
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{tlsCert}}
	srv.StartTLS()
	defer srv.Close()
	_ = certPEM

	want := FingerprintFromCert(parsed)

	t.Run("matching fingerprint succeeds", func(t *testing.T) {
		cfg, err := PinnedTLSConfig(want)
		if err != nil {
			t.Fatal(err)
		}
		client := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}
		resp, err := client.Get(srv.URL)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if string(body) != "ok" {
			t.Errorf("body=%q want ok", body)
		}
	})

	t.Run("mismatched fingerprint refuses", func(t *testing.T) {
		bad := strings.Repeat("a", 64) // valid hex format, wrong value
		cfg, err := PinnedTLSConfig(bad)
		if err != nil {
			t.Fatal(err)
		}
		client := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}
		_, err = client.Get(srv.URL)
		if err == nil {
			t.Error("expected TLS handshake error")
		} else if !strings.Contains(err.Error(), "fingerprint mismatch") {
			t.Errorf("error=%v want contains 'fingerprint mismatch'", err)
		}
	})

	t.Run("openssl colon format normalised", func(t *testing.T) {
		// Insert colons every 2 hex chars + uppercase.
		var withColons []byte
		up := strings.ToUpper(want)
		for i := 0; i < len(up); i += 2 {
			if i > 0 {
				withColons = append(withColons, ':')
			}
			withColons = append(withColons, up[i], up[i+1])
		}
		cfg, err := PinnedTLSConfig(string(withColons))
		if err != nil {
			t.Fatal(err)
		}
		client := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}
		resp, err := client.Get(srv.URL)
		if err != nil {
			t.Fatalf("colon-format: %v", err)
		}
		resp.Body.Close()
	})
}

// PinnedTLSConfig refuses an empty fingerprint — call sites must
// make the InsecureSkipVerify decision explicitly, not by accident.
func TestPinnedTLSConfig_EmptyFingerprint(t *testing.T) {
	_, err := PinnedTLSConfig("")
	if err == nil {
		t.Error("expected error for empty fingerprint")
	}
}
