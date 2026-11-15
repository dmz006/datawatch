package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A servers entry carrying tls_cert_fingerprint must load from YAML into
// RemoteServerConfig.TLSCertFingerprint, and entries without the key
// must keep the empty value.
func TestLoadRemoteServerTLSCertFingerprint(t *testing.T) {
	fp := strings.Repeat("a1", 32)
	yaml := "servers:\n" +
		"  - name: pinned-peer\n" +
		"    url: https://pinned.example.com:8080\n" +
		"    enabled: true\n" +
		"    tls_cert_fingerprint: " + fp + "\n" +
		"  - name: plain-peer\n" +
		"    url: https://plain.example.com:8080\n" +
		"    enabled: true\n"

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Servers) != 2 {
		t.Fatalf("len(Servers) = %d, want 2", len(cfg.Servers))
	}
	if cfg.Servers[0].Name != "pinned-peer" {
		t.Fatalf("Servers[0].Name = %q, want pinned-peer", cfg.Servers[0].Name)
	}
	if cfg.Servers[0].TLSCertFingerprint != fp {
		t.Errorf("TLSCertFingerprint = %q, want %q", cfg.Servers[0].TLSCertFingerprint, fp)
	}
	if cfg.Servers[0].TLSSkipVerify {
		t.Error("TLSSkipVerify must stay false when only the pin is set")
	}
	if cfg.Servers[1].TLSCertFingerprint != "" {
		t.Errorf("plain peer TLSCertFingerprint = %q, want empty", cfg.Servers[1].TLSCertFingerprint)
	}
}
