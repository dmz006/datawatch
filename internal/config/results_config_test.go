package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestResultsConfigDefault(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Results.ScheduleHistoryLimit != 20 {
		t.Errorf("DefaultConfig results.schedule_history_limit = %d, want 20", cfg.Results.ScheduleHistoryLimit)
	}
	if got := cfg.Results.EffectiveScheduleHistoryLimit(); got != 20 {
		t.Errorf("EffectiveScheduleHistoryLimit = %d, want 20", got)
	}
	// Load from a file without the key applies the default.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg2, err := Load(path) // nonexistent → DefaultConfig
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg2.Results.EffectiveScheduleHistoryLimit(); got != 20 {
		t.Errorf("Loaded default limit = %d, want 20", got)
	}
	// Explicit values below 1 fall back to the default (effective
	// minimum is 1).
	if got := (ResultsConfig{ScheduleHistoryLimit: 0}).EffectiveScheduleHistoryLimit(); got != 20 {
		t.Errorf("limit 0 effective = %d, want 20 (default)", got)
	}
	if got := (ResultsConfig{ScheduleHistoryLimit: -5}).EffectiveScheduleHistoryLimit(); got != 20 {
		t.Errorf("limit -5 effective = %d, want 20 (default)", got)
	}
	if got := (ResultsConfig{ScheduleHistoryLimit: 1}).EffectiveScheduleHistoryLimit(); got != 1 {
		t.Errorf("limit 1 effective = %d, want 1 (min)", got)
	}
	if got := (ResultsConfig{ScheduleHistoryLimit: 7}).EffectiveScheduleHistoryLimit(); got != 7 {
		t.Errorf("limit 7 effective = %d, want 7", got)
	}
}

func TestResultsConfigYAMLRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := DefaultConfig()
	cfg.Results.ScheduleHistoryLimit = 7
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Results.ScheduleHistoryLimit != 7 {
		t.Fatalf("round-tripped results.schedule_history_limit = %d, want 7", loaded.Results.ScheduleHistoryLimit)
	}

	// Raw YAML marshal → unmarshal round-trip.
	b, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	var into Config
	if err := yaml.Unmarshal(b, &into); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if into.Results.ScheduleHistoryLimit != 7 {
		t.Errorf("yaml round-trip limit = %d, want 7", into.Results.ScheduleHistoryLimit)
	}

	// Hand-written YAML with the key.
	if err := os.WriteFile(path, []byte("results:\n  schedule_history_limit: 3\n"), 0644); err != nil {
		t.Fatalf("write hand yaml: %v", err)
	}
	hcfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load hand yaml: %v", err)
	}
	if hcfg.Results.ScheduleHistoryLimit != 3 {
		t.Errorf("hand-written yaml limit = %d, want 3", hcfg.Results.ScheduleHistoryLimit)
	}
}
