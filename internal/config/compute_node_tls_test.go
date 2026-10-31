package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const computeNodeTLSFingerprint = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

func TestLoad_ComputeNodeTLSSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := `
compute_nodes:
  - name: private-ca
    kind: ollama
    address: https://gpu1:11434
    tls_ca_file: /etc/datawatch/ca.pem
  - name: pinned
    kind: ollama
    address: https://gpu2:11434
    tls_cert_fingerprint: ` + computeNodeTLSFingerprint + `
  - name: skip
    kind: ollama
    address: https://gpu3:11434
    tls_insecure_skip_verify: true
  - name: plain
    kind: ollama
    address: http://gpu4:11434
`
	if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.ComputeNodes) != 4 {
		t.Fatalf("ComputeNodes = %d entries, want 4", len(cfg.ComputeNodes))
	}
	byName := map[string]ComputeNodeYAML{}
	for _, n := range cfg.ComputeNodes {
		byName[n.Name] = n
	}
	if got := byName["private-ca"].TLSCAFile; got != "/etc/datawatch/ca.pem" {
		t.Errorf("private-ca tls_ca_file = %q", got)
	}
	if got := byName["pinned"].TLSCertFingerprint; got != computeNodeTLSFingerprint {
		t.Errorf("pinned tls_cert_fingerprint = %q", got)
	}
	if !byName["skip"].TLSInsecureSkipVerify {
		t.Error("skip tls_insecure_skip_verify = false, want true")
	}
	plain := byName["plain"]
	if plain.TLSCAFile != "" || plain.TLSCertFingerprint != "" || plain.TLSInsecureSkipVerify {
		t.Errorf("plain node TLS = %+v, want all defaults", plain)
	}
}

func TestSave_ComputeNodeTLSSettingsRoundTrip(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ComputeNodes = []ComputeNodeYAML{
		{Name: "both", Kind: "ollama", Address: "https://gpu1:11434", TLSCAFile: "/etc/datawatch/ca.pem", TLSCertFingerprint: computeNodeTLSFingerprint},
		{Name: "plain", Kind: "ollama", Address: "http://gpu2:11434"},
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(raw), "tls_ca_file"); got != 1 {
		t.Errorf("tls_ca_file appears %d times in saved YAML, want 1 (omitempty on the plain node)", got)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.ComputeNodes) != 2 {
		t.Fatalf("ComputeNodes = %d entries, want 2", len(loaded.ComputeNodes))
	}
	both, plain := loaded.ComputeNodes[0], loaded.ComputeNodes[1]
	if both.TLSCAFile != "/etc/datawatch/ca.pem" || both.TLSCertFingerprint != computeNodeTLSFingerprint {
		t.Errorf("round-tripped TLS = ca %q fp %q", both.TLSCAFile, both.TLSCertFingerprint)
	}
	if plain.TLSCAFile != "" || plain.TLSCertFingerprint != "" {
		t.Errorf("plain node gained TLS settings: %+v", plain)
	}
}
