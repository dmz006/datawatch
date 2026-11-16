// Fakes for the pull-client certificate policy. newFakeOllamaPullServer
// serves the Ollama /api/pull and /api/tags contracts over a local
// self-signed TLS server (or plain http); no external service, no sleep, no
// system trust store is touched — the trust decision under test is exactly
// the client config PullHTTPClient installs.

package compute

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newFakeOllamaPullServer(useTLS bool) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/pull", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = fmt.Fprint(w, `{"status":"pulling manifest","total":100,"completed":10}
{"status":"success"}
`)
	})
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []any{map[string]any{"name": "tiny:latest"}}})
	})
	if !useTLS {
		return httptest.NewServer(mux)
	}
	// httptest.NewTLSServer provisions a self-signed certificate; the system
	// trust store does not include it, which is exactly the case under test.
	return httptest.NewTLSServer(mux)
}

// leafFingerprint is the SHA-256 hex fingerprint a node's
// tls_cert_fingerprint must carry to pin the fake's leaf.
func leafFingerprint(srv *httptest.Server) string {
	sum := sha256.Sum256(srv.Certificate().Raw)
	return hex.EncodeToString(sum[:])
}

// writeLeafPEM stores the fake's leaf certificate as PEM in dir.
func writeLeafPEM(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	der := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	p := filepath.Join(t.TempDir(), "fake-ca.pem")
	if err := os.WriteFile(p, der, 0o600); err != nil {
		t.Fatalf("write PEM: %v", err)
	}
	return p
}

// pullOnce issues one /api/pull request through the client and returns the
// error (nil on success). It is the same contract PullModel drives, without
// pulling the inference package into this one.
func pullOnce(t *testing.T, client *http.Client, addr string) error {
	t.Helper()
	resp, err := client.Post(addr+"/api/pull", "application/json", strings.NewReader(`{"name":"tiny:latest"}`))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pull: status %d", resp.StatusCode)
	}
	return nil
}

// pullTLSConfigOf is the transport TLS config PullHTTPClient installs.
func pullTLSConfigOf(t *testing.T, n *Node) *tls.Config {
	t.Helper()
	tr, ok := PullHTTPClient(n).Transport.(*http.Transport)
	if !ok || tr.TLSClientConfig == nil {
		t.Fatalf("client transport has no TLS config")
	}
	return tr.TLSClientConfig
}

// Pulls always verify: the default client never skips certificate checks,
// even when the node asks for it (tls_insecure_skip_verify is a probe-mode
// flag and does not apply to pulls).
func TestPullHTTPClientNeverSkipsVerification(t *testing.T) {
	cases := []struct {
		name string
		n    *Node
	}{
		{"nil node", nil},
		{"default node", &Node{Name: "gpu-a", Address: "https://gpu-a:11434"}},
		{"legacy verify_tls off", &Node{Name: "gpu-a", VerifyTLS: false}},
		{"legacy verify_tls on", &Node{Name: "gpu-a", VerifyTLS: true}},
		{"node asking for skip", &Node{Name: "gpu-a", TLSInsecureSkipVerify: true}},
		{"conflicting settings", &Node{Name: "gpu-a", TLSInsecureSkipVerify: true, TLSCertFingerprint: strings.Repeat("ab", 32)}},
		{"bad fingerprint", &Node{Name: "gpu-a", TLSCertFingerprint: "not-hex"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := pullTLSConfigOf(t, tc.n)
			if cfg.InsecureSkipVerify {
				t.Fatalf("pull client skips certificate verification for %s", tc.name)
			}
			if cfg.MinVersion < tls.VersionTLS12 {
				t.Fatalf("MinVersion = %d, want at least TLS 1.2", cfg.MinVersion)
			}
		})
	}
}

// The heart of the fix: against a self-signed server, a pull with no trust
// path fails certificate verification; naming the trust (pin or CA file) is
// the only way it succeeds.
func TestPullTLSRequiresATrustPath(t *testing.T) {
	fake := newFakeOllamaPullServer(true)
	defer fake.Close()

	t.Run("no trust path fails", func(t *testing.T) {
		n := &Node{Name: "gpu-selfsigned", Address: fake.URL, VerifyTLS: false}
		err := pullOnce(t, PullHTTPClient(n), fake.URL)
		if err == nil {
			t.Fatalf("pull from a self-signed node without a trust path must fail")
		}
		wrapped := WrapPullError(n, err)
		if !strings.Contains(wrapped.Error(), "gpu-selfsigned") {
			t.Errorf("wrapped error must name the node: %v", wrapped)
		}
		if !strings.Contains(wrapped.Error(), "certificate verification") {
			t.Errorf("wrapped error must name the certificate problem: %v", wrapped)
		}
	})

	t.Run("fingerprint pin passes", func(t *testing.T) {
		n := &Node{Name: "gpu-pin", Address: fake.URL, TLSCertFingerprint: leafFingerprint(fake)}
		if err := pullOnce(t, PullHTTPClient(n), fake.URL); err != nil {
			t.Fatalf("pull with the right fingerprint pin must succeed: %v", err)
		}
	})

	t.Run("wrong fingerprint fails", func(t *testing.T) {
		other := strings.Repeat("ab", 32)
		if other == leafFingerprint(fake) {
			t.Skip("collision with the fake fingerprint")
		}
		n := &Node{Name: "gpu-wrongpin", Address: fake.URL, TLSCertFingerprint: other}
		err := pullOnce(t, PullHTTPClient(n), fake.URL)
		if err == nil {
			t.Fatalf("pull with the wrong fingerprint must fail")
		}
		if !strings.Contains(WrapPullError(n, err).Error(), "gpu-wrongpin") {
			t.Errorf("wrapped error must name the node: %v", err)
		}
	})

	t.Run("ca file passes", func(t *testing.T) {
		ca := writeLeafPEM(t, fake)
		n := &Node{Name: "gpu-ca", Address: fake.URL, TLSCAFile: ca}
		if err := pullOnce(t, PullHTTPClient(n), fake.URL); err != nil {
			t.Fatalf("pull with the leaf in tls_ca_file must succeed: %v", err)
		}
	})

	t.Run("skip flag does not apply to pulls", func(t *testing.T) {
		n := &Node{Name: "gpu-skip", Address: fake.URL, TLSInsecureSkipVerify: true}
		if err := pullOnce(t, PullHTTPClient(n), fake.URL); err == nil {
			t.Fatalf("tls_insecure_skip_verify must not let a pull skip verification")
		}
	})
}

// A plain http node is unaffected by the TLS policy.
func TestPullPlainHTTPStillWorks(t *testing.T) {
	fake := newFakeOllamaPullServer(false)
	defer fake.Close()
	n := &Node{Name: "gpu-http", Address: fake.URL}
	if err := pullOnce(t, PullHTTPClient(n), fake.URL); err != nil {
		t.Fatalf("plain http pull must work: %v", err)
	}
}

// A real Ollama-style endpoint (here the tags listing) still enforces the
// same policy on the pull client.
func TestPullTLSOnTagsEndpoint(t *testing.T) {
	fake := newFakeOllamaPullServer(true)
	defer fake.Close()

	get := func(n *Node) error {
		resp, err := PullHTTPClient(n).Get(fake.URL + "/api/tags")
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()
		return nil
	}
	n := &Node{Name: "gpu-tags", Address: fake.URL}
	if err := get(n); err == nil {
		t.Fatalf("tags call from a self-signed node without a trust path must fail")
	}
	n2 := &Node{Name: "gpu-tags-pin", Address: fake.URL, TLSCertFingerprint: leafFingerprint(fake)}
	if err := get(n2); err != nil {
		t.Fatalf("tags call with a fingerprint pin must succeed: %v", err)
	}
}

// WrapPullError wraps certificate failures for any node (the verify_tls gate
// is gone — pulls always verify) and passes everything else through.
func TestVerifyTLSWrapPullError(t *testing.T) {
	certErr := x509.UnknownAuthorityError{}
	cases := []struct {
		name       string
		n          *Node
		err        error
		wantNode   bool
		wantUnwrap bool
	}{
		{"nil error", &Node{Name: "n"}, nil, false, false},
		{"nil node", nil, fmt.Errorf("dial refused"), false, false},
		{"verify_tls off still wraps", &Node{Name: "gpu-off", VerifyTLS: false}, certErr, true, true},
		{"verify_tls on wraps", &Node{Name: "gpu-on", VerifyTLS: true}, certErr, true, true},
		{"non-certificate error passes through", &Node{Name: "gpu-io", VerifyTLS: true},
			fmt.Errorf("read tcp: connection reset"), false, false},
		{"hostname error wraps", &Node{Name: "gpu-host"}, x509.HostnameError{}, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := WrapPullError(tc.n, tc.err)
			if tc.err == nil && got != nil {
				t.Fatalf("nil in, got %v", got)
			}
			if tc.wantNode {
				if !strings.Contains(got.Error(), tc.n.Name) ||
					!strings.Contains(got.Error(), "certificate verification") {
					t.Fatalf("wrapped error must name the node and the certificate problem: %v", got)
				}
				if !errors.Is(got, tc.err) {
					t.Fatalf("wrapped error must still unwrap to the original: %v", got)
				}
			} else if tc.err != nil && got != tc.err {
				t.Fatalf("non-certificate error must pass through unchanged: %v", got)
			}
		})
	}
}
