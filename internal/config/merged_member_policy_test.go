package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadMergedMemberPolicyYAML(t *testing.T, body string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("autonomous:\n  automation:\n"+body), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

func TestMergedMemberPolicyDefaultIsCompleted(t *testing.T) {
	a := DefaultConfig().Autonomous.Automation
	if got := a.EffectiveMergedMemberPolicy(); got != MergedMemberPolicyCompleted {
		t.Fatalf("default merged_member_policy=%q, want %q", got, MergedMemberPolicyCompleted)
	}
	if MergedMemberPolicyCompleted != "completed" || MergedMemberPolicyManual != "manual" || MergedMemberPolicyBlock != "block" {
		t.Fatalf("constants drifted: completed=%q manual=%q block=%q",
			MergedMemberPolicyCompleted, MergedMemberPolicyManual, MergedMemberPolicyBlock)
	}
}

func TestMergedMemberPolicyUnsetReadsCompleted(t *testing.T) {
	var unset AutonomousAutomationConfig
	if got := unset.EffectiveMergedMemberPolicy(); got != MergedMemberPolicyCompleted {
		t.Fatalf("unset merged_member_policy reads %q, want completed", got)
	}
	// A config file that sets neither key keeps the default after Load.
	cfg, err := loadMergedMemberPolicyYAML(t, "    idle_alert_minutes: 5\n")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Autonomous.Automation.EffectiveMergedMemberPolicy(); got != MergedMemberPolicyCompleted {
		t.Fatalf("loaded without the key: %q, want completed", got)
	}
}

func TestMergedMemberPolicyValidValuesRoundTrip(t *testing.T) {
	for _, ok := range []string{MergedMemberPolicyCompleted, MergedMemberPolicyManual, MergedMemberPolicyBlock} {
		cfg, err := loadMergedMemberPolicyYAML(t, "    merged_member_policy: "+ok+"\n")
		if err != nil {
			t.Fatalf("Load rejected merged_member_policy: %s: %v", ok, err)
		}
		if got := cfg.Autonomous.Automation.EffectiveMergedMemberPolicy(); got != ok {
			t.Fatalf("merged_member_policy %s loaded as %q", ok, got)
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
		if got := back.Autonomous.Automation.EffectiveMergedMemberPolicy(); got != ok {
			t.Fatalf("merged_member_policy %s after save and reload = %q", ok, got)
		}
	}
}

func TestMergedMemberPolicyRejectsUnknownValue(t *testing.T) {
	for _, bad := range []string{"enabled", "COMPLETED", "true", "auto"} {
		if err := ValidateMergedMemberPolicy(bad); err == nil {
			t.Errorf("ValidateMergedMemberPolicy accepted %q", bad)
		}
		_, err := loadMergedMemberPolicyYAML(t, "    merged_member_policy: "+bad+"\n")
		if err == nil {
			t.Errorf("Load accepted merged_member_policy: %s", bad)
		} else if !strings.Contains(err.Error(), "merged_member_policy") {
			t.Errorf("Load error for %q does not name the key: %v", bad, err)
		}
	}
}
