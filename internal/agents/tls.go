// F10 sprint 4 (S4.3) — in-cluster TLS pinning.
//
// Workers boot inside containers/Pods that don't yet trust the
// parent's TLS certificate (it's typically a private/self-signed cert
// — Sprint 4's `trusted_cas` field on ClusterProfile addresses
// generic trust, but the parent-callback path benefits from a
// stricter approach: pin a known SHA-256 fingerprint of the parent's
// leaf certificate, so even if a CA gets compromised the worker only
// trusts the specific cert the parent presents.
//
// Flow:
//   1. Parent computes its own cert fingerprint at startup
//      (FingerprintFromPEMFile or FingerprintFromCert)
//   2. Driver injects DATAWATCH_PARENT_CERT_FINGERPRINT into the
//      spawned container's env (DockerDriver + K8sDriver, both)
//   3. Worker reads the env var and builds a tls.Config whose
//      VerifyPeerCertificate compares the leaf's SHA-256 to the
//      pinned value. Mismatch → connection refused; no fallback.
//   4. When the env var is empty (legacy / dev), the worker falls
//      back to the existing InsecureSkipVerify behaviour. Sprint 5+
//      will tighten this to require pinning when TLS is enabled.

package agents

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
)

// FingerprintFromPEMFile returns the SHA-256 fingerprint of the first
// CERTIFICATE block in a PEM-encoded file. The format matches what
// `openssl x509 -fingerprint -sha256 -noout` emits, lower-cased and
// stripped of colons (e.g. "abc123…fed"). Returns an error if the
// file is missing or contains no CERTIFICATE block.
func FingerprintFromPEMFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read cert: %w", err)
	}
	return FingerprintFromPEM(data)
}

// FingerprintFromPEM is FingerprintFromPEMFile but takes the bytes
// directly. Useful when the cert lives in memory (e.g. auto-generated
// at startup and not yet flushed to disk).
func FingerprintFromPEM(data []byte) (string, error) {
	for {
		block, rest := pem.Decode(data)
		if block == nil {
			return "", errors.New("no CERTIFICATE block in PEM data")
		}
		if block.Type == "CERTIFICATE" {
			sum := sha256.Sum256(block.Bytes)
			return hex.EncodeToString(sum[:]), nil
		}
		data = rest
	}
}

// FingerprintFromCert is the fast path when the cert is already
// parsed (e.g. tls.Certificate.Leaf populated by tls.LoadX509KeyPair
// follow-up).
func FingerprintFromCert(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

// PinnedTLSConfig returns a *tls.Config that accepts ONLY a
// certificate whose SHA-256 fingerprint matches the supplied value.
// The hostname check is skipped (Pod-internal IPs/hostnames vary by
// cluster) — the fingerprint is the sole authentication signal.
//
// fingerprint must be a hex-encoded SHA-256 (case-insensitive). Empty
// fingerprint returns an error rather than silently falling back to
// InsecureSkipVerify — call sites must make that decision explicitly.
func PinnedTLSConfig(fingerprint string) (*tls.Config, error) {
	want := strings.ToLower(strings.TrimSpace(fingerprint))
	if want == "" {
		return nil, errors.New("empty fingerprint")
	}
	// Strip colons in case the operator pasted openssl-format output.
	want = strings.ReplaceAll(want, ":", "")

	return &tls.Config{
		// We do our own validation in VerifyPeerCertificate; disable
		// the default chain check so private/self-signed parent certs
		// don't fail before our verifier sees them.
		InsecureSkipVerify: true, //nolint:gosec
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("no peer certificates presented")
			}
			sum := sha256.Sum256(rawCerts[0])
			got := hex.EncodeToString(sum[:])
			if got != want {
				return fmt.Errorf("parent cert fingerprint mismatch: got %s want %s", got, want)
			}
			return nil
		},
	}, nil
}
