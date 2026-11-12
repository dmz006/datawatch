// dashboard.hide_inactive_cards setting: *bool so an absent key reads
// its default (true), with explicit YAML round trips for both values.

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDashboardHideInactiveCardsDefault(t *testing.T) {
	var c Config
	if c.Dashboard.HideInactiveCards != nil {
		t.Fatalf("absent key: HideInactiveCards = %v, want nil", *c.Dashboard.HideInactiveCards)
	}
	if !c.Dashboard.EffectiveHideInactiveCards() {
		t.Fatal("default: EffectiveHideInactiveCards = false, want true")
	}
}

func TestDashboardHideInactiveCardsYAMLRoundTrip(t *testing.T) {
	for _, want := range []bool{false, true} {
		w := want
		t.Run(tname(want), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "datawatch.yaml")
			src := &Config{Git: GitConfig{ReleaseCIWindowMinutes: 60}}
			src.Dashboard.HideInactiveCards = &w
			if err := Save(src, path); err != nil {
				t.Fatalf("Save: %v", err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Dashboard.HideInactiveCards == nil {
				t.Fatalf("round trip %v: HideInactiveCards = nil, want explicit %v", want, want)
			}
			if *cfg.Dashboard.HideInactiveCards != want {
				t.Fatalf("round trip: value = %v, want %v", *cfg.Dashboard.HideInactiveCards, want)
			}
			if got := cfg.Dashboard.EffectiveHideInactiveCards(); got != want {
				t.Fatalf("round trip: effective = %v, want %v", got, want)
			}
		})
	}
}

func tname(want bool) string {
	if want {
		return "true"
	}
	return "false"
}

func TestDashboardHideInactiveCardsAbsentKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "datawatch.yaml")
	if err := os.WriteFile(path, []byte("hostname: testhost\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Dashboard.HideInactiveCards != nil {
		t.Fatalf("absent key: HideInactiveCards = %v, want nil", *cfg.Dashboard.HideInactiveCards)
	}
	if !cfg.Dashboard.EffectiveHideInactiveCards() {
		t.Fatal("absent key: effective = false, want the default true")
	}
}
