package multiserver

import (
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/compute"
)

// PeerTLSConfig builds the client TLS config for one peer, with the same
// three-mode shape as compute.NodeTLSConfig for the Ollama node pool:
//
//   - pin empty, skipVerify false: nil — the client does default CA-chain
//     verification.
//   - pin empty, skipVerify true:  verification disabled for this client
//     only; every other outbound call in the process keeps full
//     verification.
//   - pin set: the chain is not checked; the SHA-256 of the leaf
//     certificate must equal the pin (case-insensitive, colons and spaces
//     ignored). Pinning wins over skipVerify.
//
// Pinning reuses compute.NodeTLSConfig, the single fingerprint
// implementation shared with the Ollama node pool. Mismatch errors carry
// only the two fingerprints, never the peer URL.
func PeerTLSConfig(skipVerify bool, pin string) (*tls.Config, error) {
	pin = strings.TrimSpace(pin)
	if pin != "" {
		cfg, _, err := compute.NodeTLSConfig(&compute.Node{TLSCertFingerprint: pin})
		if err != nil {
			return nil, err
		}
		return cfg, nil
	}
	if skipVerify {
		return &tls.Config{InsecureSkipVerify: true}, nil // #nosec G402 -- operator-opted-in per peer, documented risk
	}
	return nil, nil
}

// PeerHTTPClient returns an HTTP client for talking to a peer over TLS.
// Built on PeerTLSConfig: pin set → pin verification (wins over
// skipVerify), skipVerify → no verification, otherwise default
// verification. A malformed pin fails closed: every request returns the
// pin error.
func PeerHTTPClient(skipVerify bool, pinnedSHA256 string, timeout time.Duration) *http.Client {
	cfg, err := PeerTLSConfig(skipVerify, pinnedSHA256)
	if err != nil {
		return &http.Client{Timeout: timeout, Transport: failTransport{err: err}}
	}
	if cfg == nil {
		return &http.Client{Timeout: timeout}
	}
	return &http.Client{Timeout: timeout, Transport: &http.Transport{TLSClientConfig: cfg}}
}

// ValidatePeerTLS normalizes pin (trimmed, lowercased, colons and spaces
// stripped) and requires it to be exactly 64 hex characters (a SHA-256)
// when non-empty. An empty pin is valid and yields an empty normalized
// result. Setting both a pin and skipVerify is rejected — the pin is the
// recommended way to trust a self-signed peer, so the two must not be
// mixed on one entry (same precedent as the compute node validation in
// internal/compute/node.go).
func ValidatePeerTLS(skipVerify bool, pin string) (normalized string, err error) {
	normalized = strings.ToLower(strings.NewReplacer(":", "", " ", "").Replace(strings.TrimSpace(pin)))
	if normalized == "" {
		return "", nil
	}
	if skipVerify {
		return "", errors.New("tls_cert_fingerprint and tls_skip_verify cannot both be set for the same peer — use the fingerprint pin instead of skipping verification")
	}
	if len(normalized) != 64 {
		return "", fmt.Errorf("tls_cert_fingerprint must be 64 hex chars (SHA-256), got %d", len(normalized))
	}
	if _, err := hex.DecodeString(normalized); err != nil {
		return "", errors.New("tls_cert_fingerprint must be hex (SHA-256)")
	}
	return normalized, nil
}

// TLSWarning returns "" when the peer is verified by CA chain or pinned
// to a leaf certificate fingerprint, and otherwise a one-line warning
// that the peer's certificate is not verified and that
// tls_cert_fingerprint is the safer option.
func TLSWarning(peerName string, skipVerify bool, pin string) string {
	if !skipVerify || strings.TrimSpace(pin) != "" {
		return ""
	}
	return fmt.Sprintf("%s: this peer's certificate is not verified (tls_skip_verify is set); set tls_cert_fingerprint to pin its leaf certificate instead", peerName)
}

type failTransport struct{ err error }

func (f failTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, f.err }
