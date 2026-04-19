package tlsutil

import (
	"os"
	"path/filepath"
	"testing"
)

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
	Build(cfg)

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
