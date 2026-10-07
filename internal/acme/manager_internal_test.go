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
	"strings"
	"testing"
	"time"

	"github.com/go-acme/lego/v4/certificate"

	"github.com/dmz006/datawatch/internal/config"
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

// TestApplyCertificate_UpdatesAndSavesServerTLSPaths is the regression
// test for the live-test bug (2026-10-06): the PEM was being written to
// disk and Status() correctly reported the cert as issued, but the TLS
// listener kept serving the old self-signed cert because applyCertificate
// never actually pointed server.tls_cert/tls_key at the new file (or
// mcp.tls_cert/tls_key, when configured to share the cert) and never
// persisted the change — so even a restart had nothing new to pick up.
// TestApplyCertificate_NeverPersistsResolvedDNS01Secret is a security
// regression test for BL397 Phase 2 (DNS-01): config.Save does a plain
// yaml.Marshal with no redaction. main.go's global secret-ref resolution
// pass runs once at daemon startup and mutates fullCfg.Acme.DNS01.
// TokenSecret in place -- from PLAINTEXT to that point on, in memory.
// If applyCertificate ever saved fullCfg as-is, it would permanently
// write that plaintext DNS provider token into config.yaml. This test
// simulates exactly that already-resolved in-memory state and asserts
// the file written to disk still has the original "${secret:...}"
// reference, never the plaintext.
func TestApplyCertificate_NeverPersistsResolvedDNS01Secret(t *testing.T) {
	dataDir := t.TempDir()
	cfgPath := filepath.Join(dataDir, "config.yaml")
	fullCfg := config.DefaultConfig()
	fullCfg.DataDir = dataDir
	const secretRef = "${secret:cf-zone-edit-token}"
	const plaintextToken = "cf-real-token-abc123-do-not-leak"
	fullCfg.Acme.DNS01.TokenSecret = secretRef
	if err := config.Save(fullCfg, cfgPath); err != nil {
		t.Fatalf("seed config.Save: %v", err)
	}

	// Simulate main.go's global ResolveConfig having already run: the
	// IN-MEMORY struct now holds the plaintext, exactly as it would by
	// the time a real NewManager call constructs a Manager.
	fullCfg.Acme.DNS01.TokenSecret = plaintextToken

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	leafPEM := selfSignedPEMForTest(t, key)
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	m := &Manager{
		cfg: config.AcmeConfig{
			Domains: []string{"spaceportsouth.dmzs.com"},
			Method:  "dns01",
		},
		dataDir:             dataDir,
		fullCfg:             fullCfg,
		cfgPath:             cfgPath,
		dns01TokenSecretRef: secretRef, // what NewManager would have captured fresh from disk
		status:              map[string]*DomainStatus{"spaceportsouth.dmzs.com": {Domain: "spaceportsouth.dmzs.com"}},
	}

	if err := m.applyCertificate(&certificate.Resource{Certificate: leafPEM, PrivateKey: keyPEM}); err != nil {
		t.Fatalf("applyCertificate: %v", err)
	}

	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if strings.Contains(string(raw), plaintextToken) {
		t.Fatal("SECURITY REGRESSION: the plaintext DNS-01 token was written to config.yaml on disk")
	}

	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload saved config: %v", err)
	}
	if reloaded.Acme.DNS01.TokenSecret != secretRef {
		t.Fatalf("saved token_secret = %q, want the original reference %q preserved", reloaded.Acme.DNS01.TokenSecret, secretRef)
	}
}

func TestApplyCertificate_UpdatesAndSavesServerTLSPaths(t *testing.T) {
	dataDir := t.TempDir()
	cfgPath := filepath.Join(dataDir, "config.yaml")
	fullCfg := config.DefaultConfig()
	fullCfg.DataDir = dataDir
	if err := config.Save(fullCfg, cfgPath); err != nil {
		t.Fatalf("seed config.Save: %v", err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	leafPEM := selfSignedPEMForTest(t, key)
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	restarted := false
	m := &Manager{
		cfg: config.AcmeConfig{
			Domains: []string{"spaceportsouth.dmzs.com"},
			Apply:   config.AcmeApplyConfig{UpdateMCPCert: true},
		},
		dataDir:   dataDir,
		fullCfg:   fullCfg,
		cfgPath:   cfgPath,
		status:    map[string]*DomainStatus{"spaceportsouth.dmzs.com": {Domain: "spaceportsouth.dmzs.com"}},
		restartFn: func() { restarted = true },
	}

	if err := m.applyCertificate(&certificate.Resource{Certificate: leafPEM, PrivateKey: keyPEM}); err != nil {
		t.Fatalf("applyCertificate: %v", err)
	}

	wantCert := filepath.Join(dataDir, "tls", "acme", "spaceportsouth.dmzs.com", "fullchain.pem")
	wantKey := filepath.Join(dataDir, "tls", "acme", "spaceportsouth.dmzs.com", "privkey.pem")
	if fullCfg.Server.TLSCert != wantCert {
		t.Errorf("Server.TLSCert = %q, want %q", fullCfg.Server.TLSCert, wantCert)
	}
	if fullCfg.Server.TLSKey != wantKey {
		t.Errorf("Server.TLSKey = %q, want %q", fullCfg.Server.TLSKey, wantKey)
	}
	if fullCfg.MCP.TLSCert != wantCert {
		t.Errorf("MCP.TLSCert = %q, want %q (UpdateMCPCert was true)", fullCfg.MCP.TLSCert, wantCert)
	}

	// Must be persisted to disk, not just updated in memory — a restart
	// re-reads from cfgPath.
	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload saved config: %v", err)
	}
	if reloaded.Server.TLSCert != wantCert {
		t.Errorf("saved config Server.TLSCert = %q, want %q — the fix must persist, not just mutate in memory", reloaded.Server.TLSCert, wantCert)
	}

	// restartFn is called asynchronously (500ms delay, see the
	// HTTP-response-race fix) — give it a moment.
	time.Sleep(600 * time.Millisecond)
	if !restarted {
		t.Error("expected restartFn to have been called after a successful apply")
	}
}

// TestApplyCertificate_HotSwap_SkipsRestartOnUnchangedPath is the
// regression test for BL397 Phase 3: when apply.hot_swap is true and the
// cert path isn't changing (the normal renewal case — always the same
// {data_dir}/tls/acme/<name>/ location), applyCertificate must NOT call
// restartFn. The listener's GetCertificate callback (internal/tlsutil)
// is what actually picks up the new PEM, with no daemon restart.
func TestApplyCertificate_HotSwap_SkipsRestartOnUnchangedPath(t *testing.T) {
	dataDir := t.TempDir()
	cfgPath := filepath.Join(dataDir, "config.yaml")
	fullCfg := config.DefaultConfig()
	fullCfg.DataDir = dataDir
	// Pre-seed the config as if a PRIOR apply already pointed these at
	// the ACME path — simulating the second-and-later renewal case, not
	// the first-ever switch (which always needs one restart regardless
	// of hot_swap — see the sibling test below).
	certPath := filepath.Join(dataDir, "tls", "acme", "spaceportsouth.dmzs.com", "fullchain.pem")
	keyPath := filepath.Join(dataDir, "tls", "acme", "spaceportsouth.dmzs.com", "privkey.pem")
	fullCfg.Server.TLSCert = certPath
	fullCfg.Server.TLSKey = keyPath
	if err := config.Save(fullCfg, cfgPath); err != nil {
		t.Fatalf("seed config.Save: %v", err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	leafPEM := selfSignedPEMForTest(t, key)
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	restarted := false
	m := &Manager{
		cfg: config.AcmeConfig{
			Domains: []string{"spaceportsouth.dmzs.com"},
			Apply:   config.AcmeApplyConfig{HotSwap: true},
		},
		dataDir:   dataDir,
		fullCfg:   fullCfg,
		cfgPath:   cfgPath,
		status:    map[string]*DomainStatus{"spaceportsouth.dmzs.com": {Domain: "spaceportsouth.dmzs.com"}},
		restartFn: func() { restarted = true },
	}

	if err := m.applyCertificate(&certificate.Resource{Certificate: leafPEM, PrivateKey: keyPEM}); err != nil {
		t.Fatalf("applyCertificate: %v", err)
	}

	time.Sleep(600 * time.Millisecond)
	if restarted {
		t.Error("expected restartFn NOT to be called when hot_swap is true and the cert path is unchanged")
	}
}

// TestApplyCertificate_HotSwap_StillRestartsOnFirstPathChange confirms
// the first-ever apply (switching to ACME from self-signed/custom, or
// the very first ACME issue) still restarts even with hot_swap: true —
// a running listener can't hot-reload a path it was never watching.
func TestApplyCertificate_HotSwap_StillRestartsOnFirstPathChange(t *testing.T) {
	dataDir := t.TempDir()
	cfgPath := filepath.Join(dataDir, "config.yaml")
	fullCfg := config.DefaultConfig() // Server.TLSCert is empty -- not yet pointed at ACME
	fullCfg.DataDir = dataDir
	if err := config.Save(fullCfg, cfgPath); err != nil {
		t.Fatalf("seed config.Save: %v", err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	leafPEM := selfSignedPEMForTest(t, key)
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	restarted := false
	m := &Manager{
		cfg: config.AcmeConfig{
			Domains: []string{"spaceportsouth.dmzs.com"},
			Apply:   config.AcmeApplyConfig{HotSwap: true},
		},
		dataDir:   dataDir,
		fullCfg:   fullCfg,
		cfgPath:   cfgPath,
		status:    map[string]*DomainStatus{"spaceportsouth.dmzs.com": {Domain: "spaceportsouth.dmzs.com"}},
		restartFn: func() { restarted = true },
	}

	if err := m.applyCertificate(&certificate.Resource{Certificate: leafPEM, PrivateKey: keyPEM}); err != nil {
		t.Fatalf("applyCertificate: %v", err)
	}

	time.Sleep(600 * time.Millisecond)
	if !restarted {
		t.Error("expected restartFn to be called on the first apply even with hot_swap: true (path was changing)")
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
