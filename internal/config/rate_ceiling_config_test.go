package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRateCeilingConfig_DefaultsAreZero(t *testing.T) {
	a := loadYAML(t, "autonomous:\n  enabled: true\n").Autonomous
	if a.CapacityRateRequests != 0 || a.CapacityRateTokens != 0 || a.CapacityRateWindowSeconds != 0 {
		t.Fatalf("rate ceiling defaults = %d/%d/%d, want 0/0/0", a.CapacityRateRequests, a.CapacityRateTokens, a.CapacityRateWindowSeconds)
	}
	d := DefaultConfig().Autonomous
	if d.CapacityRateRequests != 0 || d.CapacityRateTokens != 0 || d.CapacityRateWindowSeconds != 0 {
		t.Fatalf("DefaultConfig rate ceiling = %d/%d/%d, want 0/0/0", d.CapacityRateRequests, d.CapacityRateTokens, d.CapacityRateWindowSeconds)
	}
}

func TestRateCeilingConfig_SaveLoadRoundTrip(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Autonomous.CapacityRateRequests = 60
	cfg.Autonomous.CapacityRateTokens = 2000000
	cfg.Autonomous.CapacityRateWindowSeconds = 1800
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(cfg, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	a := got.Autonomous
	if a.CapacityRateRequests != 60 || a.CapacityRateTokens != 2000000 || a.CapacityRateWindowSeconds != 1800 {
		t.Fatalf("round trip = %d/%d/%d, want 60/2000000/1800", a.CapacityRateRequests, a.CapacityRateTokens, a.CapacityRateWindowSeconds)
	}
}

func TestRateCeilingConfig_NegativeRejected(t *testing.T) {
	for _, key := range []string{"capacity_rate_requests", "capacity_rate_tokens", "capacity_rate_window_seconds"} {
		p := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(p, []byte("autonomous:\n  "+key+": -1\n"), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := Load(p)
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("Load with %s: -1 error = %v, want one naming the key", key, err)
		}
	}
}
