package config

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBurnGuardDefaults(t *testing.T) {
	got := DefaultConfig().Usage.BurnGuard
	want := BurnGuardConfig{
		Enabled: true, IntervalMinutes: 60, MarginPoints: 2.0, MaxProjectionGapPoints: 25,
		Ladder:   []string{"lower_inflight", "route_pi_prompt", "hold_claude"},
		StepBack: true, CacheWritePointsPerMillion: 0.373, CacheReadPointsPerMillion: 0.0069,
		RefitOnReading:   true,
		EffortCeiling:    map[string]string{"docs": "low", "parity": "low", "live_check": "low", "final_gate": "low"},
		RetryLimitClaude: 2,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("default usage.burn_guard = %+v, want %+v", got, want)
	}
	if n := normalizeBurnGuard(got, nil); !reflect.DeepEqual(n, got) {
		t.Errorf("normalising defaults changed them: %+v", n)
	}
	// Defaults must not share state between calls.
	a, b := DefaultBurnGuard(), DefaultBurnGuard()
	a.Ladder[0] = "x"
	a.EffortCeiling["docs"] = "high"
	if b.Ladder[0] != "lower_inflight" || b.EffortCeiling["docs"] != "low" {
		t.Error("DefaultBurnGuard shares slice or map between calls")
	}
}

func TestBurnGuardNormalize(t *testing.T) {
	d := DefaultBurnGuard()
	nan := math.NaN()
	tests := []struct {
		name  string
		edit  func(c *BurnGuardConfig)
		check func(t *testing.T, c BurnGuardConfig)
		warn  string // substring expected in a warning; "" means none
	}{
		{"zero struct fills numerics", func(c *BurnGuardConfig) { *c = BurnGuardConfig{} },
			func(t *testing.T, c BurnGuardConfig) {
				if c.IntervalMinutes != 60 || c.CacheWritePointsPerMillion != 0.373 || len(c.Ladder) != 3 {
					t.Errorf("zero struct not defaulted: %+v", c)
				}
			}, ""},
		{"interval negative", func(c *BurnGuardConfig) { c.IntervalMinutes = -5 },
			func(t *testing.T, c BurnGuardConfig) { eqInt(t, c.IntervalMinutes, 60) }, "interval_minutes"},
		{"interval too large", func(c *BurnGuardConfig) { c.IntervalMinutes = 5000 },
			func(t *testing.T, c BurnGuardConfig) { eqInt(t, c.IntervalMinutes, 60) }, "interval_minutes"},
		{"margin zero is valid", func(c *BurnGuardConfig) { c.MarginPoints = 0 },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.MarginPoints, 0) }, ""},
		{"margin negative", func(c *BurnGuardConfig) { c.MarginPoints = -1 },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.MarginPoints, 2) }, "margin_points"},
		{"projection gap zero is valid (never compare)", func(c *BurnGuardConfig) { c.MaxProjectionGapPoints = 0 },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.MaxProjectionGapPoints, 0) }, ""},
		{"projection gap negative", func(c *BurnGuardConfig) { c.MaxProjectionGapPoints = -1 },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.MaxProjectionGapPoints, 25) }, "max_projection_gap_points"},
		{"projection gap NaN", func(c *BurnGuardConfig) { c.MaxProjectionGapPoints = nan },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.MaxProjectionGapPoints, 25) }, "max_projection_gap_points"},
		{"projection gap infinite", func(c *BurnGuardConfig) { c.MaxProjectionGapPoints = math.Inf(1) },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.MaxProjectionGapPoints, 25) }, "max_projection_gap_points"},
		{"projection gap over maximum", func(c *BurnGuardConfig) { c.MaxProjectionGapPoints = 101 },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.MaxProjectionGapPoints, 25) }, "max_projection_gap_points"},
		{"projection gap at maximum is valid", func(c *BurnGuardConfig) { c.MaxProjectionGapPoints = 100 },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.MaxProjectionGapPoints, 100) }, ""},
		{"margin NaN", func(c *BurnGuardConfig) { c.MarginPoints = nan },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.MarginPoints, 2) }, "margin_points"},
		{"margin too large", func(c *BurnGuardConfig) { c.MarginPoints = 99 },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.MarginPoints, 2) }, "margin_points"},
		{"cache write negative", func(c *BurnGuardConfig) { c.CacheWritePointsPerMillion = -0.1 },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.CacheWritePointsPerMillion, 0.373) }, "cache_write"},
		{"cache read infinite", func(c *BurnGuardConfig) { c.CacheReadPointsPerMillion = math.Inf(1) },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.CacheReadPointsPerMillion, 0.0069) }, "cache_read"},
		{"cache custom kept", func(c *BurnGuardConfig) { c.CacheReadPointsPerMillion = 0.01 },
			func(t *testing.T, c BurnGuardConfig) { eqF(t, c.CacheReadPointsPerMillion, 0.01) }, ""},
		{"ladder unknown rung dropped", func(c *BurnGuardConfig) { c.Ladder = []string{"hold_claude", "bogus", "Lower_Inflight"} },
			func(t *testing.T, c BurnGuardConfig) {
				if !reflect.DeepEqual(c.Ladder, []string{"hold_claude", "lower_inflight"}) {
					t.Errorf("ladder = %v", c.Ladder)
				}
			}, "bogus"},
		{"ladder duplicate dropped", func(c *BurnGuardConfig) { c.Ladder = []string{"hold_claude", "hold_claude"} },
			func(t *testing.T, c BurnGuardConfig) {
				if !reflect.DeepEqual(c.Ladder, []string{"hold_claude"}) {
					t.Errorf("ladder = %v", c.Ladder)
				}
			}, "repeated"},
		{"ladder all invalid becomes default", func(c *BurnGuardConfig) { c.Ladder = []string{"nope"} },
			func(t *testing.T, c BurnGuardConfig) {
				if !reflect.DeepEqual(c.Ladder, d.Ladder) {
					t.Errorf("ladder = %v", c.Ladder)
				}
			}, "nope"},
		{"ceiling invalid value", func(c *BurnGuardConfig) { c.EffortCeiling = map[string]string{"docs": "extreme", "x": "HIGH"} },
			func(t *testing.T, c BurnGuardConfig) {
				if c.EffortCeiling["docs"] != "low" || c.EffortCeiling["x"] != "high" || len(c.EffortCeiling) != 2 {
					t.Errorf("ceiling = %v", c.EffortCeiling)
				}
			}, "extreme"},
		{"ceiling blank kind dropped", func(c *BurnGuardConfig) { c.EffortCeiling = map[string]string{" ": "low"} },
			func(t *testing.T, c BurnGuardConfig) {
				if !reflect.DeepEqual(c.EffortCeiling, d.EffortCeiling) {
					t.Errorf("ceiling = %v", c.EffortCeiling)
				}
			}, "blank task kind"},
		{"retry limit negative", func(c *BurnGuardConfig) { c.RetryLimitClaude = -1 },
			func(t *testing.T, c BurnGuardConfig) { eqInt(t, c.RetryLimitClaude, 2) }, "retry_limit_claude"},
		{"retry limit zero is valid", func(c *BurnGuardConfig) { c.RetryLimitClaude = 0 },
			func(t *testing.T, c BurnGuardConfig) { eqInt(t, c.RetryLimitClaude, 0) }, ""},
		{"booleans taken as given", func(c *BurnGuardConfig) { c.Enabled, c.StepBack, c.RefitOnReading = false, false, false },
			func(t *testing.T, c BurnGuardConfig) {
				if c.Enabled || c.StepBack || c.RefitOnReading {
					t.Errorf("booleans changed: %+v", c)
				}
			}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := DefaultBurnGuard()
			tc.edit(&in)
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

func TestBurnGuardYAMLRoundTrip(t *testing.T) {
	in := DefaultBurnGuard()
	in.Enabled = false
	in.IntervalMinutes = 30
	in.MarginPoints = 0
	in.Ladder = []string{"hold_claude"}
	in.StepBack = false
	in.RefitOnReading = false
	in.RetryLimitClaude = 0
	in.EffortCeiling = map[string]string{"docs": "cheaper_backend"}
	data, err := yaml.Marshal(UsageConfig{BurnGuard: in})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "burn_guard:") {
		t.Errorf("yaml lacks burn_guard key:\n%s", data)
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

func TestBurnGuardAskOnChangeDefaultAndRoundTrip(t *testing.T) {
	if d := DefaultBurnGuard(); d.AskOnChange {
		t.Fatal("AskOnChange default = true, want false")
	}
	for _, v := range []bool{true, false} {
		in := DefaultBurnGuard()
		in.AskOnChange = v
		data, err := yaml.Marshal(UsageConfig{BurnGuard: in})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if !strings.Contains(string(data), "ask_on_change:") {
			t.Errorf("yaml lacks explicit ask_on_change key:\n%s", data)
		}
		var back UsageConfig
		if err := yaml.Unmarshal(data, &back); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got := normalizeBurnGuard(back.BurnGuard, nil); got.AskOnChange != v {
			t.Errorf("AskOnChange round trip = %v, want %v", got.AskOnChange, v)
		}
	}
}

func TestBurnGuardYAMLPartialKeepsDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if err := yaml.Unmarshal([]byte("usage:\n  burn_guard:\n    interval_minutes: 15\n"), cfg); err != nil {
		t.Fatal(err)
	}
	g := normalizeBurnGuard(cfg.Usage.BurnGuard, nil)
	want := DefaultBurnGuard()
	want.IntervalMinutes = 15
	if !reflect.DeepEqual(g, want) {
		t.Errorf("partial yaml = %+v, want %+v", g, want)
	}
}

func eqInt(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}

func eqF(t *testing.T, got, want float64) {
	t.Helper()
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestBurnGuardProjectionGapSurfaceValues pins the max_projection_gap_points rule the
// config layer applies on load: the default is 25, zero is kept (never compare), and a
// negative, NaN or over-maximum value is replaced by the default with a warning.
func TestBurnGuardProjectionGapSurfaceValues(t *testing.T) {
	if got := DefaultBurnGuard().MaxProjectionGapPoints; got != 25 || BurnGuardProjectionGapDefault != 25 {
		t.Fatalf("default max_projection_gap_points = %v, want 25", got)
	}
	cases := []struct {
		name string
		v    float64
		want float64
		warn bool
	}{
		{"zero kept", 0, 0, false},
		{"at maximum kept", BurnGuardProjectionGapMax, BurnGuardProjectionGapMax, false},
		{"negative degraded", -1, 25, true},
		{"NaN degraded", math.NaN(), 25, true},
		{"over maximum degraded", BurnGuardProjectionGapMax + 1, 25, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := DefaultBurnGuard()
			in.MaxProjectionGapPoints = tc.v
			var lines []string
			out := normalizeBurnGuard(in, func(f string, a ...interface{}) { lines = append(lines, sprintf(f, a...)) })
			eqF(t, out.MaxProjectionGapPoints, tc.want)
			joined := strings.Join(lines, "\n")
			if tc.warn && !strings.Contains(joined, "max_projection_gap_points") {
				t.Errorf("no warning naming max_projection_gap_points: %q", joined)
			}
			if !tc.warn && len(lines) > 0 {
				t.Errorf("unexpected warning: %q", joined)
			}
		})
	}
}
