package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestShippedDefaults asserts the shipped block defaults: unset fields read
// enabled=true, show_chip=true, filter_default="all",
// count_separately=true, backfill_on_start=false — the same "unset reads
// default" contract as PreserveAttemptWork.
func TestShippedDefaults(t *testing.T) {
	c := ShippedConfig{}
	if !c.EffectiveEnabled() {
		t.Error("unset enabled should read true")
	}
	if !c.EffectiveShowChip() {
		t.Error("unset show_chip should read true")
	}
	if got := c.EffectiveFilterDefault(); got != ShippedFilterAll {
		t.Errorf("unset filter_default should read %q, got %q", ShippedFilterAll, got)
	}
	if !c.EffectiveCountSeparately() {
		t.Error("unset count_separately should read true")
	}
	if c.EffectiveBackfillOnStart() {
		t.Error("unset backfill_on_start should read false")
	}

	// A DefaultConfig load path must agree.
	cfg := DefaultConfig()
	if !cfg.Autonomous.Shipped.EffectiveEnabled() ||
		!cfg.Autonomous.Shipped.EffectiveShowChip() ||
		cfg.Autonomous.Shipped.EffectiveFilterDefault() != "all" ||
		!cfg.Autonomous.Shipped.EffectiveCountSeparately() ||
		cfg.Autonomous.Shipped.EffectiveBackfillOnStart() {
		t.Errorf("DefaultConfig shipped defaults wrong: %+v", cfg.Autonomous.Shipped)
	}
}

// TestShippedConfigRoundTrip writes a full shipped block to a config file,
// loads it, and also exercises the parse/Set surface; both must agree on
// the five settings, and bad values must be rejected on load.
func TestShippedConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()

	truthy := false // explicit explicit false values
	offFilter := ShippedFilterShipped
	yamlText := `
autonomous:
  shipped:
    enabled: true
    show_chip: false
    filter_default: shipped
    count_separately: false
    backfill_on_start: true
`
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte(yamlText), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := cfg.Autonomous.Shipped
	if s.Enabled == nil || *s.Enabled != true {
		t.Errorf("enabled = %v, want explicit true", s.Enabled)
	}
	if s.ShowChip == nil || *s.ShowChip != truthy {
		t.Errorf("show_chip = %v, want explicit false", s.ShowChip)
	}
	if s.FilterDefault != offFilter {
		t.Errorf("filter_default = %q, want %q", s.FilterDefault, offFilter)
	}
	if s.CountSeparately == nil || *s.CountSeparately != false {
		t.Errorf("count_separately = %v, want explicit false", s.CountSeparately)
	}
	if s.BackfillOnStart == nil || *s.BackfillOnStart != true {
		t.Errorf("backfill_on_start = %v, want explicit true", s.BackfillOnStart)
	}

	// Effective readers agree with the stored values.
	if s.EffectiveShowChip() || s.EffectiveCountSeparately() {
		t.Errorf("effective show_chip/count_separately must be false: %+v", s)
	}
	if s.EffectiveFilterDefault() != ShippedFilterShipped {
		t.Errorf("effective filter_default = %q, want %q", s.EffectiveFilterDefault(), ShippedFilterShipped)
	}

	// Parse/Set surface: the Set path must accept the same values the file
	// accepts, produce the same stored form, and refuse bad values.
	var c ShippedConfig
	if err := c.Set("enabled", "true"); err != nil {
		t.Fatalf("Set(enabled, true): %v", err)
	}
	if err := c.Set("show_chip", false); err != nil {
		t.Fatalf("Set(show_chip, false): %v", err)
	}
	if err := c.Set("filter_default", "Shipped"); err != nil {
		t.Fatalf("Set(filter_default, \"Shipped\") case-insensitive: %v", err)
	}
	if err := c.Set("count_separately", "false"); err != nil {
		t.Fatalf("Set(count_separately, \"false\"): %v", err)
	}
	if err := c.Set("backfill_on_start", true); err != nil {
		t.Fatalf("Set(backfill_on_start, true): %v", err)
	}
	if *c.Enabled != true || *c.ShowChip != false || c.FilterDefault != ShippedFilterShipped || *c.CountSeparately != false || *c.BackfillOnStart != true {
		t.Errorf("Set round-trip = %+v", c)
	}

	// A bad filter value must be rejected both at load and on Set, and a
	// blank filter_default is legal (takes the default).
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("autonomous:\n  shipped:\n    filter_default: everything\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bad); err == nil || !strings.Contains(err.Error(), "autonomous.shipped.filter_default") {
		t.Errorf("Load with filter_default=everything: err = %v, want refusal naming the setting", err)
	}
	if err := c.Set("filter_default", 42); err == nil {
		t.Error("Set(filter_default, 42) must be refused")
	}
	if err := c.Set("enabled", "maybe"); err == nil {
		t.Error("Set(enabled, \"maybe\") must be refused")
	}
	if err := c.Set("unknown_key", true); err == nil {
		t.Error("Set(unknown_key) must be refused")
	}
	var blank ShippedConfig
	if err := blank.Set("filter_default", ""); err != nil {
		t.Errorf("Set(filter_default, \"\") blank should take the default, got: %v", err)
	}
	if blank.FilterDefault != ShippedFilterAll {
		t.Errorf("blank filter_default = %q, want %q", blank.FilterDefault, ShippedFilterAll)
	}

	// Keys outside the shipped block are not this block's concern.
	if err := ValidateShippedSetting("autonomous.preserve_attempt_work", true); err != nil {
		t.Errorf("ValidateShippedSetting must ignore non-shipped keys, got %v", err)
	}
	if err := ValidateShippedSetting(ShippedSettingPrefix+"filter_default", ShippedFilterUnshipped); err != nil {
		t.Errorf("ValidateShippedSetting(filter_default=unshipped): %v", err)
	}
}
