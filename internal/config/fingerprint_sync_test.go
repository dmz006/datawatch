package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// TestFingerprintSyncYAMLSeed — the per-peer resource-fingerprint-exchange
// opt-in must YAML-seed false when the key is absent and true when it is
// present. Absent defaults to false: opt-in per peer is the operator
// decision recorded in
// docs/plans/2026-10-14-concurrency-and-budget-design.md (open question 10).
func TestFingerprintSyncYAMLSeed(t *testing.T) {
	absent := []byte(`
servers:
  - name: off-peer
    url: http://off-peer.example.com:8080
    token: tok
    enabled: true
  - name: on-peer
    url: http://on-peer.example.com:8080
    token: tok
    enabled: true
    fingerprint_sync: true
`)
	var c struct {
		Servers []RemoteServerConfig `yaml:"servers"`
	}
	if err := yaml.Unmarshal(absent, &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(c.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(c.Servers))
	}
	if c.Servers[0].FingerprintSync {
		t.Errorf("server without the fingerprint_sync key must seed FingerprintSync=false, got true")
	}
	if !c.Servers[1].FingerprintSync {
		t.Errorf("server with fingerprint_sync: true must seed FingerprintSync=true, got false")
	}
}
