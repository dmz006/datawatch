package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestVerifierRunCapDefaultIs2700(t *testing.T) {
	if VerifierRunCapDefaultSeconds != 2700 {
		t.Fatalf("VerifierRunCapDefaultSeconds = %d, want 2700", VerifierRunCapDefaultSeconds)
	}
	cfg := DefaultConfig()
	if got := cfg.Autonomous.VerifierMaxRunSeconds; got != 2700 {
		t.Fatalf("DefaultConfig Autonomous.VerifierMaxRunSeconds = %d, want 2700", got)
	}
}

func TestVerifierRunCapZeroAccepted(t *testing.T) {
	if err := ValidateVerifierRunCapSeconds(0); err != nil {
		t.Fatalf("ValidateVerifierRunCapSeconds(0) = %v, want nil", err)
	}
	if got := VerifierRunCapSeconds(0); got != 0 {
		t.Fatalf("VerifierRunCapSeconds(0) = %d, want 0 (no cap)", got)
	}
}

func TestVerifierRunCapNegativeRefused(t *testing.T) {
	err := ValidateVerifierRunCapSeconds(-1)
	if err == nil {
		t.Fatal("ValidateVerifierRunCapSeconds(-1) = nil, want error")
	}
	if !strings.Contains(err.Error(), "autonomous.verifier_max_run_seconds") {
		t.Fatalf("error %q does not name the key", err.Error())
	}
}

func TestVerifierRunCapYAMLRoundTripKeepsZero(t *testing.T) {
	in := AutonomousConfig{VerifierMaxRunSeconds: 0}
	b, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), "verifier_max_run_seconds: 0") {
		t.Fatalf("marshalled YAML lacks explicit 0 key:\n%s", b)
	}
	var out AutonomousConfig
	if err := yaml.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.VerifierMaxRunSeconds != 0 {
		t.Fatalf("round-trip VerifierMaxRunSeconds = %d, want 0", out.VerifierMaxRunSeconds)
	}
}
