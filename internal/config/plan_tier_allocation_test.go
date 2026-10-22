package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanTierAllocationConfig(t *testing.T) {
	t.Run("default is empty", func(t *testing.T) {
		got := DefaultConfig().Autonomous.PlanTierAllocationUSD
		if got == nil || len(got) != 0 {
			t.Fatalf("default plan_tier_allocation_usd = %#v, want empty non-nil map", got)
		}
	})

	t.Run("valid table loads", func(t *testing.T) {
		cfg := loadYAML(t, `autonomous:
  enabled: true
  plan_tier_allocation_usd:
    pro: 20
    max_5x: 100.5
    max_20x: 400
    api: 0
`)
		got := cfg.Autonomous.PlanTierAllocationUSD
		if got["pro"] != 20 || got["max_5x"] != 100.5 || got["max_20x"] != 400 || got["api"] != 0 {
			t.Fatalf("table not loaded: %v", got)
		}
	})

	t.Run("negative value rejected", func(t *testing.T) {
		err := loadExpectError(t, `autonomous:
  plan_tier_allocation_usd:
    pro: -1
`)
		if err == nil || !strings.Contains(err.Error(), "plan_tier_allocation_usd") {
			t.Fatalf("negative value must be a config error naming the key, got %v", err)
		}
	})

	t.Run("unknown tier rejected", func(t *testing.T) {
		err := loadExpectError(t, `autonomous:
  plan_tier_allocation_usd:
    pro: 20
    ultra: 50
`)
		if err == nil || !strings.Contains(err.Error(), "plan_tier_allocation_usd") {
			t.Fatalf("unknown tier must be a config error naming the key, got %v", err)
		}
	})

	t.Run("yaml round trip", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Autonomous.PlanTierAllocationUSD = map[string]float64{"pro": 20, "max_5x": 99.5, "api": 0}
		p := filepath.Join(t.TempDir(), "config.yaml")
		if err := Save(cfg, p); err != nil {
			t.Fatalf("Save: %v", err)
		}
		round, err := Load(p)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		got := round.Autonomous.PlanTierAllocationUSD
		want := map[string]float64{"pro": 20, "max_5x": 99.5, "api": 0}
		if len(got) != len(want) {
			t.Fatalf("round-trip table = %v, want %v", got, want)
		}
		for k, v := range want {
			if got[k] != v {
				t.Fatalf("round-trip[%q] = %v, want %v (table=%v)", k, got[k], v, got)
			}
		}
	})
}
