package main

import (
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

func applyRunCapFromConfig(cfg *config.Config) int {
	autonomouspkg.DefaultVerifierStats.SetRunCapSeconds(config.VerifierRunCapSeconds(cfg.Autonomous.VerifierMaxRunSeconds))
	return autonomouspkg.DefaultVerifierStats.Snapshot().RunCapSeconds
}

func TestVerifierRunCapStatsFromConfigDefault(t *testing.T) {
	if got := applyRunCapFromConfig(config.DefaultConfig()); got != 2700 {
		t.Fatalf("run cap = %d, want 2700", got)
	}
}

func TestVerifierRunCapStatsFromConfigZero(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Autonomous.VerifierMaxRunSeconds = 0
	if got := applyRunCapFromConfig(cfg); got != 0 {
		t.Fatalf("run cap = %d, want 0", got)
	}
}
