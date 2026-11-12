package config

import (
	"os"
	"path/filepath"
	"testing"
)

func loadCreateSearchYAML(t *testing.T, yaml string) *Config {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

// Unset keys read the defaults: the pre-check searches in shadow mode with a
// 0.5 threshold.
func TestOverlapConfigDefaultsRespectDefaultsWhenUnset(t *testing.T) {
	cfg := loadCreateSearchYAML(t, "autonomous:\n  automation:\n    idle_alert_minutes: 38\n")
	if got := cfg.Autonomous.Automation.EffectiveCreateSearchMode(); got != "shadow" {
		t.Errorf("EffectiveCreateSearchMode = %q, want shadow", got)
	}
	if got := cfg.Autonomous.Automation.EffectiveCreateSearchThreshold(); got != 0.5 {
		t.Errorf("EffectiveCreateSearchThreshold = %v, want 0.5", got)
	}
}

// Explicit valid values are accepted on load.
func TestOverlapConfigLoadAcceptsValidModeAndThreshold(t *testing.T) {
	for mode, want := range map[string]string{"off": "off", "shadow": "shadow", "on": "on"} {
		y := "autonomous:\n  automation:\n    create_search_mode: " + mode + "\n    create_search_threshold: 0.8\n"
		cfg := loadCreateSearchYAML(t, y)
		if got := cfg.Autonomous.Automation.EffectiveCreateSearchMode(); got != want {
			t.Errorf("EffectiveCreateSearchMode(%s) = %q, want %q", mode, got, want)
		}
		if got := cfg.Autonomous.Automation.EffectiveCreateSearchThreshold(); got != 0.8 {
			t.Errorf("EffectiveCreateSearchThreshold = %v, want 0.8", got)
		}
	}
	// Both boundary values are valid: 1 is included, small positives work.
	cfg := loadCreateSearchYAML(t, "autonomous:\n  automation:\n    create_search_threshold: 1\n")
	if got := cfg.Autonomous.Automation.EffectiveCreateSearchThreshold(); got != 1 {
		t.Errorf("threshold 1 = %v, want 1", got)
	}
	cfg = loadCreateSearchYAML(t, "autonomous:\n  automation:\n    create_search_threshold: 0.01\n")
	if got := cfg.Autonomous.Automation.EffectiveCreateSearchThreshold(); got != 0.01 {
		t.Errorf("threshold 0.01 = %v, want 0.01", got)
	}
}

// Bogus modes and out-of-range, zero and non-finite thresholds are rejected
// on config load.
func TestOverlapConfigLoadRefusesBogusValues(t *testing.T) {
	cases := []string{
		"autonomous:\n  automation:\n    create_search_mode: bogus\n",
		"autonomous:\n  automation:\n    create_search_mode: Shadow\n",
		"autonomous:\n  automation:\n    create_search_threshold: 0\n",
		"autonomous:\n  automation:\n    create_search_threshold: -0.2\n",
		"autonomous:\n  automation:\n    create_search_threshold: 1.5\n",
		"autonomous:\n  automation:\n    create_search_mode: on\n    create_search_threshold: 0\n",
	}
	for _, y := range cases {
		p := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(p, []byte(y), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if _, err := Load(p); err == nil {
			t.Errorf("Load(%q): want refusal error, got nil", y)
		}
	}
}

// The exported validators behave the same in isolation.
func TestOverlapConfigValidatorsStandalone(t *testing.T) {
	if err := ValidateCreateSearchMode("shadow"); err != nil {
		t.Errorf("ValidateCreateSearchMode(shadow) = %v, want nil", err)
	}
	if err := ValidateCreateSearchMode("bogus"); err == nil {
		t.Error("ValidateCreateSearchMode(bogus) = nil, want error")
	}
	if err := ValidateCreateSearchThreshold(0.5); err != nil {
		t.Errorf("ValidateCreateSearchThreshold(0.5) = %v, want nil", err)
	}
	if err := ValidateCreateSearchThreshold(0); err == nil {
		t.Error("ValidateCreateSearchThreshold(0) = nil, want error")
	}
	if err := ValidateCreateSearchThreshold(1.01); err == nil {
		t.Error("ValidateCreateSearchThreshold(1.01) = nil, want error")
	}
}

// DefaultConfig ships the effective defaults: shadow with 0.5.
func TestOverlapConfigDefaultConfigShipsDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if got := cfg.Autonomous.Automation.EffectiveCreateSearchMode(); got != "shadow" {
		t.Errorf("DefaultConfig mode = %q, want shadow", got)
	}
	if got := cfg.Autonomous.Automation.EffectiveCreateSearchThreshold(); got != 0.5 {
		t.Errorf("DefaultConfig threshold = %v, want 0.5", got)
	}
}
