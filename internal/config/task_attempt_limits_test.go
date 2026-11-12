package config

import "testing"

func TestTaskAttemptLimitsLoadFromYAML(t *testing.T) {
	cfg := loadYAML(t, "autonomous:\n  task_max_attempt_seconds: 5400\n  task_no_progress_seconds: 1800\n")
	if cfg.Autonomous.TaskMaxAttemptSeconds != 5400 {
		t.Fatalf("task_max_attempt_seconds = %d, want 5400", cfg.Autonomous.TaskMaxAttemptSeconds)
	}
	if cfg.Autonomous.TaskNoProgressSeconds != 1800 {
		t.Fatalf("task_no_progress_seconds = %d, want 1800", cfg.Autonomous.TaskNoProgressSeconds)
	}
}

func TestTaskAttemptLimitsDefaultOff(t *testing.T) {
	d := DefaultConfig().Autonomous
	if d.TaskMaxAttemptSeconds != 0 || d.TaskNoProgressSeconds != 0 {
		t.Fatalf("DefaultConfig limits = %d/%d, want 0/0", d.TaskMaxAttemptSeconds, d.TaskNoProgressSeconds)
	}
}

func TestTaskAttemptLimitsOmittedStayZero(t *testing.T) {
	cfg := loadYAML(t, "autonomous:\n  enabled: true\n")
	if cfg.Autonomous.TaskMaxAttemptSeconds != 0 || cfg.Autonomous.TaskNoProgressSeconds != 0 {
		t.Fatalf("omitted limits = %d/%d, want 0/0", cfg.Autonomous.TaskMaxAttemptSeconds, cfg.Autonomous.TaskNoProgressSeconds)
	}
}
