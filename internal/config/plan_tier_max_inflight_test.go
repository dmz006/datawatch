package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func wantTierTable() map[string]int {
	// Operator-policy starting table: pro=1, max_5x=2, max_20x=4,
	// api=0 (unlimited — the dollar budget is a later phase).
	return map[string]int{"pro": 1, "max_5x": 2, "max_20x": 4, "api": 0}
}

func TestAutonomousPlanTierMaxInflight_Defaults(t *testing.T) {
	cfg := loadYAML(t, "autonomous:\n  enabled: true\n")
	m := cfg.Autonomous.PlanTierMaxInflight
	for k, v := range wantTierTable() {
		if m[k] != v {
			t.Fatalf("default plan_tier_max_inflight[%q] = %d, want %d (map=%v)", k, m[k], v, m)
		}
	}
	d := DefaultConfig().Autonomous.PlanTierMaxInflight
	for k, v := range wantTierTable() {
		if d[k] != v {
			t.Fatalf("DefaultConfig plan_tier_max_inflight[%q] = %d, want %d", k, d[k], v)
		}
	}
}

func TestAutonomousPlanTierMaxInflight_YAMLOverrideLoads(t *testing.T) {
	cfg := loadYAML(t, `autonomous:
  enabled: true
  plan_tier_max_inflight:
    pro: 3
    max_5x: 5
    max_20x: 8
    api: 0
`)
	m := cfg.Autonomous.PlanTierMaxInflight
	if m["pro"] != 3 || m["max_5x"] != 5 || m["max_20x"] != 8 {
		t.Fatalf("override map not loaded: %v", m)
	}
}

func TestAutonomousPlanTierMaxInflight_YAMLRoundTrip(t *testing.T) {
	cfg := DefaultConfig()
	// A 0 must round-trip (api: unlimited is a real operator setting).
	cfg.Autonomous.PlanTierMaxInflight = map[string]int{"pro": 2, "max_5x": 3, "max_20x": 5, "api": 0}

	// Save/Load through the real file path is the round trip operators
	// actually use.
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(cfg, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	round, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := round.Autonomous.PlanTierMaxInflight
	want := map[string]int{"pro": 2, "max_5x": 3, "max_20x": 5, "api": 0}
	if len(got) != len(want) {
		t.Fatalf("round-trip map = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("round-trip map[%q] = %d, want %d (map=%v)", k, got[k], v, got)
		}
	}
}

func loadExpectError(t *testing.T, body string) error {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(p)
	return err
}

func TestAutonomousPlanTierMaxInflight_NegativeCapRejected(t *testing.T) {
	err := loadExpectError(t, `autonomous:
  plan_tier_max_inflight:
    pro: -1
`)
	if err == nil || !strings.Contains(err.Error(), "plan_tier_max_inflight") {
		t.Fatalf("negative tier cap must be a config error naming the key, got %v", err)
	}
}

func TestAutonomousPlanTierMaxInflight_UnknownTierRejected(t *testing.T) {
	err := loadExpectError(t, `autonomous:
  plan_tier_max_inflight:
    pro: 1
    ultra: 2
`)
	if err == nil || !strings.Contains(err.Error(), "plan_tier_max_inflight") {
		t.Fatalf("unknown tier key must be a config error naming the key, got %v", err)
	}
}
