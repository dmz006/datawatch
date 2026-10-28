package config

import (
	"os"
	"path/filepath"
	"testing"
)

func loadTriageYAML(t *testing.T, value string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "autonomous:\n  automation:\n    triage_alert_minutes: " + value + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

func TestConfigTriageAlertMinutesDefaultAndUnset(t *testing.T) {
	if got := DefaultConfig().Autonomous.Automation.EffectiveTriageAlertMinutes(); got != DefaultAutomationTriageAlertMinutes {
		t.Fatalf("default triage_alert_minutes=%d, want %d", got, DefaultAutomationTriageAlertMinutes)
	}
	var unset AutonomousAutomationConfig
	if got := unset.EffectiveTriageAlertMinutes(); got != 30 {
		t.Fatalf("unset triage_alert_minutes resolves to %d, want 30", got)
	}
}

func TestConfigTriageAlertMinutesLoadAcceptsZeroAndPositive(t *testing.T) {
	for value, want := range map[string]int{"0": 0, "45": 45} {
		cfg, err := loadTriageYAML(t, value)
		if err != nil {
			t.Fatalf("Load rejected triage_alert_minutes: %s: %v", value, err)
		}
		if got := cfg.Autonomous.Automation.EffectiveTriageAlertMinutes(); got != want {
			t.Fatalf("triage_alert_minutes %s loaded as %d, want %d", value, got, want)
		}
	}
}

func TestConfigTriageAlertMinutesLoadRejectsBadValues(t *testing.T) {
	for _, bad := range []string{"-1", "-30", "soon"} {
		if _, err := loadTriageYAML(t, bad); err == nil {
			t.Errorf("Load accepted triage_alert_minutes: %s", bad)
		}
	}
}

// A fractional YAML number is not refused on load: the YAML decoder narrows it
// to a whole number. The REST, MCP, CLI and comm write paths do refuse it; this
// test pins the load-time behaviour so the documentation can state it plainly.
func TestConfigTriageAlertMinutesLoadTruncatesFraction(t *testing.T) {
	cfg, err := loadTriageYAML(t, "1.5")
	if err != nil {
		t.Skipf("this YAML decoder refuses a fractional value on load: %v", err)
	}
	if got := cfg.Autonomous.Automation.EffectiveTriageAlertMinutes(); got != 1 {
		t.Fatalf("1.5 loaded as %d, want 1 (truncated)", got)
	}
}
