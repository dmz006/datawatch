package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPeerSilenceDefaultIsFive(t *testing.T) {
	if got := DefaultConfig().Observer.Peers.SilentAlertMinutes; got != 5 {
		t.Fatalf("default SilentAlertMinutes=%d want 5", got)
	}
}

func TestPeerSilenceYAMLRoundTrip(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Observer.Peers.SilentAlertMinutes = 12
	out, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "silent_alert_minutes: 12") {
		t.Fatalf("yaml missing key:\n%s", out)
	}
	back := DefaultConfig()
	if err := yaml.Unmarshal(out, back); err != nil {
		t.Fatal(err)
	}
	if back.Observer.Peers.SilentAlertMinutes != 12 {
		t.Fatalf("round trip=%d want 12", back.Observer.Peers.SilentAlertMinutes)
	}
}

func TestPeerSilenceZeroKept(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Observer.Peers.SilentAlertMinutes = 0
	out, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, out, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Observer.Peers.SilentAlertMinutes != 0 {
		t.Fatalf("zero not kept: got %d", got.Observer.Peers.SilentAlertMinutes)
	}
}
