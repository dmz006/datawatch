package compute

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
)

// PullHTTPClient builds the HTTP client used to pull a model onto the node.
// The timeout is zero because a pull can run for a very long time.
//
// Certificate verification is always on for pulls: the server certificate is
// verified against the system roots, plus the node's tls_ca_file when set,
// and against the node's tls_cert_fingerprint when pinned — the same policy
// as node probes (NodeTLSConfig is the only place that builds node TLS
// configs, so this file sets no verification flags itself). A node whose TLS
// settings do not build a config (for example a fingerprint that is not 64
// hex chars, or tls_insecure_skip_verify combined with a pin) falls back to
// plain system-root verification instead of skipping checks.
//
// Behavior change: pulls used to skip verification unless verify_tls was on.
// verify_tls is kept for configuration compatibility but no longer turns
// verification off, and tls_insecure_skip_verify does not apply to pulls.
// To pull from a node whose certificate the system does not trust, name the
// trust: set tls_ca_file or tls_cert_fingerprint on the node. A plain http
// address is unaffected.
func PullHTTPClient(n *Node) *http.Client {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if n != nil {
		if pcfg, mode, err := NodeTLSConfig(n); err == nil && mode != TLSModeVerificationOff {
			cfg = pcfg
		}
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = cfg
	return &http.Client{Timeout: 0, Transport: tr}
}

// WrapPullError names the node and the certificate problem when a pull failed
// certificate verification. Pulls always verify, so the wrapping no longer
// depends on the node's verify_tls field. Any other error passes through
// unchanged.
func WrapPullError(n *Node, err error) error {
	if err == nil || n == nil {
		return err
	}
	var unknownAuth x509.UnknownAuthorityError
	var certInvalid x509.CertificateInvalidError
	var hostname x509.HostnameError
	var verifyErr *tls.CertificateVerificationError
	var pinMismatch fingerprintMismatchError
	if errors.As(err, &unknownAuth) || errors.As(err, &certInvalid) ||
		errors.As(err, &hostname) || errors.As(err, &verifyErr) ||
		errors.As(err, &pinMismatch) {
		return fmt.Errorf("node %s: certificate verification failed: %w; trust the certificate with tls_ca_file or tls_cert_fingerprint", n.Name, err)
	}
	return err
}
