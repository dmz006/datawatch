package acme

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// selfSignedPEMForTest builds a minimal self-signed cert PEM block for
// parseLeafCert tests — no network, no real CA involved.
func selfSignedPEMForTest(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test.example"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(90 * 24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestAtomicWriteFile_WritesAndOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fullchain.pem")

	if err := atomicWriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "first" {
		t.Fatalf("got %q, want %q", got, "first")
	}

	if err := atomicWriteFile(path, []byte("second"), 0600); err != nil {
		t.Fatalf("atomicWriteFile overwrite: %v", err)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after overwrite: %v", err)
	}
	if string(got) != "second" {
		t.Fatalf("got %q, want %q", got, "second")
	}

	// No leftover .tmp file.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected no leftover .tmp file, stat err = %v", err)
	}
}

func TestParseLeafCert_FindsCertificateBlockAmongChain(t *testing.T) {
	// Build a minimal self-signed leaf to get a real CERTIFICATE PEM
	// block, then prepend an unrelated PEM block (simulating a chain
	// where the leaf isn't the first block) to confirm parseLeafCert
	// skips non-CERTIFICATE blocks correctly.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	leafPEM := selfSignedPEMForTest(t, key)

	fakeOtherBlock := []byte("-----BEGIN PRIVATE KEY-----\nbm90LWEtY2VydA==\n-----END PRIVATE KEY-----\n")
	combined := append(append([]byte{}, fakeOtherBlock...), leafPEM...)

	cert, err := parseLeafCert(combined)
	if err != nil {
		t.Fatalf("parseLeafCert: %v", err)
	}
	if cert == nil {
		t.Fatal("expected a parsed certificate")
	}
	if time.Until(cert.NotAfter) <= 0 {
		t.Fatal("expected a cert that has not yet expired (test fixture bug)")
	}
}

// TestLoadExistingCertStatus_FindsAndParsesAFreshCert is the regression
// test for the live-test restart-loop bug (2026-10-06): NewManager used
// to always start every domain's status at Issued:false, so Apply's own
// restart (after a successful issue) looked, on the next boot, exactly
// like "never issued" — triggering another immediate issue, another
// restart, forever. loadExistingCertStatus must find a fresh, valid cert
// already on disk and report it as issued, with the real expiry.
func TestLoadExistingCertStatus_FindsAndParsesAFreshCert(t *testing.T) {
	dataDir := t.TempDir()
	domain := "spaceportsouth.dmzs.com"
	certDir := filepath.Join(dataDir, "tls", "acme", domain)
	if err := os.MkdirAll(certDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	leafPEM := selfSignedPEMForTest(t, key)
	if err := os.WriteFile(filepath.Join(certDir, "fullchain.pem"), leafPEM, 0600); err != nil {
		t.Fatalf("write fullchain.pem: %v", err)
	}

	issued, notAfter := loadExistingCertStatus(dataDir, []string{domain})
	if !issued {
		t.Fatal("expected an already-issued cert on disk to be found")
	}
	if time.Until(notAfter) <= 0 {
		t.Fatal("expected a NotAfter in the future for a freshly-written test cert")
	}
}

func TestLoadExistingCertStatus_NoFileMeansNotIssued(t *testing.T) {
	issued, notAfter := loadExistingCertStatus(t.TempDir(), []string{"never-issued.example"})
	if issued {
		t.Fatal("expected issued=false when no cert file exists yet")
	}
	if !notAfter.IsZero() {
		t.Fatalf("expected a zero NotAfter, got %v", notAfter)
	}
}
