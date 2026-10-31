package compute

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// TLSMode names how a probe of a node's HTTPS endpoint verifies the server.
type TLSMode string

const (
	// TLSModeVerified — system roots only.
	TLSModeVerified TLSMode = "verified"
	// TLSModeCAFile — system roots plus the node's tls_ca_file.
	TLSModeCAFile TLSMode = "ca_file"
	// TLSModePinned — leaf certificate must match tls_cert_fingerprint;
	// the chain and hostname are not checked.
	TLSModePinned TLSMode = "pinned"
	// TLSModeCAFilePinned — chain must verify against system roots plus
	// tls_ca_file AND the leaf must match tls_cert_fingerprint.
	TLSModeCAFilePinned TLSMode = "ca_file_pinned"
	// TLSModeVerificationOff — tls_insecure_skip_verify: nothing is checked.
	TLSModeVerificationOff TLSMode = "verification_off"
)

// NodeTLSConfig builds the client TLS config for probing node n and reports
// which verification mode it implements. It is the only place that sets
// InsecureSkipVerify for node probes, so callers contain none.
//
// The base trust store is x509.SystemCertPool, which Go already extends from
// SSL_CERT_FILE and SSL_CERT_DIR; the daemon has no CA bundle setting of its
// own (secrets.vault.tls_ca_file belongs to Vault only).
func NodeTLSConfig(n *Node) (cfg *tls.Config, mode TLSMode, err error) {
	if n == nil {
		return nil, "", errors.New("compute node: nil node")
	}
	caFile := strings.TrimSpace(n.TLSCAFile)
	var want string
	if n.TLSCertFingerprint != "" {
		want, err = normalizeCertFingerprint(n.TLSCertFingerprint)
		if err != nil {
			return nil, "", err
		}
	}
	if n.TLSInsecureSkipVerify {
		if caFile != "" || want != "" {
			return nil, "", errors.New("compute node: tls_insecure_skip_verify cannot be combined with tls_ca_file or tls_cert_fingerprint (choose one: verify, pin or skip)")
		}
		return &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, // #nosec G402 -- explicit per-node opt-out (tls_insecure_skip_verify)
		}, TLSModeVerificationOff, nil
	}

	var pool *x509.CertPool
	if caFile != "" {
		pool, err = x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		pemData, rerr := os.ReadFile(caFile) // #nosec G304 -- operator-configured CA path
		if rerr != nil {
			return nil, "", fmt.Errorf("compute node: read tls_ca_file %q: %w", caFile, rerr)
		}
		if !pool.AppendCertsFromPEM(pemData) {
			return nil, "", fmt.Errorf("compute node: tls_ca_file %q contains no valid PEM certs", caFile)
		}
	}

	switch {
	case want == "":
		cfg = &tls.Config{MinVersion: tls.VersionTLS12}
		if pool == nil {
			return cfg, TLSModeVerified, nil
		}
		cfg.RootCAs = pool
		return cfg, TLSModeCAFile, nil

	case pool == nil:
		return &tls.Config{
			MinVersion: tls.VersionTLS12,
			// The leaf fingerprint is the sole trust signal; it is checked
			// in VerifyPeerCertificate, so the default chain check is off.
			InsecureSkipVerify: true, // #nosec G402 -- pinned via VerifyPeerCertificate (tls_cert_fingerprint)
			VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
				if len(rawCerts) == 0 {
					return errors.New("no peer certificates presented")
				}
				return checkLeafFingerprint(rawCerts[0], want)
			},
		}, TLSModePinned, nil

	default:
		// Chain must verify against the pool; the Go verifier is replaced by
		// VerifyConnection so the leaf pin is enforced on the same handshake.
		return &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, // #nosec G402 -- chain verified manually in VerifyConnection (tls_ca_file + tls_cert_fingerprint)
			VerifyConnection: func(cs tls.ConnectionState) error {
				if len(cs.PeerCertificates) == 0 {
					return errors.New("no peer certificates presented")
				}
				opts := x509.VerifyOptions{
					Roots:         pool,
					DNSName:       cs.ServerName,
					Intermediates: x509.NewCertPool(),
				}
				for _, c := range cs.PeerCertificates[1:] {
					opts.Intermediates.AddCert(c)
				}
				if _, verr := cs.PeerCertificates[0].Verify(opts); verr != nil {
					return fmt.Errorf("verify peer certificate: %w", verr)
				}
				return checkLeafFingerprint(cs.PeerCertificates[0].Raw, want)
			},
		}, TLSModeCAFilePinned, nil
	}
}

func checkLeafFingerprint(raw []byte, want string) error {
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("node cert fingerprint mismatch: got %s want %s", got, want)
	}
	return nil
}

// normalizeCertFingerprint returns fp as 64 lowercase hex chars, dropping
// colons and spaces, or an error naming tls_cert_fingerprint.
func normalizeCertFingerprint(fp string) (string, error) {
	out := strings.ToLower(strings.NewReplacer(":", "", " ", "").Replace(strings.TrimSpace(fp)))
	if len(out) != 64 {
		return "", fmt.Errorf("compute node: tls_cert_fingerprint must be 64 hex chars (SHA-256), got %d", len(out))
	}
	if _, err := hex.DecodeString(out); err != nil {
		return "", errors.New("compute node: tls_cert_fingerprint must be hex (SHA-256)")
	}
	return out, nil
}
