package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// captureUsageCalculatorLog swaps the warning sink for the test and returns
// the lines logged so far.
func captureUsageCalculatorLog(t *testing.T) func() []string {
	t.Helper()
	var lines []string
	orig := usageCalculatorLogf
	usageCalculatorLogf = func(format string, args ...interface{}) {
		lines = append(lines, strings.TrimSpace(sprintf(format, args...)))
	}
	t.Cleanup(func() { usageCalculatorLogf = orig })
	return func() []string { return lines }
}

func TestUsageCalculatorDefaults(t *testing.T) {
	c := DefaultConfig().Usage.Calculator
	want := UsageCalculatorConfig{
		Variant: "all", Policy: "use-all", SafetyPoints: 5, CostPerTaskPoints: 0.1,
		BaselinePer24h: 3, MaxLanes: 3, ResetWeekday: "sunday", ResetTime: "08:00",
	}
	if c != want {
		t.Errorf("default usage.calculator = %+v, want %+v", c, want)
	}
	if err := ValidateUsageCalculator(c); err != nil {
		t.Errorf("defaults fail Validate: %v", err)
	}
}

func TestUsageCalculatorLoadAppliesDefaultsAndKeepsExplicitZero(t *testing.T) {
	logged := captureUsageCalculatorLog(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte("usage:\n  calculator:\n    safety_points: 0\n    baseline_per_24h: 0\n    max_lanes: 5\n    policy: pace\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	c := cfg.Usage.Calculator
	if c.SafetyPoints != 0 || c.BaselinePer24h != 0 || c.MaxLanes != 5 || c.Policy != "pace" {
		t.Errorf("explicit values lost: %+v", c)
	}
	if c.Variant != "all" || c.CostPerTaskPoints != 0.1 || c.ResetZone != "" || c.ResetTime != "08:00" || c.ResetWeekday != "sunday" {
		t.Errorf("unset keys did not default: %+v", c)
	}
	if got := logged(); len(got) != 0 {
		t.Errorf("a clean load logged %v", got)
	}
}

func TestUsageCalculatorRetiredVariantFallsBackAndLogsOneLine(t *testing.T) {
	logged := captureUsageCalculatorLog(t)
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte("usage:\n  calculator:\n    variant: radar-dial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Usage.Calculator.Variant != UsageVariantAll {
		t.Errorf("retired variant = %q, want %q", cfg.Usage.Calculator.Variant, UsageVariantAll)
	}
	got := logged()
	if len(got) != 1 {
		t.Fatalf("logged %d lines, want 1: %v", len(got), got)
	}
	if strings.Contains(got[0], "\n") || !strings.Contains(got[0], "usage.calculator.variant") || !strings.Contains(got[0], "radar-dial") || !strings.Contains(got[0], `"all"`) {
		t.Errorf("log line = %q", got[0])
	}
}

func TestUsageCalculatorEffectiveIsQuiet(t *testing.T) {
	logged := captureUsageCalculatorLog(t)
	c := EffectiveUsageCalculator(UsageCalculatorConfig{Variant: "gone", Policy: "x", MaxLanes: 99, ResetZone: "+05:00"})
	d := DefaultUsageCalculator()
	if c.Variant != d.Variant || c.Policy != d.Policy || c.MaxLanes != d.MaxLanes {
		t.Errorf("effective = %+v", c)
	}
	want := "UTC"
	if z, ok := ResolveUsageServerZone(); ok {
		want = z
	}
	if c.ResetZone != want {
		t.Errorf("invalid explicit zone effective = %q, want the resolved server zone %q", c.ResetZone, want)
	}
	if got := logged(); len(got) != 0 {
		t.Errorf("Effective logged %v", got)
	}
	if got := EffectiveUsageCalculator(UsageCalculatorConfig{Variant: " Lane-Planner ", ResetWeekday: "FRIDAY"}); got.Variant != UsageVariantLanePlanner || got.ResetWeekday != "friday" {
		t.Errorf("case/space not normalised: %+v", got)
	}
}

func TestUsageCalculatorNormalizeWarnsOnOutOfRange(t *testing.T) {
	logged := captureUsageCalculatorLog(t)
	c := normalizeUsageCalculator(UsageCalculatorConfig{
		SafetyPoints: 51, CostPerTaskPoints: -1, BaselinePer24h: 21, MaxLanes: 9,
		ResetTime: "25:00", ResetZone: "Mars/Olympus", Keeper: "bad peer!",
	}, usageCalculatorLogf)
	d := DefaultUsageCalculator()
	if c.SafetyPoints != d.SafetyPoints || c.CostPerTaskPoints != d.CostPerTaskPoints || c.BaselinePer24h != d.BaselinePer24h ||
		c.MaxLanes != d.MaxLanes || c.ResetTime != d.ResetTime || c.ResetZone != d.ResetZone || c.Keeper != "" {
		t.Errorf("not defaulted: %+v", c)
	}
	if got := logged(); len(got) != 7 {
		t.Errorf("logged %d lines, want 7: %v", len(got), got)
	}
}

func TestValidateUsageCalculatorBounds(t *testing.T) {
	okF := func(name string, f func(float64) error, vals ...float64) {
		for _, v := range vals {
			if err := f(v); err != nil {
				t.Errorf("%s(%v) = %v, want ok", name, v, err)
			}
		}
	}
	badF := func(name string, f func(float64) error, vals ...float64) {
		for _, v := range vals {
			if f(v) == nil {
				t.Errorf("%s(%v) accepted", name, v)
			}
		}
	}
	nan := zeroOverZero()
	okF("safety", ValidateUsageCalculatorSafetyPoints, 0, 5, 50)
	badF("safety", ValidateUsageCalculatorSafetyPoints, -0.1, 50.1, nan)
	okF("cost", ValidateUsageCalculatorCostPerTaskPoints, 0.0001, 0.1, 12)
	badF("cost", ValidateUsageCalculatorCostPerTaskPoints, 0, -1, nan)
	okF("baseline", ValidateUsageCalculatorBaselinePer24h, 0, 3, 20)
	badF("baseline", ValidateUsageCalculatorBaselinePer24h, -1, 20.5, nan)
	for _, n := range []int{1, 3, 8} {
		if err := ValidateUsageCalculatorMaxLanes(n); err != nil {
			t.Errorf("max_lanes %d: %v", n, err)
		}
	}
	for _, n := range []int{0, -1, 9} {
		if ValidateUsageCalculatorMaxLanes(n) == nil {
			t.Errorf("max_lanes %d accepted", n)
		}
	}
}

func TestValidateUsageCalculatorChoices(t *testing.T) {
	for _, v := range []string{"", "all", "pace-gauge", "Lane-Planner", "burn-ledger"} {
		if err := ValidateUsageCalculatorVariant(v); err != nil {
			t.Errorf("variant %q: %v", v, err)
		}
	}
	if ValidateUsageCalculatorVariant("pace_gauge") == nil {
		t.Error("variant pace_gauge accepted")
	}
	for _, v := range []string{"", "use-all", "pace", "finish-early"} {
		if err := ValidateUsageCalculatorPolicy(v); err != nil {
			t.Errorf("policy %q: %v", v, err)
		}
	}
	if ValidateUsageCalculatorPolicy("burn") == nil {
		t.Error("policy burn accepted")
	}
	for _, v := range usageWeekdays {
		if err := ValidateUsageCalculatorResetWeekday(v); err != nil {
			t.Errorf("weekday %q: %v", v, err)
		}
	}
	if ValidateUsageCalculatorResetWeekday("someday") == nil {
		t.Error("weekday someday accepted")
	}
}

func TestValidateUsageCalculatorInvalidClock(t *testing.T) {
	for _, v := range []string{"", "00:00", "08:00", "23:59", "19:30"} {
		if err := ValidateUsageCalculatorResetTime(v); err != nil {
			t.Errorf("clock %q: %v", v, err)
		}
	}
	for _, v := range []string{"24:00", "8:00", "08:60", "0800", "08:00:00", "8am", "-1:00", "ab:cd"} {
		if ValidateUsageCalculatorResetTime(v) == nil {
			t.Errorf("clock %q accepted", v)
		}
	}
}

func TestValidateUsageCalculatorInvalidZone(t *testing.T) {
	for _, v := range []string{"", "UTC", "America/New_York", "Europe/London", "Asia/Kolkata"} {
		if err := ValidateUsageCalculatorResetZone(v); err != nil {
			t.Errorf("zone %q: %v", v, err)
		}
	}
	for _, v := range []string{"Mars/Olympus", "New_York", "+05:00", "-03:00", "UTC+5", "GMT-3", "Etc/GMT+5", "Local", "local"} {
		if ValidateUsageCalculatorResetZone(v) == nil {
			t.Errorf("zone %q accepted", v)
		}
	}
}

func TestValidateUsageCalculatorKeeper(t *testing.T) {
	for _, v := range []string{"", "gpu-box", "peer_2.lan"} {
		if err := ValidateUsageCalculatorKeeper(v); err != nil {
			t.Errorf("keeper %q: %v", v, err)
		}
	}
	for _, v := range []string{"has space", "peer/one", strings.Repeat("a", 129)} {
		if ValidateUsageCalculatorKeeper(v) == nil {
			t.Errorf("keeper %q accepted", v)
		}
	}
}

func TestValidateUsageCalculatorWholeStruct(t *testing.T) {
	if err := ValidateUsageCalculator(DefaultUsageCalculator()); err != nil {
		t.Fatal(err)
	}
	bad := DefaultUsageCalculator()
	bad.ResetZone = "Mars/Olympus"
	if ValidateUsageCalculator(bad) == nil {
		t.Error("bad zone accepted by struct validation")
	}
}

func sprintf(format string, args ...interface{}) string { return fmt.Sprintf(format, args...) }

func zeroOverZero() float64 {
	z := 0.0
	return z / z
}

// fakeServerZone fixes the three server-zone lookups so the tests do not
// depend on the actual host. env returns the fake TZ value (a nil env leaves
// TZ unset); link is the fake local-time symlink target (a nil link makes the
// read fail); local defaults to a "Local"-named zone so the time.Local
// fallback never resolves.
func fakeServerZone(t *testing.T, tz string, link func() (string, error)) {
	t.Helper()
	origEnv, origLink, origLocal := usageServerZoneEnv, usageLocaltimeTarget, usageLocalLocation
	usageServerZoneEnv = func(key string) string {
		if key == "TZ" {
			return tz
		}
		return ""
	}
	usageLocaltimeTarget = func(path string) (string, error) {
		if path != usageLocaltimeLink {
			return "", errors.New("fake: not the local-time symlink")
		}
		if link == nil {
			return "", errors.New("fake: not a symlink")
		}
		return link()
	}
	usageLocalLocation = time.FixedZone("Local", 0)
	t.Cleanup(func() { usageServerZoneEnv, usageLocaltimeTarget, usageLocalLocation = origEnv, origLink, origLocal })
}

// TestEffectiveUsageResetZoneUnsetFollowsFakeTZ covers an unset zone
// following a fake TZ value.
func TestEffectiveUsageResetZoneUnsetFollowsFakeTZ(t *testing.T) {
	fakeServerZone(t, "America/New_York", nil)
	zone, serverZone, fellBack := EffectiveUsageResetZone(UsageCalculatorConfig{})
	if zone != "America/New_York" || !serverZone || fellBack {
		t.Fatalf("EffectiveUsageResetZone() = %q, %v, %v; want America/New_York, true, false", zone, serverZone, fellBack)
	}
	if e := EffectiveUsageCalculator(UsageCalculatorConfig{}); e.ResetZone != "America/New_York" {
		t.Errorf("effective calculator ResetZone = %q, want America/New_York", e.ResetZone)
	}
}

// TestEffectiveUsageResetZoneExplicitWins covers a set zone (including an
// explicit UTC) being used as-is and never rewritten to the server zone.
func TestEffectiveUsageResetZoneExplicitWins(t *testing.T) {
	fakeServerZone(t, "America/New_York", nil)
	for _, z := range []string{"UTC", "Europe/London"} {
		zone, serverZone, fellBack := EffectiveUsageResetZone(UsageCalculatorConfig{ResetZone: z})
		if zone != z || serverZone || fellBack {
			t.Errorf("EffectiveUsageResetZone(%q) = %q, %v, %v; want %q, false, false", z, zone, serverZone, fellBack, z)
		}
		if e := EffectiveUsageCalculator(UsageCalculatorConfig{ResetZone: z}); e.ResetZone != z {
			t.Errorf("explicit %q rewritten to %q", z, e.ResetZone)
		}
	}
	logged := captureUsageCalculatorLog(t)
	normalizeUsageCalculator(UsageCalculatorConfig{ResetZone: "UTC"}, usageCalculatorLogf)
	if got := logged(); len(got) != 0 {
		t.Errorf("an explicit valid zone logged %v", got)
	}
}

// TestEffectiveUsageResetZoneInvalidTZFallsBack covers a fixed-offset TZ such
// as UTC+5 being rejected and the reset falling back to UTC with fellBack.
func TestEffectiveUsageResetZoneInvalidTZFallsBack(t *testing.T) {
	fakeServerZone(t, "UTC+5", nil)
	if zone, ok := ResolveUsageServerZone(); ok {
		t.Fatalf("ResolveUsageServerZone() = %q, false for the fixed offset UTC+5", zone)
	}
	zone, serverZone, fellBack := EffectiveUsageResetZone(UsageCalculatorConfig{})
	if zone != "UTC" || !serverZone || !fellBack {
		t.Fatalf("EffectiveUsageResetZone() = %q, %v, %v; want UTC, true, true", zone, serverZone, fellBack)
	}

	logged := captureUsageCalculatorLog(t)
	loadDir := t.TempDir()
	p := filepath.Join(loadDir, "config.yaml")
	if err := os.WriteFile(p, []byte("usage:\n  calculator:\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err != nil {
		t.Fatal(err)
	}
	if got := logged(); len(got) != 0 {
		t.Errorf("an unset zone with an invalid host TZ logged %v", got)
	}
}

// TestEffectiveUsageResetZoneFollowsFakeLocaltimeLink covers TZ unset and the
// zone coming from a faked local-time symlink target.
func TestEffectiveUsageResetZoneFollowsFakeLocaltimeLink(t *testing.T) {
	fakeServerZone(t, "", func() (string, error) { return "/usr/share/zoneinfo/Asia/Tokyo", nil })
	zone, ok := ResolveUsageServerZone()
	if zone != "Asia/Tokyo" || !ok {
		t.Fatalf("ResolveUsageServerZone() = %q, %v; want Asia/Tokyo, true", zone, ok)
	}
	zone, serverZone, fellBack := EffectiveUsageResetZone(UsageCalculatorConfig{})
	if zone != "Asia/Tokyo" || !serverZone || fellBack {
		t.Fatalf("EffectiveUsageResetZone() = %q, %v, %v; want Asia/Tokyo, true, false", zone, serverZone, fellBack)
	}

	// A target without a zoneinfo element (or nothing after it) never resolves.
	for _, bad := range []string{"/usr/bin/date", "/usr/share/zoneinfo"} {
		fakeServerZone(t, "", func(target string) func() (string, error) {
			return func() (string, error) { return bad, nil }
		}(""))
		if _, ok := ResolveUsageServerZone(); ok {
			t.Errorf("target %q resolved to a zone", bad)
		}
	}
}
