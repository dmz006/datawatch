package compute

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
)

// PullHTTPClient builds the HTTP client used to pull a model onto the node.
// The timeout is zero because a pull can run for a very long time. When
// VerifyTLS is false the transport skips certificate verification; when it is
// true the transport verifies against the system roots. A plain http address
// is unaffected either way.
func PullHTTPClient(n *Node) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if n != nil && n.VerifyTLS {
		tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	} else {
		// The node is operator-registered on a private network and
		// verification is opt-in per node (verify_tls).
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402
	}
	return &http.Client{Timeout: 0, Transport: tr}
}

// WrapPullError names the node and the certificate problem when a pull failed
// certificate verification on a node with verify_tls on. Any other error, and
// any error on a node with verify_tls off, passes through unchanged.
func WrapPullError(n *Node, err error) error {
	if err == nil || n == nil || !n.VerifyTLS {
		return err
	}
	var unknownAuth x509.UnknownAuthorityError
	var certInvalid x509.CertificateInvalidError
	var hostname x509.HostnameError
	var verifyErr *tls.CertificateVerificationError
	if errors.As(err, &unknownAuth) || errors.As(err, &certInvalid) ||
		errors.As(err, &hostname) || errors.As(err, &verifyErr) {
		return fmt.Errorf("node %s: certificate verification failed (verify_tls is on): %w; trust the certificate or turn verify_tls off for this node", n.Name, err)
	}
	return err
}
