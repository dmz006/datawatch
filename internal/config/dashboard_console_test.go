// dashboard.console.* settings: *bool so an absent key reads its default
// (true) — the overall enabled switch and all five show_* keys — with
// explicit YAML round trips for both values.

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// consoleField pairs one dashboard.console.* key with its pointer field and
// its Effective* helper, so every key gets the same coverage.
type consoleField struct {
	name string
	get  func(*Config) *bool
	set  func(*Config, *bool)
	eff  func(*Config) bool
}

func consoleFields() []consoleField {
	setP := func(f **bool, v *bool) { *f = v }
	return []consoleField{
		{"enabled", func(c *Config) *bool { return c.Dashboard.Console.Enabled }, func(cfg *Config, v *bool) { setP(&cfg.Dashboard.Console.Enabled, v) }, func(c *Config) bool { return c.Dashboard.Console.EffectiveEnabled() }},
		{"show_questions", func(c *Config) *bool { return c.Dashboard.Console.ShowQuestions }, func(cfg *Config, v *bool) { setP(&cfg.Dashboard.Console.ShowQuestions, v) }, func(c *Config) bool { return c.Dashboard.Console.EffectiveShowQuestions() }},
		{"show_held_blocked", func(c *Config) *bool { return c.Dashboard.Console.ShowHeld }, func(cfg *Config, v *bool) { setP(&cfg.Dashboard.Console.ShowHeld, v) }, func(c *Config) bool { return c.Dashboard.Console.EffectiveShowHeld() }},
		{"show_lanes", func(c *Config) *bool { return c.Dashboard.Console.ShowLanes }, func(cfg *Config, v *bool) { setP(&cfg.Dashboard.Console.ShowLanes, v) }, func(c *Config) bool { return c.Dashboard.Console.EffectiveShowLanes() }},
		{"show_usage", func(c *Config) *bool { return c.Dashboard.Console.ShowUsage }, func(cfg *Config, v *bool) { setP(&cfg.Dashboard.Console.ShowUsage, v) }, func(c *Config) bool { return c.Dashboard.Console.EffectiveShowUsage() }},
		{"show_ci_release", func(c *Config) *bool { return c.Dashboard.Console.ShowCIRelease }, func(cfg *Config, v *bool) { setP(&cfg.Dashboard.Console.ShowCIRelease, v) }, func(c *Config) bool { return c.Dashboard.Console.EffectiveShowCIRelease() }},
	}
}

func TestDashboardConsoleDefaultsTrue(t *testing.T) {
	var cfg Config
	for _, f := range consoleFields() {
		t.Run(f.name, func(t *testing.T) {
			if f.get(&cfg) != nil {
				t.Fatalf("absent key: %s = %v, want nil", f.name, *f.get(&cfg))
			}
			if !f.eff(&cfg) {
				t.Fatalf("default: %s reads false, want the default true", f.name)
			}
		})
	}
}

func TestDashboardConsoleYAMLRoundTrip(t *testing.T) {
	for _, f := range consoleFields() {
		t.Run(f.name, func(t *testing.T) {
			for _, want := range []bool{false, true} {
				w := want
				t.Run(tname(want), func(t *testing.T) {
					path := filepath.Join(t.TempDir(), "datawatch.yaml")
					src := &Config{Git: GitConfig{ReleaseCIWindowMinutes: 60}}
					f.set(src, &w)
					if err := Save(src, path); err != nil {
						t.Fatalf("Save: %v", err)
					}
					cfg, err := Load(path)
					if err != nil {
						t.Fatalf("Load: %v", err)
					}
					if f.get(cfg) == nil {
						t.Fatalf("round trip %v: %s = nil, want explicit %v", want, f.name, want)
					}
					if *f.get(cfg) != want {
						t.Fatalf("round trip: %s = %v, want %v", f.name, *f.get(cfg), want)
					}
					if got := f.eff(cfg); got != want {
						t.Fatalf("round trip: %s effective = %v, want %v", f.name, got, want)
					}
				})
			}
		})
	}
}

// TestDashboardConsolePartialKeysReadDefaults pins that writing one
// dashboard.console.* key leaves the sibling keys at their default, true,
// exactly like the hide_inactive_cards pattern does.
func TestDashboardConsolePartialKeysReadDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "datawatch.yaml")
	body := "dashboard:\n" +
		"  hide_inactive_cards: false\n" +
		"  console:\n" +
		"    enabled: false\n" +
		"    show_lanes: true\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// The hide_inactive_cards neighbour is unaffected by the console block.
	if got := cfg.Dashboard.EffectiveHideInactiveCards(); got {
		t.Fatal("hide_inactive_cards = true, want the explicit false to stick")
	}
	want := map[string]bool{
		"enabled":           false,
		"show_questions":    true,
		"show_held_blocked": true,
		"show_lanes":        true,
		"show_usage":        true,
		"show_ci_release":   true,
	}
	for _, f := range consoleFields() {
		if got := f.eff(cfg); got != want[f.name] {
			t.Fatalf("%s = %v, want %v (absent keys read the default true; explicit values stick)", f.name, got, want[f.name])
		}
	}
}
