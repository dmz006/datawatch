package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnlandedAlertEffectiveAndValidate(t *testing.T) {
	if !UnlandedAlertEnabledEffective(nil) || UnlandedAlertEnabledEffective(BoolPtr(false)) {
		t.Fatal("enabled effective")
	}
	zero, five, neg := 0, 5, -1
	if UnlandedAlertThresholdEffective(nil) != 3 || UnlandedAlertThresholdEffective(&zero) != 0 || UnlandedAlertThresholdEffective(&five) != 5 {
		t.Fatal("threshold effective")
	}
	if ValidateUnlandedAlertThreshold(nil) != nil || ValidateUnlandedAlertThreshold(&zero) != nil {
		t.Fatal("valid refused")
	}
	if err := ValidateUnlandedAlertThreshold(&neg); err == nil || !strings.Contains(err.Error(), "unlanded_alert_threshold") {
		t.Fatalf("negative: %v", err)
	}
}

func TestUnlandedAlertConfigLoad(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) string {
		p := filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cfg, err := Load(write("autonomous:\n  unlanded_alert_enabled: false\n  unlanded_alert_threshold: 0\n"))
	if err != nil {
		t.Fatal(err)
	}
	if UnlandedAlertEnabledEffective(cfg.Autonomous.UnlandedAlertEnabled) || UnlandedAlertThresholdEffective(cfg.Autonomous.UnlandedAlertThreshold) != 0 {
		t.Fatalf("loaded = %v / %v", cfg.Autonomous.UnlandedAlertEnabled, cfg.Autonomous.UnlandedAlertThreshold)
	}
	if _, err := Load(write("autonomous:\n  unlanded_alert_threshold: -2\n")); err == nil {
		t.Fatal("negative threshold loaded")
	}
}
