package multiserver

import (
	"net/http"
	"time"

	"github.com/dmz006/datawatch/internal/compute"
)

// PeerHTTPClient returns an HTTP client for talking to a peer over TLS.
//
//   - pinnedSHA256 empty, skipVerify false: same as HTTPClient(false, timeout).
//   - pinnedSHA256 empty, skipVerify true:  same as HTTPClient(true, timeout).
//   - pinnedSHA256 set: the chain is not checked; the SHA-256 of the leaf
//     certificate must equal the pin (case-insensitive, colons and spaces
//     ignored). Pinning wins over skipVerify.
//
// Pinning reuses compute.NodeTLSConfig, the single fingerprint
// implementation shared with the Ollama node pool. Mismatch errors carry
// only the two fingerprints, never the peer URL. A malformed pin fails
// closed: every request returns the pin error.
//
// No per-peer fingerprint field exists in internal/config/config.go or the
// multiserver store (config.go only has the compute-node
// tls_cert_fingerprint), and none is added here; callers pass the pin in.
func PeerHTTPClient(skipVerify bool, pinnedSHA256 string, timeout time.Duration) *http.Client {
	if pinnedSHA256 == "" {
		return HTTPClient(skipVerify, timeout)
	}
	cfg, _, err := compute.NodeTLSConfig(&compute.Node{TLSCertFingerprint: pinnedSHA256})
	if err != nil {
		return &http.Client{Timeout: timeout, Transport: failTransport{err: err}}
	}
	return &http.Client{Timeout: timeout, Transport: &http.Transport{TLSClientConfig: cfg}}
}

type failTransport struct{ err error }

func (f failTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, f.err }
