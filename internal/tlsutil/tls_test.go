package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeTestCert writes a minimal self-signed cert/key pair with the given
// serial number (so two calls produce distinguishably different certs),
// for BL397 Phase 3 hot-reload tests.
func writeTestCert(t *testing.T, certPath, keyPath string, serial int64) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: "reload-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		t.Fatalf("write key: %v", err)
	}
}

func certSerial(t *testing.T, cert *tls.Certificate) int64 {
	t.Helper()
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	return leaf.SerialNumber.Int64()
}

func TestBuild_AutoGenerate(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Enabled:      true,
		AutoGenerate: true,
		DataDir:      dir,
		Name:         "test",
	}
	tlsCfg, err := Build(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if tlsCfg == nil {
		t.Fatal("expected non-nil tls.Config")
	}

	// Cert files should be created
	certPath := filepath.Join(dir, "tls", "test", "cert.pem")
	keyPath := filepath.Join(dir, "tls", "test", "key.pem")
	if _, err := os.Stat(certPath); err != nil {
		t.Errorf("cert file not created: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Errorf("key file not created: %v", err)
	}
}

func TestBuild_Disabled(t *testing.T) {
	cfg := Config{Enabled: false}
	tlsCfg, err := Build(cfg)
	// When disabled, may return nil config or error depending on cert presence
	_ = err
	if cfg.Enabled == false && tlsCfg != nil {
		t.Error("expected nil config when disabled")
	}
}

func TestBuild_CustomCert(t *testing.T) {
	dir := t.TempDir()
	// First generate a cert
	cfg := Config{Enabled: true, AutoGenerate: true, DataDir: dir, Name: "gen"}
	_, _ = Build(cfg)

	certPath := filepath.Join(dir, "tls", "gen", "cert.pem")
	keyPath := filepath.Join(dir, "tls", "gen", "key.pem")

	// Now use the generated cert as custom
	cfg2 := Config{
		Enabled:  true,
		CertFile: certPath,
		KeyFile:  keyPath,
	}
	tlsCfg, err := Build(cfg2)
	if err != nil {
		t.Fatal(err)
	}
	if tlsCfg == nil {
		t.Fatal("expected non-nil config with custom cert")
	}
}

func TestBuild_MissingCert(t *testing.T) {
	cfg := Config{
		Enabled:  true,
		CertFile: "/nonexistent/cert.pem",
		KeyFile:  "/nonexistent/key.pem",
	}
	_, err := Build(cfg)
	if err == nil {
		t.Error("expected error for missing cert files")
	}
}

func TestBuild_SANs(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Enabled:      true,
		AutoGenerate: true,
		DataDir:      dir,
		Name:         "santest",
		SANs:         []string{"myhost.local", "203.0.113.100"},
	}
	tlsCfg, err := Build(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if tlsCfg == nil {
		t.Fatal("expected non-nil config")
	}
}

// TestBuild_GetCertificate_ReloadsOnMtimeChange is the core BL397 Phase 3
// behavior: a tls.Config built by Build must pick up a cert rotation at
// the SAME path (e.g. an ACME renewal rewriting the same fullchain.pem)
// on the next handshake, with no daemon restart.
func TestBuild_GetCertificate_ReloadsOnMtimeChange(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	writeTestCert(t, certPath, keyPath, 1)

	tlsCfg, err := Build(Config{Enabled: true, CertFile: certPath, KeyFile: keyPath})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if tlsCfg.GetCertificate == nil {
		t.Fatal("expected GetCertificate to be set (not a static Certificates slice)")
	}

	cert1, err := tlsCfg.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate (initial): %v", err)
	}
	if got := certSerial(t, cert1); got != 1 {
		t.Fatalf("initial cert serial = %d, want 1", got)
	}

	// Rewrite at the SAME path with a different serial, and force the
	// mtime forward explicitly -- some filesystems have mtime resolution
	// coarser than this test's wall-clock runtime, so a bare rewrite
	// isn't reliably "later" without this.
	writeTestCert(t, certPath, keyPath, 2)
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(certPath, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	cert2, err := tlsCfg.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate (after rotation): %v", err)
	}
	if got := certSerial(t, cert2); got != 2 {
		t.Fatalf("after rotation, cert serial = %d, want 2 — hot-reload did not pick up the new cert", got)
	}
}

// TestBuild_GetCertificate_KeepsServingLastGoodOnLoadError confirms a
// transient load failure (e.g. caught mid-write during a renewal) falls
// back to the last known-good cert instead of failing every handshake.
func TestBuild_GetCertificate_KeepsServingLastGoodOnLoadError(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	writeTestCert(t, certPath, keyPath, 1)

	tlsCfg, err := Build(Config{Enabled: true, CertFile: certPath, KeyFile: keyPath})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, err := tlsCfg.GetCertificate(nil); err != nil {
		t.Fatalf("GetCertificate (initial): %v", err)
	}

	// Corrupt the cert file and bump mtime forward, simulating a caught-
	// mid-write race.
	future := time.Now().Add(time.Hour)
	if err := os.WriteFile(certPath, []byte("not a valid cert"), 0600); err != nil {
		t.Fatalf("corrupt cert: %v", err)
	}
	if err := os.Chtimes(certPath, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	cert, err := tlsCfg.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate after a corrupt rewrite should keep serving the last good cert, got error: %v", err)
	}
	if got := certSerial(t, cert); got != 1 {
		t.Fatalf("expected to keep serving serial 1 (last good) after a failed reload, got %d", got)
	}
}
