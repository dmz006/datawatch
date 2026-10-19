package config

import "testing"

func TestStaleTaskDefaultIs900(t *testing.T) {
	cfg := loadYAML(t, "autonomous:\n  enabled: true\n")
	if cfg.Autonomous.StaleTaskSeconds != 900 {
		t.Fatalf("stale_task_seconds = %d, want default 900", cfg.Autonomous.StaleTaskSeconds)
	}
	if got := DefaultConfig().Autonomous.StaleTaskSeconds; got != 900 {
		t.Fatalf("DefaultConfig stale_task_seconds = %d, want 900", got)
	}
}

func TestStaleTaskDefaultRespectsExplicitValue(t *testing.T) {
	cfg := loadYAML(t, "autonomous:\n  stale_task_seconds: 1200\n")
	if cfg.Autonomous.StaleTaskSeconds != 1200 {
		t.Fatalf("stale_task_seconds = %d, want explicit 1200", cfg.Autonomous.StaleTaskSeconds)
	}
}
