package config

import (
	"os"
	"testing"
)

// loadCleanupYAML writes the YAML text into a temporary file, loads it
// through the production path (so the applyDefaults + validateLoaded chain
// runs exactly as on daemon boot), and returns the loaded config.
func loadCleanupYAML(t *testing.T, body string) *Config {
	t.Helper()
	file := t.TempDir() + "/cleanup.yaml"
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(file)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

// The Effective* accessors must resolve unset and invalid values to the
// defaults on a bare zero-valued config as well as a loaded one — the
// daemon can hand the config to the autonomous manager before any YAML has
// been parsed.
func TestCleanupConfigDefaults(t *testing.T) {
	for name, cfg := range map[string]AutonomousAutomationConfig{
		"zero_value":     AutonomousAutomationConfig{},
		"default_loaded": DefaultConfig().Autonomous.Automation,
		"partial_loaded": loadCleanupYAML(t, "autonomous:\n  enabled: true\n  automation:\n    idle_alert_minutes: 5\n").Autonomous.Automation,
	} {
		t.Run(name, func(t *testing.T) {
			if got := cfg.EffectiveCleanupMode(); got != CleanupModeReport {
				t.Errorf("EffectiveCleanupMode = %q; want %q (default)", got, CleanupModeReport)
			}
			if got := cfg.EffectiveCleanupRetentionHours(); got != DefaultAutomationCleanupRetentionHours {
				t.Errorf("EffectiveCleanupRetentionHours = %d; want %d (default)", got, DefaultAutomationCleanupRetentionHours)
			}
			if got := cfg.EffectiveCleanupMaxRemovalsPerPass(); got != DefaultAutomationCleanupMaxRemovalsPerPass {
				t.Errorf("EffectiveCleanupMaxRemovalsPerPass = %d; want %d (default)", got, DefaultAutomationCleanupMaxRemovalsPerPass)
			}
			if got := cfg.EffectiveDiskAlertPercent(); got != DefaultAutomationDiskAlertPercent {
				t.Errorf("EffectiveDiskAlertPercent = %d; want %d (default)", got, DefaultAutomationDiskAlertPercent)
			}
		})
	}
}

func TestCleanupConfigUnsetReadsDefaults(t *testing.T) {
	cfg := loadCleanupYAML(t, `
autonomous:
  enabled: true
  automation:
    idle_alert_minutes: 5
    triage_alert_minutes: 30
`)
	a := cfg.Autonomous.Automation
	if a.CleanupMode != "" {
		t.Errorf("CleanupMode = %q; want unset (empty)", a.CleanupMode)
	}
	if a.CleanupRetentionHours != 0 || a.CleanupMaxRemovalsPerPass != 0 || a.DiskAlertPercent != 0 {
		t.Errorf("unset numeric keys are %+v; want zero values", a)
	}
	if got := a.EffectiveCleanupMode(); got != "report" {
		t.Errorf("EffectiveCleanupMode = %q; want report (default)", got)
	}
	if got := a.EffectiveCleanupRetentionHours(); got != 24 {
		t.Errorf("EffectiveCleanupRetentionHours = %d; want 24 (default)", got)
	}
	if got := a.EffectiveCleanupMaxRemovalsPerPass(); got != 10 {
		t.Errorf("EffectiveCleanupMaxRemovalsPerPass = %d; want 10 (default)", got)
	}
	if got := a.EffectiveDiskAlertPercent(); got != 85 {
		t.Errorf("EffectiveDiskAlertPercent = %d; want 85 (default)", got)
	}
}

func TestCleanupConfigExplicitValuesAreReadBack(t *testing.T) {
	cfg := loadCleanupYAML(t, `
autonomous:
  automation:
    idle_alert_minutes: 30
    cleanup_mode: off
    cleanup_retention_hours: 48
    cleanup_max_removals_per_pass: 5
    disk_alert_percent: 70
`)
	a := cfg.Autonomous.Automation
	if a.CleanupMode != "off" || a.CleanupRetentionHours != 48 || a.CleanupMaxRemovalsPerPass != 5 || a.DiskAlertPercent != 70 {
		t.Fatalf("raw = %+v; want mode off, retention 48, max 5, percent 70", a)
	}
	if got := a.EffectiveCleanupMode(); got != "off" {
		t.Errorf("EffectiveCleanupMode = %q; want off", got)
	}
	if got := a.EffectiveCleanupRetentionHours(); got != 48 {
		t.Errorf("EffectiveCleanupRetentionHours = %d; want 48", got)
	}
	if got := a.EffectiveCleanupMaxRemovalsPerPass(); got != 5 {
		t.Errorf("EffectiveCleanupMaxRemovalsPerPass = %d; want 5", got)
	}
	if got := a.EffectiveDiskAlertPercent(); got != 70 {
		t.Errorf("EffectiveDiskAlertPercent = %d; want 70", got)
	}
}

func TestCleanupConfigOnModeIsReadBack(t *testing.T) {
	cfg := loadCleanupYAML(t, `
autonomous:
  automation:
    idle_alert_minutes: 30
    cleanup_mode: on
`)
	a := cfg.Autonomous.Automation
	if got := a.EffectiveCleanupMode(); got != "on" {
		t.Errorf("EffectiveCleanupMode = %q; want on (explicit value)", got)
	}
}

// An unknown cleanup_mode word is NOT refused: unlike merge_pending_mode
// or create_search_mode, an unrecognized value loads and every read
// resolves it to report.
func TestCleanupConfigUnknownModeReadsReport(t *testing.T) {
	for _, word := range []string{"enabled", "TRUE", "ON", "cleanup", " delete "} {
		cfg := loadCleanupYAML(t, "autonomous:\n  automation:\n    idle_alert_minutes: 30\n    cleanup_mode: "+word+"\n")
		a := cfg.Autonomous.Automation
		if got := a.EffectiveCleanupMode(); got != "report" {
			t.Errorf("cleanup_mode %q: EffectiveCleanupMode = %q; want report (unknown values read as the default)", word, got)
		}
	}
}

// Zero and negative values are not refused on load: they read the defaults
// through the Effective* accessors.
func TestCleanupConfigZeroAndNegativeReadDefault(t *testing.T) {
	cfg := loadCleanupYAML(t, `
autonomous:
  automation:
    idle_alert_minutes: 30
    cleanup_retention_hours: 0
    cleanup_max_removals_per_pass: -5
`)
	a := cfg.Autonomous.Automation
	if a.CleanupRetentionHours != 0 || a.CleanupMaxRemovalsPerPass != -5 {
		t.Fatalf("raw = %+v; want the written 0 and -5", a)
	}
	if got := a.EffectiveCleanupRetentionHours(); got != 24 {
		t.Errorf("EffectiveCleanupRetentionHours = %d; want 24 (zero reads the default)", got)
	}
	if got := a.EffectiveCleanupMaxRemovalsPerPass(); got != 10 {
		t.Errorf("EffectiveCleanupMaxRemovalsPerPass = %d; want 10 (negative reads the default)", got)
	}
}

// disk_alert_percent is clamped to 1..100 on read, with 0 (unset) reading
// the 85 default.
func TestCleanupConfigDiskAlertPercentClamped(t *testing.T) {
	cases := []struct {
		write string
		yaml  string
		want  int
	}{
		{"unset zero reads 85", "", 85},
		{"negative clamps to the minimum 1", "-20", 1},
		{"the minimum 1 is kept", "1", 1},
		{"above the maximum 100 clamps to 100", "130", 100},
		{"the maximum 100 is kept", "100", 100},
		{"a value in range is kept", "90", 90},
	}
	for _, c := range cases {
		t.Run(c.write, func(t *testing.T) {
			body := "autonomous:\n  automation:\n    idle_alert_minutes: 30\n"
			if c.yaml != "" {
				body += "    disk_alert_percent: " + c.yaml + "\n"
			}
			cfg := loadCleanupYAML(t, body)
			if got := cfg.Autonomous.Automation.EffectiveDiskAlertPercent(); got != c.want {
				t.Errorf("EffectiveDiskAlertPercent = %d; want %d", got, c.want)
			}
		})
	}
}

// A written value must round-trip a Save/Load pair with the same raw value
// — the REST config-write path merges through the manager's config and
// writes it back, so an operator's explicit setting must not be reset by
// the accessors or by the save.
func TestCleanupConfigYAMLRoundTrip(t *testing.T) {
	dir := t.TempDir()
	file := dir + "/cleanup-roundtrip.yaml"
	initial := `
autonomous:
  enabled: true
  automation:
    idle_alert_minutes: 90
    cleanup_mode: on
    cleanup_retention_hours: 48
    cleanup_max_removals_per_pass: 5
    disk_alert_percent: 60
`
	if err := os.WriteFile(file, []byte(initial), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(file)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.Autonomous.Automation.CleanupMode = "on"
	cfg.Autonomous.Automation.CleanupRetentionHours = 48
	cfg.Autonomous.Automation.CleanupMaxRemovalsPerPass = 5
	cfg.Autonomous.Automation.DiskAlertPercent = 60
	if err := Save(cfg, file); err != nil {
		t.Fatalf("Save: %v", err)
	}
	cfg2, err := Load(file)
	if err != nil {
		t.Fatalf("Load again: %v", err)
	}
	a := cfg2.Autonomous.Automation
	if a.CleanupMode != "on" {
		t.Errorf("mode after round trip = %q; want on", a.CleanupMode)
	}
	if a.CleanupRetentionHours != 48 {
		t.Errorf("retention after round trip = %d; want 48", a.CleanupRetentionHours)
	}
	if a.CleanupMaxRemovalsPerPass != 5 {
		t.Errorf("max removals after round trip = %d; want 5 (not the %d default)", a.CleanupMaxRemovalsPerPass, DefaultAutomationCleanupMaxRemovalsPerPass)
	}
	if a.DiskAlertPercent != 60 {
		t.Errorf("percent after round trip = %d; want 60 (not the %d default)", a.DiskAlertPercent, DefaultAutomationDiskAlertPercent)
	}
	if got := a.EffectiveCleanupMode(); got != "on" {
		t.Errorf("effective mode after round trip = %q; want on", got)
	}
}
