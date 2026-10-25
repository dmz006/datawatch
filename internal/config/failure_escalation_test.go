package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadEscalationYAML(t *testing.T, body string) (*Config, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(p)
}

func TestFailureEscalationConfigDefaults(t *testing.T) {
	fe := DefaultConfig().Autonomous.FailureEscalation
	if !fe.Enabled || fe.MaxFailures != 3 || len(fe.Steps) != 0 {
		t.Fatalf("default = %+v, want enabled, 3 failures, no steps", fe)
	}

	// A config with no failure_escalation block loads with the defaults.
	cfg, err := loadEscalationYAML(t, "autonomous:\n  enabled: true\n")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Autonomous.FailureEscalation; !got.Enabled || got.MaxFailures != 3 {
		t.Fatalf("loaded default = %+v", got)
	}
}

func TestFailureEscalationConfigYAMLLoad(t *testing.T) {
	cfg, err := loadEscalationYAML(t, `
autonomous:
  failure_escalation:
    max_failures: 4
    steps:
      - backend: pi-prompt
        model: qwen3.8:27b
        attempts: 2
      - backend: claude-code
        model: claude-opus-5-5
        effort: high
`)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	fe := cfg.Autonomous.FailureEscalation
	// enabled keeps its seeded default when the key is absent.
	if !fe.Enabled || fe.MaxFailures != 4 || len(fe.Steps) != 2 {
		t.Fatalf("loaded = %+v", fe)
	}
	if s := fe.Steps[0]; s.Backend != "pi-prompt" || s.Model != "qwen3.8:27b" || s.Attempts != 2 {
		t.Fatalf("step 0 = %+v", s)
	}
	if s := fe.Steps[1]; s.Backend != "claude-code" || s.Effort != "high" || s.Attempts != 0 {
		t.Fatalf("step 1 = %+v", s)
	}

	off, err := loadEscalationYAML(t, "autonomous:\n  failure_escalation:\n    enabled: false\n")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := off.Autonomous.FailureEscalation; got.Enabled || got.MaxFailures != 3 {
		t.Fatalf("enabled:false load = %+v (max_failures should still default to 3)", got)
	}

	// A max_failures of 0 reads as the default.
	zero, err := loadEscalationYAML(t, "autonomous:\n  failure_escalation:\n    max_failures: 0\n")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := zero.Autonomous.FailureEscalation.MaxFailures; got != 3 {
		t.Fatalf("max_failures 0 read as %d, want 3", got)
	}
}

func TestFailureEscalationConfigSaveRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	cfg := DefaultConfig()
	cfg.Autonomous.FailureEscalation = FailureEscalationConfig{
		Enabled: false, MaxFailures: 5,
		Steps: []FailureEscalationStepConfig{{Backend: "claude-code", Model: "claude-opus-5-5", Attempts: 2}},
	}
	if err := Save(cfg, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, _ := os.ReadFile(p)
	if !strings.Contains(string(raw), "failure_escalation") {
		t.Fatalf("saved YAML missing failure_escalation:\n%s", raw)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := loaded.Autonomous.FailureEscalation
	if got.Enabled || got.MaxFailures != 5 || len(got.Steps) != 1 || got.Steps[0].Attempts != 2 {
		t.Fatalf("round trip = %+v", got)
	}
}

func TestFailureEscalationConfigRejectsInvalid(t *testing.T) {
	cases := []struct{ name, yaml, want string }{
		{"negative max_failures", "autonomous:\n  failure_escalation:\n    max_failures: -1\n", "autonomous.failure_escalation.max_failures"},
		{"negative attempts", "autonomous:\n  failure_escalation:\n    steps:\n      - backend: claude-code\n        attempts: -2\n", "autonomous.failure_escalation.steps[0].attempts"},
		{"too many steps", "autonomous:\n  failure_escalation:\n    steps: [{}, {}, {}, {}, {}, {}, {}, {}, {}]\n", "autonomous.failure_escalation.steps"},
		{"unknown key", "autonomous:\n  failure_escalation:\n    max_failure: 4\n", "autonomous.failure_escalation.max_failure"},
		{"unknown step key", "autonomous:\n  failure_escalation:\n    steps:\n      - backend: claude-code\n        mdoel: x\n", "autonomous.failure_escalation.steps[0].mdoel"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := loadEscalationYAML(t, c.yaml)
			if err == nil {
				t.Fatal("Load succeeded, want error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not name %q", err, c.want)
			}
		})
	}

	if err := (FailureEscalationConfig{MaxFailures: -3}).Validate(); err == nil {
		t.Fatal("Validate accepted a negative max_failures")
	}
	if err := DefaultFailureEscalationConfig().Validate(); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
}
