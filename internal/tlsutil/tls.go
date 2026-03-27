// Package tlsutil provides TLS configuration helpers with post-quantum support
// and self-signed certificate generation for datawatch services.
package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Config holds TLS settings for a service.
type Config struct {
	// Enabled controls whether TLS is active.
	Enabled bool

	// CertFile and KeyFile are paths to PEM-encoded TLS certificate and key.
	// If empty and AutoGenerate is true, a self-signed cert is created.
	CertFile string
	KeyFile  string

	// AutoGenerate creates a self-signed cert in dataDir if CertFile/KeyFile are empty.
	AutoGenerate bool

	// DataDir is where auto-generated certs are stored (e.g. ~/.datawatch).
	DataDir string

	// Name is used as a subdirectory under DataDir/tls/ and as the cert CN.
	Name string

	// SANs are additional Subject Alternative Names (IPs or DNS names).
	SANs []string
}

// Build returns a *tls.Config configured for TLS 1.3 minimum with post-quantum
// key exchange enabled. In Go 1.23+, X25519Kyber768Draft00 hybrid PQC key
// exchange is negotiated automatically when CurvePreferences is nil.
//
// If AutoGenerate is set and no cert files are provided, a self-signed certificate
// is generated and persisted in DataDir/tls/Name/.
func Build(cfg Config) (*tls.Config, error) {
	certFile, keyFile := cfg.CertFile, cfg.KeyFile

	if (certFile == "" || keyFile == "") && cfg.AutoGenerate {
		var err error
		certFile, keyFile, err = ensureSelfSigned(cfg)
		if err != nil {
			return nil, fmt.Errorf("auto-generate cert: %w", err)
		}
	}

	if certFile == "" || keyFile == "" {
		return nil, fmt.Errorf("TLS cert and key are required (set tls_cert/tls_key or enable tls_auto_generate)")
	}

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load TLS cert %s: %w", certFile, err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		// TLS 1.3 minimum — enables post-quantum hybrid key exchange by default
		// (X25519Kyber768Draft00 in Go 1.23+, X25519MLKEM768 in Go 1.24+).
		MinVersion: tls.VersionTLS13,
		// Leave CurvePreferences nil so Go picks the best including PQC hybrids.
	}, nil
}

// ensureSelfSigned returns paths to an existing or newly-generated self-signed cert.
func ensureSelfSigned(cfg Config) (certFile, keyFile string, err error) {
	dir := filepath.Join(cfg.DataDir, "tls", cfg.Name)
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")

	// Re-use existing cert if both files are present.
	if _, err := os.Stat(certFile); err == nil {
		if _, err := os.Stat(keyFile); err == nil {
			return certFile, keyFile, nil
		}
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", "", fmt.Errorf("create tls dir: %w", err)
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generate key: %w", err)
	}

	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	cn := cfg.Name
	if cn == "" {
		cn = "datawatch"
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour), // 10 years
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,
		BasicConstraintsValid: true,
	}

	// Add localhost and 127.0.0.1 by default.
	tmpl.DNSNames = append([]string{"localhost"}, cfg.SANs...)
	tmpl.IPAddresses = append(tmpl.IPAddresses, net.ParseIP("127.0.0.1"), net.ParseIP("::1"))
	for _, san := range cfg.SANs {
		if ip := net.ParseIP(san); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, san)
		}
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return "", "", fmt.Errorf("create certificate: %w", err)
	}

	// Write cert.pem
	cf, err := os.OpenFile(certFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return "", "", fmt.Errorf("write cert: %w", err)
	}
	defer cf.Close()
	if err := pem.Encode(cf, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		return "", "", err
	}

	// Write key.pem
	privDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return "", "", fmt.Errorf("marshal key: %w", err)
	}
	kf, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return "", "", fmt.Errorf("write key: %w", err)
	}
	defer kf.Close()
	if err := pem.Encode(kf, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privDER}); err != nil {
		return "", "", err
	}

	return certFile, keyFile, nil
}
