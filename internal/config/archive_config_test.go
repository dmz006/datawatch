package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestArchiveConfigDefaults(t *testing.T) {
	a := DefaultConfig().Autonomous.Archive
	if a.RetentionDays != 0 || a.Enabled != nil {
		t.Fatalf("default archive = %+v, want zero retention and unset enabled", a)
	}
	if a.EffectiveRetentionDays() != 30 {
		t.Fatalf("effective retention = %d, want 30", a.EffectiveRetentionDays())
	}
	if !a.EffectiveEnabled() {
		t.Fatal("archive must be enabled by default")
	}
}

func TestArchiveConfigZeroUsesDefault(t *testing.T) {
	cfg, err := loadProgressYAML(t, "autonomous:\n  archive:\n    retention_days: 0\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Autonomous.Archive.EffectiveRetentionDays(); got != 30 {
		t.Fatalf("retention_days 0 -> %d, want 30", got)
	}
}

func TestArchiveConfigNegativeRejected(t *testing.T) {
	_, err := loadProgressYAML(t, "autonomous:\n  archive:\n    retention_days: -3\n")
	if err == nil || !strings.Contains(err.Error(), "autonomous.archive.retention_days") {
		t.Fatalf("negative retention error = %v", err)
	}
	if err := (AutonomousArchiveConfig{RetentionDays: -1}).validate(); err == nil {
		t.Fatal("validate accepted a negative value")
	}
}

func TestArchiveConfigYAMLRoundTrip(t *testing.T) {
	off := false
	in := AutonomousArchiveConfig{Enabled: &off, RetentionDays: 14}
	raw, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out AutonomousArchiveConfig
	if err := yaml.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.RetentionDays != 14 || out.Enabled == nil || *out.Enabled || out.EffectiveEnabled() {
		t.Fatalf("round trip = %+v from\n%s", out, raw)
	}
	cfg, err := loadProgressYAML(t, "autonomous:\n  archive:\n    enabled: false\n    retention_days: 7\n")
	if err != nil {
		t.Fatal(err)
	}
	a := cfg.Autonomous.Archive
	if a.EffectiveEnabled() || a.EffectiveRetentionDays() != 7 {
		t.Fatalf("loaded archive = %+v", a)
	}
}
