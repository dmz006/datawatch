package compute

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTLSTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func serverFingerprint(srv *httptest.Server) string {
	sum := sha256.Sum256(srv.Certificate().Raw)
	return hex.EncodeToString(sum[:])
}

func writeServerCA(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca.pem")
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// probe performs a GET against srv using the TLS config built for n.
func probe(t *testing.T, srv *httptest.Server, n *Node) (TLSMode, error) {
	t.Helper()
	cfg, mode, err := NodeTLSConfig(n)
	if err != nil {
		return "", err
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}
	defer client.CloseIdleConnections()
	resp, err := client.Get(srv.URL)
	if err != nil {
		return mode, err
	}
	_ = resp.Body.Close()
	return mode, nil
}

func tlsTestNode() *Node {
	return &Node{Name: "tls-node", Kind: KindRemote, Address: "https://127.0.0.1"}
}

func TestNodeTLSDefaultRejectsSelfSigned(t *testing.T) {
	srv := newTLSTestServer(t)
	mode, err := probe(t, srv, tlsTestNode())
	if err == nil {
		t.Fatal("default config accepted a self-signed certificate")
	}
	if mode != TLSModeVerified {
		t.Errorf("mode = %q, want %q", mode, TLSModeVerified)
	}
}

func TestNodeTLSCAFileTrustsServer(t *testing.T) {
	srv := newTLSTestServer(t)
	n := tlsTestNode()
	n.TLSCAFile = writeServerCA(t, srv)
	mode, err := probe(t, srv, n)
	if err != nil {
		t.Fatalf("probe with CA file: %v", err)
	}
	if mode != TLSModeCAFile {
		t.Errorf("mode = %q, want %q", mode, TLSModeCAFile)
	}
}

func TestNodeTLSFingerprintPin(t *testing.T) {
	srv := newTLSTestServer(t)

	n := tlsTestNode()
	n.TLSCertFingerprint = serverFingerprint(srv)
	mode, err := probe(t, srv, n)
	if err != nil {
		t.Fatalf("probe with matching fingerprint: %v", err)
	}
	if mode != TLSModePinned {
		t.Errorf("mode = %q, want %q", mode, TLSModePinned)
	}

	bad := tlsTestNode()
	bad.TLSCertFingerprint = strings.Repeat("ab", 32)
	if _, err := probe(t, srv, bad); err == nil {
		t.Fatal("wrong fingerprint was accepted")
	}
}

func TestNodeTLSCAFileAndFingerprint(t *testing.T) {
	srv := newTLSTestServer(t)

	ok := tlsTestNode()
	ok.TLSCAFile = writeServerCA(t, srv)
	ok.TLSCertFingerprint = serverFingerprint(srv)
	mode, err := probe(t, srv, ok)
	if err != nil {
		t.Fatalf("probe with CA file and matching fingerprint: %v", err)
	}
	if mode != TLSModeCAFilePinned {
		t.Errorf("mode = %q, want %q", mode, TLSModeCAFilePinned)
	}

	bad := tlsTestNode()
	bad.TLSCAFile = writeServerCA(t, srv)
	bad.TLSCertFingerprint = strings.Repeat("00", 32)
	if _, err := probe(t, srv, bad); err == nil {
		t.Fatal("CA file with wrong fingerprint was accepted")
	}
}

func TestNodeTLSCAFileAndFingerprintRejectsUntrustedChain(t *testing.T) {
	srv := newTLSTestServer(t)

	// Right fingerprint, but the CA file holds an unrelated certificate.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "unrelated test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	caPath := filepath.Join(t.TempDir(), "unrelated.pem")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	n := tlsTestNode()
	n.TLSCAFile = caPath
	n.TLSCertFingerprint = serverFingerprint(srv)
	if _, err := probe(t, srv, n); err == nil {
		t.Fatal("chain not rooted in the CA file was accepted")
	}
}

func TestNodeTLSSkipVerifyReportsVerificationOff(t *testing.T) {
	srv := newTLSTestServer(t)
	n := tlsTestNode()
	n.TLSInsecureSkipVerify = true
	mode, err := probe(t, srv, n)
	if err != nil {
		t.Fatalf("probe with skip verify: %v", err)
	}
	if mode != TLSModeVerificationOff {
		t.Errorf("mode = %q, want %q", mode, TLSModeVerificationOff)
	}
}

func TestNodeTLSValidateRejectsBadSettings(t *testing.T) {
	cases := map[string]func(n *Node){
		"short fingerprint":   func(n *Node) { n.TLSCertFingerprint = "abcd" },
		"non-hex fingerprint": func(n *Node) { n.TLSCertFingerprint = strings.Repeat("zz", 32) },
		"skip with CA file": func(n *Node) {
			n.TLSInsecureSkipVerify = true
			n.TLSCAFile = "/etc/ssl/ca.pem"
		},
		"skip with fingerprint": func(n *Node) {
			n.TLSInsecureSkipVerify = true
			n.TLSCertFingerprint = strings.Repeat("ab", 32)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			n := tlsTestNode()
			mutate(n)
			err := n.Validate()
			if err == nil {
				t.Fatal("Validate accepted invalid TLS settings")
			}
			if strings.Contains(name, "fingerprint") && !strings.Contains(name, "skip") &&
				!strings.Contains(err.Error(), "tls_cert_fingerprint") {
				t.Errorf("error %q does not name tls_cert_fingerprint", err)
			}
			if strings.Contains(name, "skip") && !strings.Contains(err.Error(), "choose one") {
				t.Errorf("error %q lacks the 'choose one' guidance", err)
			}
		})
	}
}

func TestNodeTLSUnusableCAFileIsError(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.pem")
	n := tlsTestNode()
	n.TLSCAFile = missing
	if _, _, err := NodeTLSConfig(n); err == nil || !strings.Contains(err.Error(), missing) {
		t.Errorf("unreadable CA file: err = %v, want error naming %q", err, missing)
	}

	junk := filepath.Join(dir, "junk.pem")
	if err := os.WriteFile(junk, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	n.TLSCAFile = junk
	if _, _, err := NodeTLSConfig(n); err == nil || !strings.Contains(err.Error(), junk) {
		t.Errorf("non-PEM CA file: err = %v, want error naming %q", err, junk)
	}
}

func TestNodeTLSFingerprintNormalisation(t *testing.T) {
	srv := newTLSTestServer(t)
	plain := serverFingerprint(srv)
	var parts []string
	for i := 0; i < len(plain); i += 2 {
		parts = append(parts, strings.ToUpper(plain[i:i+2]))
	}
	n := tlsTestNode()
	n.TLSCertFingerprint = " " + strings.Join(parts, ":") + " "
	if err := n.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if n.TLSCertFingerprint != plain {
		t.Errorf("fingerprint = %q, want %q", n.TLSCertFingerprint, plain)
	}
	if _, err := probe(t, srv, n); err != nil {
		t.Fatalf("probe with normalised fingerprint: %v", err)
	}
}

func TestNodeTLSValidateTrimsCAFile(t *testing.T) {
	n := tlsTestNode()
	n.TLSCAFile = "  /etc/ssl/ca.pem \n"
	if err := n.Validate(); err != nil {
		t.Fatal(err)
	}
	if n.TLSCAFile != "/etc/ssl/ca.pem" {
		t.Errorf("TLSCAFile = %q, want trimmed", n.TLSCAFile)
	}
}

func TestNodeTLSSettingsKeyChangesWithEachSetting(t *testing.T) {
	base := &Node{Name: "n"}
	seen := map[string]string{base.TLSSettingsKey(): "base"}
	for label, n := range map[string]*Node{
		"ca":          {TLSCAFile: "/x/ca.pem"},
		"fingerprint": {TLSCertFingerprint: strings.Repeat("ab", 32)},
		"opt-out":     {TLSInsecureSkipVerify: true},
	} {
		k := n.TLSSettingsKey()
		if prev, dup := seen[k]; dup {
			t.Errorf("%s key collides with %s: %q", label, prev, k)
		}
		seen[k] = label
	}
	if (&Node{Name: "a"}).TLSSettingsKey() != (&Node{Name: "b"}).TLSSettingsKey() {
		t.Error("the key must depend only on the TLS settings")
	}
}
