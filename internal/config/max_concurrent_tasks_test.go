package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func loadYAML(t *testing.T, body string) *Config {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func TestAutonomousMaxConcurrentTasks_DefaultsToOne(t *testing.T) {
	cfg := loadYAML(t, "autonomous:\n  enabled: true\n")
	if cfg.Autonomous.MaxConcurrentTasks != 1 {
		t.Fatalf("max_concurrent_tasks = %d, want default 1", cfg.Autonomous.MaxConcurrentTasks)
	}
	if got := DefaultConfig().Autonomous.MaxConcurrentTasks; got != 1 {
		t.Fatalf("DefaultConfig max_concurrent_tasks = %d, want 1", got)
	}
}

func TestAutonomousMaxConcurrentTasks_SetAndRoundTrip(t *testing.T) {
	cfg := loadYAML(t, "autonomous:\n  max_concurrent_tasks: 4\n")
	if cfg.Autonomous.MaxConcurrentTasks != 4 {
		t.Fatalf("max_concurrent_tasks = %d, want 4", cfg.Autonomous.MaxConcurrentTasks)
	}
	p := filepath.Join(t.TempDir(), "saved.yaml")
	if err := Save(cfg, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	again, err := Load(p)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if again.Autonomous.MaxConcurrentTasks != 4 {
		t.Fatalf("after round trip max_concurrent_tasks = %d, want 4", again.Autonomous.MaxConcurrentTasks)
	}
}

func TestDeprecatedKeys_MaxParallelTasks(t *testing.T) {
	cfg := loadYAML(t, "autonomous:\n  max_parallel_tasks: 1\n")
	if !slices.Contains(cfg.DeprecatedKeys, "autonomous.max_parallel_tasks") {
		t.Fatalf("DeprecatedKeys = %v, want autonomous.max_parallel_tasks", cfg.DeprecatedKeys)
	}
	clean := loadYAML(t, "autonomous:\n  max_concurrent_tasks: 2\n")
	if len(clean.DeprecatedKeys) != 0 {
		t.Fatalf("DeprecatedKeys = %v, want empty", clean.DeprecatedKeys)
	}
	if DefaultConfig().Autonomous.MaxParallelTasks != 0 {
		t.Fatal("DefaultConfig must not inject a max_parallel_tasks default")
	}
}

func TestDeprecatedKeys_LoadSecurePlaintext(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte("autonomous:\n  max_parallel_tasks: 3\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadSecure(p, nil)
	if err != nil {
		t.Fatalf("LoadSecure: %v", err)
	}
	if !slices.Contains(cfg.DeprecatedKeys, "autonomous.max_parallel_tasks") {
		t.Fatalf("DeprecatedKeys = %v, want autonomous.max_parallel_tasks", cfg.DeprecatedKeys)
	}
	if cfg.Autonomous.MaxConcurrentTasks != 1 {
		t.Fatalf("max_concurrent_tasks = %d, want default 1", cfg.Autonomous.MaxConcurrentTasks)
	}
}
