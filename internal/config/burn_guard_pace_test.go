// pace_line policy settings: defaults, normalization, one shared validator.
//
// The pace_line settings sit flat under usage.burn_guard beside the existing
// guard settings (story 1a680001): mode off|shadow|on (unset reads shadow),
// week_tolerance 0..10 (default 2), points_to_week_factor 1..10 (default 1.3),
// window_percent_per_point 1..25 (default 4), window_stop_percent 50..95
// (default 85), reading_freshness_minutes 1..1440 (default 60). Settings only:
// the guard's behavior is unchanged until the pace_line story lands; the
// window anchors are fixed behavior, not a setting.

package config

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBurnGuardPaceLineModes(t *testing.T) {
	if got := BurnGuardModes(); !reflect.DeepEqual(got, []string{"off", "shadow", "on"}) {
		t.Errorf("BurnGuardModes() = %v, want [off shadow on]", got)
	}
	for _, ok := range []string{"off", "shadow", "on", " SHADOW ", "On"} {
		if !IsBurnGuardMode(ok) {
			t.Errorf("IsBurnGuardMode(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", " ", "auto", "Shadowy", "off on"} {
		if IsBurnGuardMode(bad) {
			t.Errorf("IsBurnGuardMode(%q) = true, want false", bad)
		}
	}
}

func TestBurnGuardPaceLineNormalize(t *testing.T) {
	nan := math.NaN()
	tests := []struct {
		name  string
		edit  func(c *BurnGuardConfig)
		check func(t *testing.T, c BurnGuardConfig)
		warn  string // substring expected in a warning; "" means none
	}{
		{"zero struct: unset mode reads shadow, unset numerics take defaults",
			func(c *BurnGuardConfig) { *c = BurnGuardConfig{} },
			func(t *testing.T, c BurnGuardConfig) {
				if c.PaceLineMode != "shadow" {
					t.Errorf("mode = %q, want shadow (unset reads shadow)", c.PaceLineMode)
				}
				if !(c.PaceLineWeekTolerance == 0 && c.PaceLinePointsToWeekFactor == 1.3 &&
					c.PaceLineWindowPercentPerPoint == 4 && c.PaceLineWindowStopPercent == 85 &&
					c.PaceLineReadingFreshnessMinutes == 60) {
					t.Errorf("zero struct not defaulted: %+v", c)
				}
			}, ""},
		{"defaults normalize to themselves", nil, func(t *testing.T, c BurnGuardConfig) {
			if !reflect.DeepEqual(c, DefaultBurnGuard()) {
				t.Errorf("defaults changed: %+v", c)
			}
		}, ""},
		{"mode off stays", func(c *BurnGuardConfig) { c.PaceLineMode = "off" },
			func(t *testing.T, c BurnGuardConfig) { eqStr(t, c.PaceLineMode, "off") }, ""},
		{"mode on case-folded and trimmed", func(c *BurnGuardConfig) { c.PaceLineMode = " ON " },
			func(t *testing.T, c BurnGuardConfig) { eqStr(t, c.PaceLineMode, "on") }, ""},
		{"mode invalid degrades to shadow", func(c *BurnGuardConfig) { c.PaceLineMode = "auto" },
			func(t *testing.T, c BurnGuardConfig) { eqStr(t, c.PaceLineMode, "shadow") }, "usage.burn_guard.mode"},
		{"week_tolerance at 0 is valid", func(c *BurnGuardConfig) { c.PaceLineWeekTolerance = 0 },
			func(t *testing.T, c BurnGuardConfig) { eqInt(t, c.PaceLineWeekTolerance, 0) }, ""},
		{"week_tolerance at maximum is valid", func(c *BurnGuardConfig) { c.PaceLineWeekTolerance = 10 },
			func(t *testing.T, c BurnGuardConfig) { eqInt(t, c.PaceLineWeekTolerance, 10) }, ""},
		{"week_tolerance negative", func(c *BurnGuardConfig) { c.PaceLineWeekTolerance = -1 },
			func(t *testing.T, c BurnGuardConfig) { eqInt(t, c.PaceLineWeekTolerance, 2) }, "week_tolerance"},
		{"week_tolerance over maximum", func(c *BurnGuardConfig) { c.PaceLineWeekTolerance = 11 },
			func(t *testing.T, c BurnGuardConfig) { eqInt(t, c.PaceLineWeekTolerance, 2) }, "week_tolerance"},
		{"points_to_week_factor zero is unset", func(c *BurnGuardConfig) { c.PaceLinePointsToWeekFactor = 0 },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.PaceLinePointsToWeekFactor, 1.3) }, ""},
		{"points_to_week_factor at minimum is valid", func(c *BurnGuardConfig) { c.PaceLinePointsToWeekFactor = 1 },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.PaceLinePointsToWeekFactor, 1) }, ""},
		{"points_to_week_factor at maximum is valid", func(c *BurnGuardConfig) { c.PaceLinePointsToWeekFactor = 10 },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.PaceLinePointsToWeekFactor, 10) }, ""},
		{"points_to_week_factor below minimum", func(c *BurnGuardConfig) { c.PaceLinePointsToWeekFactor = 0.9 },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.PaceLinePointsToWeekFactor, 1.3) }, "points_to_week_factor"},
		{"points_to_week_factor above maximum", func(c *BurnGuardConfig) { c.PaceLinePointsToWeekFactor = 10.1 },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.PaceLinePointsToWeekFactor, 1.3) }, "points_to_week_factor"},
		{"points_to_week_factor NaN", func(c *BurnGuardConfig) { c.PaceLinePointsToWeekFactor = nan },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.PaceLinePointsToWeekFactor, 1.3) }, "points_to_week_factor"},
		{"window_percent_per_point at minimum is valid", func(c *BurnGuardConfig) { c.PaceLineWindowPercentPerPoint = 1 },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.PaceLineWindowPercentPerPoint, 1) }, ""},
		{"window_percent_per_point at maximum is valid", func(c *BurnGuardConfig) { c.PaceLineWindowPercentPerPoint = 25 },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.PaceLineWindowPercentPerPoint, 25) }, ""},
		{"window_percent_per_point zero is unset", func(c *BurnGuardConfig) { c.PaceLineWindowPercentPerPoint = 0 },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.PaceLineWindowPercentPerPoint, 4) }, ""},
		{"window_percent_per_point below minimum", func(c *BurnGuardConfig) { c.PaceLineWindowPercentPerPoint = 0.5 },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.PaceLineWindowPercentPerPoint, 4) }, "window_percent_per_point"},
		{"window_percent_per_point above maximum", func(c *BurnGuardConfig) { c.PaceLineWindowPercentPerPoint = 25.5 },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.PaceLineWindowPercentPerPoint, 4) }, "window_percent_per_point"},
		{"window_stop_percent at minimum is valid", func(c *BurnGuardConfig) { c.PaceLineWindowStopPercent = 50 },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.PaceLineWindowStopPercent, 50) }, ""},
		{"window_stop_percent at maximum is valid", func(c *BurnGuardConfig) { c.PaceLineWindowStopPercent = 95 },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.PaceLineWindowStopPercent, 95) }, ""},
		{"window_stop_percent below minimum", func(c *BurnGuardConfig) { c.PaceLineWindowStopPercent = 49.5 },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.PaceLineWindowStopPercent, 85) }, "window_stop_percent"},
		{"window_stop_percent above maximum", func(c *BurnGuardConfig) { c.PaceLineWindowStopPercent = 95.1 },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.PaceLineWindowStopPercent, 85) }, "window_stop_percent"},
		{"reading_freshness at minimum is valid", func(c *BurnGuardConfig) { c.PaceLineReadingFreshnessMinutes = 1 },
			func(t *testing.T, c BurnGuardConfig) { eqInt(t, c.PaceLineReadingFreshnessMinutes, 1) }, ""},
		{"reading_freshness at maximum is valid", func(c *BurnGuardConfig) { c.PaceLineReadingFreshnessMinutes = 1440 },
			func(t *testing.T, c BurnGuardConfig) { eqInt(t, c.PaceLineReadingFreshnessMinutes, 1440) }, ""},
		{"reading_freshness zero is unset", func(c *BurnGuardConfig) { c.PaceLineReadingFreshnessMinutes = 0 },
			func(t *testing.T, c BurnGuardConfig) { eqInt(t, c.PaceLineReadingFreshnessMinutes, 60) }, ""},
		{"reading_freshness over maximum", func(c *BurnGuardConfig) { c.PaceLineReadingFreshnessMinutes = 1441 },
			func(t *testing.T, c BurnGuardConfig) { eqInt(t, c.PaceLineReadingFreshnessMinutes, 60) }, "reading_freshness_minutes"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := DefaultBurnGuard()
			if tc.edit != nil {
				tc.edit(&in)
			}
			var lines []string
			out := normalizeBurnGuard(in, func(f string, a ...interface{}) { lines = append(lines, sprintf(f, a...)) })
			tc.check(t, out)
			joined := strings.Join(lines, "\n")
			if tc.warn == "" && len(lines) > 0 {
				t.Errorf("unexpected warning: %s", joined)
			}
			if tc.warn != "" && !strings.Contains(joined, tc.warn) {
				t.Errorf("warning %q not found in %q", tc.warn, joined)
			}
		})
	}
}

// The one shared validator: every accepted setting passes, and a refusal is
// one error naming the offending key and its allowed range.
func TestValidateBurnGuardPaceLine(t *testing.T) {
	ok := DefaultBurnGuard()
	if err := ValidateBurnGuardPaceLine(ok); err != nil {
		t.Fatalf("defaults refused: %v", err)
	}
	ok.PaceLineMode = "" // unset mode reads shadow and is accepted
	if err := ValidateBurnGuardPaceLine(ok); err != nil {
		t.Fatalf("unset mode refused: %v", err)
	}
	// Boundary values are valid on every key.
	boundary := DefaultBurnGuard()
	boundary.PaceLineMode = "off"
	boundary.PaceLineWeekTolerance = 0
	boundary.PaceLinePointsToWeekFactor = 1
	boundary.PaceLineWindowPercentPerPoint = 1
	boundary.PaceLineWindowStopPercent = 50
	boundary.PaceLineReadingFreshnessMinutes = 1
	if err := ValidateBurnGuardPaceLine(boundary); err != nil {
		t.Fatalf("boundary values refused: %v", err)
	}
	boundary = DefaultBurnGuard()
	boundary.PaceLineMode = "on"
	boundary.PaceLineWeekTolerance = 10
	boundary.PaceLinePointsToWeekFactor = 10
	boundary.PaceLineWindowPercentPerPoint = 25
	boundary.PaceLineWindowStopPercent = 95
	boundary.PaceLineReadingFreshnessMinutes = 1440
	if err := ValidateBurnGuardPaceLine(boundary); err != nil {
		t.Fatalf("upper boundary values refused: %v", err)
	}

	tests := []struct {
		name  string
		edit  func(c *BurnGuardConfig)
		wants []string // substrings all expected in the one error
	}{
		{"mode refused", func(c *BurnGuardConfig) { c.PaceLineMode = "auto" },
			[]string{"usage.burn_guard.mode", "off, shadow, on"}},
		{"week_tolerance below", func(c *BurnGuardConfig) { c.PaceLineWeekTolerance = -1 },
			[]string{"usage.burn_guard.week_tolerance", "0 to 10"}},
		{"week_tolerance above", func(c *BurnGuardConfig) { c.PaceLineWeekTolerance = 11 },
			[]string{"usage.burn_guard.week_tolerance", "0 to 10"}},
		{"points_to_week_factor below", func(c *BurnGuardConfig) { c.PaceLinePointsToWeekFactor = 0 },
			[]string{"usage.burn_guard.points_to_week_factor", "1 to 10"}},
		{"points_to_week_factor above", func(c *BurnGuardConfig) { c.PaceLinePointsToWeekFactor = 10.1 },
			[]string{"usage.burn_guard.points_to_week_factor", "1 to 10"}},
		{"points_to_week_factor NaN", func(c *BurnGuardConfig) { c.PaceLinePointsToWeekFactor = math.NaN() },
			[]string{"usage.burn_guard.points_to_week_factor"}},
		{"points_to_week_factor infinite", func(c *BurnGuardConfig) { c.PaceLinePointsToWeekFactor = math.Inf(1) },
			[]string{"usage.burn_guard.points_to_week_factor"}},
		{"window_percent_per_point below", func(c *BurnGuardConfig) { c.PaceLineWindowPercentPerPoint = 0.5 },
			[]string{"usage.burn_guard.window_percent_per_point", "1 to 25"}},
		{"window_percent_per_point above", func(c *BurnGuardConfig) { c.PaceLineWindowPercentPerPoint = 25.5 },
			[]string{"usage.burn_guard.window_percent_per_point", "1 to 25"}},
		{"window_stop_percent below", func(c *BurnGuardConfig) { c.PaceLineWindowStopPercent = 49 },
			[]string{"usage.burn_guard.window_stop_percent", "50 to 95"}},
		{"window_stop_percent above", func(c *BurnGuardConfig) { c.PaceLineWindowStopPercent = 96 },
			[]string{"usage.burn_guard.window_stop_percent", "50 to 95"}},
		{"reading_freshness below", func(c *BurnGuardConfig) { c.PaceLineReadingFreshnessMinutes = 0 },
			[]string{"usage.burn_guard.reading_freshness_minutes", "1 to 1440"}},
		{"reading_freshness above", func(c *BurnGuardConfig) { c.PaceLineReadingFreshnessMinutes = 1441 },
			[]string{"usage.burn_guard.reading_freshness_minutes", "1 to 1440"}},
		{"several bad keys in one clear error",
			func(c *BurnGuardConfig) {
				c.PaceLineMode, c.PaceLineWeekTolerance, c.PaceLineWindowStopPercent = "auto", 99, 40
			},
			[]string{"usage.burn_guard.mode", "usage.burn_guard.week_tolerance", "usage.burn_guard.window_stop_percent"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := DefaultBurnGuard()
			tc.edit(&c)
			err := ValidateBurnGuardPaceLine(c)
			if err == nil {
				t.Fatalf("accepted %+v, want a refusal", c)
			}
			for _, want := range tc.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %q", err, want)
				}
			}
		})
	}
}

func TestBurnGuardPaceLineYAMLRoundTrip(t *testing.T) {
	in := DefaultBurnGuard()
	in.PaceLineMode = "on"
	in.PaceLineWeekTolerance = 5
	in.PaceLinePointsToWeekFactor = 2
	in.PaceLineWindowPercentPerPoint = 6
	in.PaceLineWindowStopPercent = 90
	in.PaceLineReadingFreshnessMinutes = 120
	data, err := yaml.Marshal(UsageConfig{BurnGuard: in})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, key := range []string{
		"mode:", "week_tolerance:", "points_to_week_factor:",
		"window_percent_per_point:", "window_stop_percent:", "reading_freshness_minutes:",
	} {
		if !strings.Contains(text, key) {
			t.Errorf("yaml lacks %s key:\n%s", key, text)
		}
	}
	var back UsageConfig
	if err := yaml.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	got := normalizeBurnGuard(back.BurnGuard, nil)
	if !reflect.DeepEqual(got, in) {
		t.Errorf("round trip = %+v, want %+v", got, in)
	}
}

func TestBurnGuardPaceLineYAMLPartialKeepsDefaults(t *testing.T) {
	cfg := DefaultConfig()
	y := "usage:\n  burn_guard:\n    mode: off\n    window_stop_percent: 90\n"
	if err := yaml.Unmarshal([]byte(y), cfg); err != nil {
		t.Fatal(err)
	}
	got := normalizeBurnGuard(cfg.Usage.BurnGuard, nil)
	want := DefaultBurnGuard()
	want.PaceLineMode = "off"
	want.PaceLineWindowStopPercent = 90
	if !reflect.DeepEqual(got, want) {
		t.Errorf("partial yaml = %+v, want %+v", got, want)
	}
}

func eqStr(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
