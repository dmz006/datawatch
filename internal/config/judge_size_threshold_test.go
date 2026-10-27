package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// saveAndReload writes cfg with Save and loads it back with Load.
func saveAndReload(t *testing.T, cfg *Config) *Config {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(cfg, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load(p)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	return out
}

func TestJudgeSizeThreshold_AbsentKeyYieldsDefault(t *testing.T) {
	if JudgeSizeThresholdDefaultLines != 400 {
		t.Fatalf("JudgeSizeThresholdDefaultLines = %d, want 400", JudgeSizeThresholdDefaultLines)
	}
	cfg := loadYAML(t, "autonomous:\n  enabled: true\n")
	if got := cfg.Autonomous.JudgeSizeThresholdLines; got != 400 {
		t.Fatalf("absent key: judge_size_threshold_lines = %d, want 400", got)
	}
	if got := DefaultConfig().Autonomous.JudgeSizeThresholdLines; got != 400 {
		t.Fatalf("DefaultConfig judge_size_threshold_lines = %d, want 400", got)
	}
}

func TestJudgeSizeThreshold_ExplicitZeroRoundTrips(t *testing.T) {
	cfg := loadYAML(t, "autonomous:\n  judge_size_threshold_lines: 0\n")
	if got := cfg.Autonomous.JudgeSizeThresholdLines; got != 0 {
		t.Fatalf("explicit 0 loaded as %d, want 0", got)
	}
	back := saveAndReload(t, cfg)
	if got := back.Autonomous.JudgeSizeThresholdLines; got != 0 {
		t.Fatalf("explicit 0 after save and load = %d, want 0", got)
	}
	if JudgeSizeRuleEnabled(back.Autonomous.JudgeSizeThresholdLines) {
		t.Fatal("size rule enabled for threshold 0, want off")
	}
}

func TestJudgeSizeThreshold_PositiveValueRoundTrips(t *testing.T) {
	cfg := loadYAML(t, "autonomous:\n  judge_size_threshold_lines: 750\n")
	if got := cfg.Autonomous.JudgeSizeThresholdLines; got != 750 {
		t.Fatalf("loaded %d, want 750", got)
	}
	back := saveAndReload(t, cfg)
	if got := back.Autonomous.JudgeSizeThresholdLines; got != 750 {
		t.Fatalf("after save and load = %d, want 750", got)
	}
	if !JudgeSizeRuleEnabled(back.Autonomous.JudgeSizeThresholdLines) {
		t.Fatal("size rule off for threshold 750, want on")
	}
}

func TestJudgeSizeThreshold_NegativeValueFailsValidation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte("autonomous:\n  judge_size_threshold_lines: -1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(p)
	if err == nil {
		t.Fatal("Load accepted a negative judge_size_threshold_lines, want error")
	}
	if !strings.Contains(err.Error(), "judge_size_threshold_lines") {
		t.Fatalf("error %q does not name judge_size_threshold_lines", err)
	}
}
