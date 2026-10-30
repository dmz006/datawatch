package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadMergePendingYAML(t *testing.T, body string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("autonomous:\n  automation:\n"+body), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

func TestMergePendingConfigDefaultsAreOffAnd30(t *testing.T) {
	a := DefaultConfig().Autonomous.Automation
	if got := a.EffectiveMergePendingMode(); got != MergePendingModeOff {
		t.Fatalf("default merge_pending_mode=%q, want %q", got, MergePendingModeOff)
	}
	if got := a.EffectiveMergePendingAlertMinutes(); got != 30 {
		t.Fatalf("default merge_pending_alert_minutes=%d, want 30", got)
	}
	if DefaultAutomationMergePendingAlertMinutes != DefaultAutomationTriageAlertMinutes {
		t.Fatalf("merge-pending default %d must match the shared default %d",
			DefaultAutomationMergePendingAlertMinutes, DefaultAutomationTriageAlertMinutes)
	}
}

func TestMergePendingConfigUnsetMinutesReads30(t *testing.T) {
	var unset AutonomousAutomationConfig
	if got := unset.EffectiveMergePendingAlertMinutes(); got != 30 {
		t.Fatalf("unset merge_pending_alert_minutes reads %d, want 30", got)
	}
	if got := unset.EffectiveMergePendingMode(); got != MergePendingModeOff {
		t.Fatalf("unset merge_pending_mode reads %q, want off", got)
	}
	// A config file that sets neither key keeps the defaults after Load.
	cfg, err := loadMergePendingYAML(t, "    idle_alert_minutes: 5\n")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Autonomous.Automation.EffectiveMergePendingAlertMinutes(); got != 30 {
		t.Fatalf("loaded without the key: %d, want 30", got)
	}
}

func TestMergePendingConfigExplicitZeroSurvives(t *testing.T) {
	cfg, err := loadMergePendingYAML(t, "    merge_pending_alert_minutes: 0\n")
	if err != nil {
		t.Fatalf("Load rejected an explicit 0: %v", err)
	}
	if got := cfg.Autonomous.Automation.EffectiveMergePendingAlertMinutes(); got != 0 {
		t.Fatalf("explicit 0 loaded as %d, want 0 (alert off)", got)
	}
	// It must also survive a save and reload.
	path := filepath.Join(t.TempDir(), "saved.yaml")
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := back.Autonomous.Automation.EffectiveMergePendingAlertMinutes(); got != 0 {
		t.Fatalf("explicit 0 after save and reload = %d, want 0", got)
	}
}

func TestMergePendingConfigRejectsUnknownMode(t *testing.T) {
	for _, bad := range []string{"enabled", "ON", "true", "shadow-only"} {
		if err := ValidateMergePendingMode(bad); err == nil {
			t.Errorf("ValidateMergePendingMode accepted %q", bad)
		}
		_, err := loadMergePendingYAML(t, "    merge_pending_mode: "+bad+"\n")
		if err == nil {
			t.Errorf("Load accepted merge_pending_mode: %s", bad)
		} else if !strings.Contains(err.Error(), "merge_pending_mode") {
			t.Errorf("Load error for %q does not name the key: %v", bad, err)
		}
	}
	for _, ok := range []string{MergePendingModeOff, MergePendingModeShadow, MergePendingModeOn} {
		cfg, err := loadMergePendingYAML(t, "    merge_pending_mode: "+ok+"\n")
		if err != nil {
			t.Fatalf("Load rejected merge_pending_mode: %s: %v", ok, err)
		}
		if got := cfg.Autonomous.Automation.EffectiveMergePendingMode(); got != ok {
			t.Fatalf("merge_pending_mode %s loaded as %q", ok, got)
		}
	}
}

func TestMergePendingConfigRejectsNegativeMinutes(t *testing.T) {
	for _, bad := range []string{"-1", "-30", "soon"} {
		if _, err := loadMergePendingYAML(t, "    merge_pending_alert_minutes: "+bad+"\n"); err == nil {
			t.Errorf("Load accepted merge_pending_alert_minutes: %s", bad)
		}
	}
	cfg, err := loadMergePendingYAML(t, "    merge_pending_alert_minutes: 45\n")
	if err != nil {
		t.Fatalf("Load rejected 45: %v", err)
	}
	if got := cfg.Autonomous.Automation.EffectiveMergePendingAlertMinutes(); got != 45 {
		t.Fatalf("45 loaded as %d", got)
	}
}
