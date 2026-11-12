package config

import "testing"

func TestJudgePromptFileThreshold_AbsentKeyYieldsDefault(t *testing.T) {
	if JudgePromptFileThresholdDefaultLines != 300 {
		t.Fatalf("JudgePromptFileThresholdDefaultLines = %d, want 300", JudgePromptFileThresholdDefaultLines)
	}
	cfg := loadYAML(t, "autonomous:\n  enabled: true\n")
	if got := cfg.Autonomous.JudgePromptFileThresholdLines; got != 300 {
		t.Fatalf("absent key: judge_prompt_file_threshold_lines = %d, want 300", got)
	}
	if got := DefaultConfig().Autonomous.JudgePromptFileThresholdLines; got != 300 {
		t.Fatalf("DefaultConfig judge_prompt_file_threshold_lines = %d, want 300", got)
	}
}

func TestJudgePromptFileThreshold_ExplicitZeroRoundTrips(t *testing.T) {
	cfg := loadYAML(t, "autonomous:\n  judge_prompt_file_threshold_lines: 0\n")
	if got := cfg.Autonomous.JudgePromptFileThresholdLines; got != 0 {
		t.Fatalf("explicit 0 loaded as %d, want 0", got)
	}
	back := saveAndReload(t, cfg)
	if got := back.Autonomous.JudgePromptFileThresholdLines; got != 0 {
		t.Fatalf("explicit 0 after save and load = %d, want 0", got)
	}
}

func TestJudgePromptFileThreshold_ExplicitValueRoundTrips(t *testing.T) {
	cfg := loadYAML(t, "autonomous:\n  judge_prompt_file_threshold_lines: 1200\n")
	if got := cfg.Autonomous.JudgePromptFileThresholdLines; got != 1200 {
		t.Fatalf("loaded %d, want 1200", got)
	}
	back := saveAndReload(t, cfg)
	if got := back.Autonomous.JudgePromptFileThresholdLines; got != 1200 {
		t.Fatalf("after save and load = %d, want 1200", got)
	}
}
