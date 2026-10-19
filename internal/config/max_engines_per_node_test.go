package config

import "testing"

func TestAutonomousMaxEnginesPerNode_EmptyConfigDefaultsToOne(t *testing.T) {
	cfg := loadYAML(t, "autonomous:\n  enabled: true\n")
	if cfg.Autonomous.MaxEnginesPerNode != 1 {
		t.Fatalf("max_engines_per_node = %d, want default 1", cfg.Autonomous.MaxEnginesPerNode)
	}
	if got := DefaultConfig().Autonomous.MaxEnginesPerNode; got != 1 {
		t.Fatalf("DefaultConfig max_engines_per_node = %d, want 1", got)
	}
}

func TestAutonomousMaxEnginesPerNode_YAMLValueLoads(t *testing.T) {
	cfg := loadYAML(t, "autonomous:\n  max_engines_per_node: 2\n")
	if cfg.Autonomous.MaxEnginesPerNode != 2 {
		t.Fatalf("max_engines_per_node = %d, want 2", cfg.Autonomous.MaxEnginesPerNode)
	}
}

func TestAutonomousMaxEnginesPerNode_ZeroClampsToOne(t *testing.T) {
	cfg := loadYAML(t, "autonomous:\n  max_engines_per_node: 0\n")
	if cfg.Autonomous.MaxEnginesPerNode != 1 {
		t.Fatalf("max_engines_per_node = %d, want clamp to 1", cfg.Autonomous.MaxEnginesPerNode)
	}
}

func TestAutonomousMaxEnginesPerNode_NegativeClampsToOne(t *testing.T) {
	cfg := loadYAML(t, "autonomous:\n  max_engines_per_node: -3\n")
	if cfg.Autonomous.MaxEnginesPerNode != 1 {
		t.Fatalf("max_engines_per_node = %d, want clamp to 1", cfg.Autonomous.MaxEnginesPerNode)
	}
}
