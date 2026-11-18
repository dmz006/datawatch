// The six pace_line settings in the CLI usage burn-guard config: a value inside
// its range is forwarded as-is, and anything outside (or not a number) is
// refused before a PUT is sent.

package main

import (
	"strings"
	"testing"
)

func TestBurnGuardPaceCLIAcceptsRanges(t *testing.T) {
	for _, tc := range []struct {
		text string
		key  string
		want float64
	}{
		{"week_tolerance=0", "week_tolerance", 0},
		{"week_tolerance=2", "week_tolerance", 2},
		{"week_tolerance=10", "week_tolerance", 10},
		{"points_to_week_factor=1", "points_to_week_factor", 1},
		{"points_to_week_factor=1.3", "points_to_week_factor", 1.3},
		{"points_to_week_factor=10", "points_to_week_factor", 10},
		{"window_percent_per_point=1", "window_percent_per_point", 1},
		{"window_percent_per_point=25", "window_percent_per_point", 25},
		{"window_stop_percent=50", "window_stop_percent", 50},
		{"window_stop_percent=85", "window_stop_percent", 85},
		{"window_stop_percent=95", "window_stop_percent", 95},
		{"reading_freshness_minutes=1", "reading_freshness_minutes", 1},
		{"reading_freshness_minutes=1440", "reading_freshness_minutes", 1440},
	} {
		body, err := parseBurnGuardSettings([]string{tc.text})
		if err != nil {
			t.Fatalf("%s: err = %v", tc.text, err)
		}
		if got, _ := body[tc.key].(float64); got != tc.want {
			t.Fatalf("%s: body = %v, want %v", tc.text, body[tc.key], tc.want)
		}
	}
	for _, tc := range []struct{ text, key, want string }{
		{"mode=off", "mode", "off"},
		{"mode=shadow", "mode", "shadow"},
		{"mode=on", "mode", "on"},
		{"mode=ON", "mode", "ON"}, // matching is case-insensitive, like the other choices
	} {
		body, err := parseBurnGuardSettings([]string{tc.text})
		if err != nil {
			t.Fatalf("%s: err = %v", tc.text, err)
		}
		if got := body[tc.key]; got != tc.want {
			t.Fatalf("%s: body = %v, want %q", tc.text, got, tc.want)
		}
	}
}

func TestBurnGuardPaceCLIRefusesInvalid(t *testing.T) {
	for _, bad := range []string{
		"mode=loud",
		"week_tolerance=-1", "week_tolerance=11", "week_tolerance=2.5", "week_tolerance=many",
		"points_to_week_factor=0.9", "points_to_week_factor=11", "points_to_week_factor=abc",
		"window_percent_per_point=0.5", "window_percent_per_point=26",
		"window_stop_percent=49", "window_stop_percent=96",
		"reading_freshness_minutes=0", "reading_freshness_minutes=1441", "reading_freshness_minutes=60.5", "reading_freshness_minutes=",
	} {
		_, err := parseBurnGuardSettings([]string{bad})
		if err == nil {
			t.Fatalf("%q accepted, want a refusal", bad)
		}
		key := bad
		if i := strings.IndexByte(key, '='); i >= 0 {
			key = key[:i]
		}
		if !strings.Contains(err.Error(), key) {
			t.Fatalf("%q: error = %q, want it to name the key %q", bad, err.Error(), key)
		}
	}
}
