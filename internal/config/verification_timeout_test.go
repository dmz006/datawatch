package config

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestNormalizeVerificationTimeout_UnsetConfigDefaults(t *testing.T) {
	var cfg AutonomousConfig
	got := NormalizeVerificationTimeoutSeconds(cfg.VerificationTimeoutSeconds)
	if got != 1800 {
		t.Fatalf("unset config: got %d seconds, want 1800", got)
	}
}

func TestNormalizeVerificationTimeout_ZeroDefaults(t *testing.T) {
	if got := NormalizeVerificationTimeoutSeconds(0); got != 1800 {
		t.Fatalf("zero: got %d seconds, want 1800", got)
	}
}

func TestNormalizeVerificationTimeout_NegativeDefaults(t *testing.T) {
	if got := NormalizeVerificationTimeoutSeconds(-5); got != 1800 {
		t.Fatalf("negative -5: got %d seconds, want 1800", got)
	}
}

func TestNormalizeVerificationTimeout_ConfiguredValuePassesThrough(t *testing.T) {
	got := NormalizeVerificationTimeoutSeconds(900)
	if got != 900 {
		t.Fatalf("configured 900: got %d seconds, want 900", got)
	}
	if d := time.Duration(got) * time.Second; d != 15*time.Minute {
		t.Fatalf("duration: got %v, want 15m0s", d)
	}
}

func TestVerificationTimeout_YAMLRoundTripLandsInField(t *testing.T) {
	const doc = `
autonomous:
  verification_timeout_seconds: 900
`
	var parsed struct {
		Autonomous AutonomousConfig `yaml:"autonomous"`
	}
	if err := yaml.Unmarshal([]byte(doc), &parsed); err != nil {
		t.Fatalf("yaml unmarshal: %v", err)
	}
	if parsed.Autonomous.VerificationTimeoutSeconds != 900 {
		t.Fatalf("VerificationTimeoutSeconds: got %d, want 900", parsed.Autonomous.VerificationTimeoutSeconds)
	}
}
