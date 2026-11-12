package config

import (
	"path/filepath"
	"testing"
)

func TestResumeMaxAge_DefaultIs3600(t *testing.T) {
	// Truly empty config file: everything must come from defaults.
	empty := loadYAML(t, "")
	if empty.Autonomous.ResumeMaxAgeSeconds != 3600 {
		t.Fatalf("empty config: resume_max_age_seconds = %d, want default 3600", empty.Autonomous.ResumeMaxAgeSeconds)
	}
	// Absent key under a populated autonomous section: Load merges
	// YAML over DefaultConfig(), so 3600 must survive (no
	// applyDefaults normalisation touches this field).
	cfg := loadYAML(t, "autonomous:\n  enabled: true\n")
	if cfg.Autonomous.ResumeMaxAgeSeconds != 3600 {
		t.Fatalf("resume_max_age_seconds = %d, want default 3600", cfg.Autonomous.ResumeMaxAgeSeconds)
	}
	if got := DefaultConfig().Autonomous.ResumeMaxAgeSeconds; got != 3600 {
		t.Fatalf("DefaultConfig resume_max_age_seconds = %d, want 3600", got)
	}
}

func TestResumeMaxAge_ZeroIsMeaningfulAndRoundTrips(t *testing.T) {
	cfg := loadYAML(t, "autonomous:\n  resume_max_age_seconds: 0\n")
	if cfg.Autonomous.ResumeMaxAgeSeconds != 0 {
		t.Fatalf("resume_max_age_seconds = %d, want explicit 0 (never resume) preserved", cfg.Autonomous.ResumeMaxAgeSeconds)
	}
	p := filepath.Join(t.TempDir(), "saved.yaml")
	if err := Save(cfg, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	again, err := Load(p)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if again.Autonomous.ResumeMaxAgeSeconds != 0 {
		t.Fatalf("after round trip resume_max_age_seconds = %d, want 0", again.Autonomous.ResumeMaxAgeSeconds)
	}
}

func TestResumeMaxAge_NegativeRoundTripsUnchanged(t *testing.T) {
	cfg := loadYAML(t, "autonomous:\n  resume_max_age_seconds: -1\n")
	if cfg.Autonomous.ResumeMaxAgeSeconds != -1 {
		t.Fatalf("resume_max_age_seconds = %d, want explicit -1 preserved", cfg.Autonomous.ResumeMaxAgeSeconds)
	}
	p := filepath.Join(t.TempDir(), "saved.yaml")
	if err := Save(cfg, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	again, err := Load(p)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if again.Autonomous.ResumeMaxAgeSeconds != -1 {
		t.Fatalf("after round trip resume_max_age_seconds = %d, want -1", again.Autonomous.ResumeMaxAgeSeconds)
	}
}

func TestResumeMaxAge_ExplicitValueRoundTrips(t *testing.T) {
	cfg := loadYAML(t, "autonomous:\n  resume_max_age_seconds: 7200\n")
	if cfg.Autonomous.ResumeMaxAgeSeconds != 7200 {
		t.Fatalf("resume_max_age_seconds = %d, want 7200", cfg.Autonomous.ResumeMaxAgeSeconds)
	}
	p := filepath.Join(t.TempDir(), "saved.yaml")
	if err := Save(cfg, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	again, err := Load(p)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if again.Autonomous.ResumeMaxAgeSeconds != 7200 {
		t.Fatalf("after round trip resume_max_age_seconds = %d, want 7200", again.Autonomous.ResumeMaxAgeSeconds)
	}
}
