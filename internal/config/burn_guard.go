// The usage.burn_guard block: settings for the guard that compares the
// projected weekly burn with the pace line and steps down a ladder of
// responses when the projection runs over. Normalisation follows
// normalizeUsageCalculator: an invalid saved value degrades to its default and
// logs a warning rather than failing daemon start.
package config

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

const (
	// Ladder rung names, in the order the default ladder applies them.
	BurnGuardRungLowerInflight = "lower_inflight"
	BurnGuardRungRoutePiPrompt = "route_pi_prompt"
	BurnGuardRungHoldClaude    = "hold_claude"

	// Effort ceilings: a task kind is capped at this effort, or sent to a
	// cheaper backend.
	BurnGuardCeilingLow          = "low"
	BurnGuardCeilingMedium       = "medium"
	BurnGuardCeilingHigh         = "high"
	BurnGuardCeilingCheapBackend = "cheaper_backend"

	BurnGuardIntervalMinutesDefault  = 60
	BurnGuardIntervalMinutesMax      = 1440
	BurnGuardMarginPointsDefault     = 2.0
	BurnGuardMarginPointsMax         = 50.0
	BurnGuardProjectionGapDefault    = 25.0
	BurnGuardProjectionGapMax        = 100.0
	BurnGuardCacheWriteDefault       = 0.373
	BurnGuardCacheReadDefault        = 0.0069
	BurnGuardPointsPerMillionMax     = 1000.0
	BurnGuardRetryLimitClaudeDefault = 2
	BurnGuardRetryLimitClaudeMax     = 20
)

// BurnGuardConfig is the usage.burn_guard section. Booleans and numerics carry
// no omitempty so an explicit false or 0 survives a save and reload.
type BurnGuardConfig struct {
	// Enabled turns the guard on (default true).
	Enabled bool `yaml:"enabled" json:"enabled"`
	// IntervalMinutes is how often the guard evaluates, 1 to 1440 (default 60).
	IntervalMinutes int `yaml:"interval_minutes" json:"interval_minutes"`
	// MarginPoints is how far the projection may run over the pace line
	// before the guard acts, 0 to 50 (default 2.0).
	MarginPoints float64 `yaml:"margin_points" json:"margin_points"`
	// MaxProjectionGapPoints is the largest gap, in points, allowed between
	// the projection and the pace line, 0 to 100 (default 25). 0 means never
	// compare: the gap check is off.
	MaxProjectionGapPoints float64 `yaml:"max_projection_gap_points" json:"max_projection_gap_points"`
	// Ladder is the ordered list of rungs applied while over the line
	// (default lower_inflight, route_pi_prompt, hold_claude).
	Ladder []string `yaml:"ladder,omitempty" json:"ladder,omitempty"`
	// StepBack undoes rungs in reverse once the projection is back under the
	// line (default true).
	StepBack bool `yaml:"step_back" json:"step_back"`
	// CacheWritePointsPerMillion and CacheReadPointsPerMillion price cache
	// tokens in weekly points per million tokens (defaults 0.373, 0.0069).
	CacheWritePointsPerMillion float64 `yaml:"cache_write_points_per_million" json:"cache_write_points_per_million"`
	CacheReadPointsPerMillion  float64 `yaml:"cache_read_points_per_million" json:"cache_read_points_per_million"`
	// RefitOnReading refits the cost model whenever a new usage reading
	// arrives (default true).
	RefitOnReading bool `yaml:"refit_on_reading" json:"refit_on_reading"`
	// EffortCeiling maps a task kind to its ceiling: low | medium | high |
	// cheaper_backend (default docs, parity, live_check, final_gate -> low).
	EffortCeiling map[string]string `yaml:"effort_ceiling,omitempty" json:"effort_ceiling,omitempty"`
	// RetryLimitClaude is the most retries of one task on Claude, 0 to 20
	// (default 2).
	RetryLimitClaude int `yaml:"retry_limit_claude" json:"retry_limit_claude"`
	// AskOnChange: when true, the guard asks one open question per rung
	// change; when false (default) it sends its alert only.
	AskOnChange bool `yaml:"ask_on_change" json:"ask_on_change"`
}

var (
	burnGuardRungs    = []string{BurnGuardRungLowerInflight, BurnGuardRungRoutePiPrompt, BurnGuardRungHoldClaude}
	burnGuardCeilings = []string{BurnGuardCeilingLow, BurnGuardCeilingMedium, BurnGuardCeilingHigh, BurnGuardCeilingCheapBackend}
)

// burnGuardLogf receives the warning lines raised while normalising a loaded
// config. Tests replace it to capture the output.
var burnGuardLogf = func(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// DefaultBurnGuard returns the burn guard settings applied when none are
// configured. Every call returns fresh slice and map values.
func DefaultBurnGuard() BurnGuardConfig {
	return BurnGuardConfig{
		Enabled:                    true,
		IntervalMinutes:            BurnGuardIntervalMinutesDefault,
		MarginPoints:               BurnGuardMarginPointsDefault,
		MaxProjectionGapPoints:     BurnGuardProjectionGapDefault,
		Ladder:                     append([]string(nil), burnGuardRungs...),
		StepBack:                   true,
		CacheWritePointsPerMillion: BurnGuardCacheWriteDefault,
		CacheReadPointsPerMillion:  BurnGuardCacheReadDefault,
		RefitOnReading:             true,
		EffortCeiling: map[string]string{
			"docs":       BurnGuardCeilingLow,
			"parity":     BurnGuardCeilingLow,
			"live_check": BurnGuardCeilingLow,
			"final_gate": BurnGuardCeilingLow,
		},
		RetryLimitClaude: BurnGuardRetryLimitClaudeDefault,
		AskOnChange:      false, // the guard alerts only; it does not ask questions
	}
}

func burnGuardFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// normalizeBurnGuard returns c with every invalid or unset value replaced by
// its default. Invalid (as opposed to unset) values are reported through warn.
// Enabled, StepBack, RefitOnReading and AskOnChange are taken as given:
// a config loads on top of DefaultConfig, so an unset key keeps its
// default.
func normalizeBurnGuard(c BurnGuardConfig, warn func(format string, args ...interface{})) BurnGuardConfig {
	d := DefaultBurnGuard()
	say := func(format string, args ...interface{}) {
		if warn != nil {
			warn(format, args...)
		}
	}
	out := c

	if c.IntervalMinutes == 0 {
		out.IntervalMinutes = d.IntervalMinutes
	} else if c.IntervalMinutes < 1 || c.IntervalMinutes > BurnGuardIntervalMinutesMax {
		say("[warn] usage.burn_guard.interval_minutes: %d — using %d", c.IntervalMinutes, d.IntervalMinutes)
		out.IntervalMinutes = d.IntervalMinutes
	}

	num := func(key string, v, max, def float64) float64 {
		if burnGuardFinite(v) && v >= 0 && v <= max {
			return v
		}
		say("[warn] usage.burn_guard.%s: %v — using %v", key, v, def)
		return def
	}
	out.MarginPoints = num("margin_points", c.MarginPoints, BurnGuardMarginPointsMax, d.MarginPoints)
	// Zero is valid (never compare); a negative, NaN, infinite or
	// over-maximum value is replaced by the default.
	out.MaxProjectionGapPoints = num("max_projection_gap_points", c.MaxProjectionGapPoints, BurnGuardProjectionGapMax, d.MaxProjectionGapPoints)
	// An unset price is the default quietly; zero is not a usable price.
	price := func(key string, v, def float64) float64 {
		if v == 0 {
			return def
		}
		if burnGuardFinite(v) && v > 0 && v <= BurnGuardPointsPerMillionMax {
			return v
		}
		say("[warn] usage.burn_guard.%s: %v — using %v", key, v, def)
		return def
	}
	out.CacheWritePointsPerMillion = price("cache_write_points_per_million", c.CacheWritePointsPerMillion, d.CacheWritePointsPerMillion)
	out.CacheReadPointsPerMillion = price("cache_read_points_per_million", c.CacheReadPointsPerMillion, d.CacheReadPointsPerMillion)

	// Ladder: keep valid, non-duplicate rungs in order; an empty result is
	// the default ladder.
	out.Ladder = nil
	seen := map[string]bool{}
	for _, r := range c.Ladder {
		name, ok := usageChoice(r, burnGuardRungs)
		switch {
		case !ok:
			say("[warn] usage.burn_guard.ladder: %q is not a rung — dropped (allowed: %s)", r, strings.Join(burnGuardRungs, ", "))
		case seen[name]:
			say("[warn] usage.burn_guard.ladder: %q repeated — dropped", r)
		default:
			seen[name] = true
			out.Ladder = append(out.Ladder, name)
		}
	}
	if len(out.Ladder) == 0 {
		out.Ladder = d.Ladder
	}

	// Effort ceiling: an empty map is the default; an invalid ceiling for a
	// kind degrades to low, and a blank kind is dropped.
	if len(c.EffortCeiling) == 0 {
		out.EffortCeiling = d.EffortCeiling
	} else {
		out.EffortCeiling = make(map[string]string, len(c.EffortCeiling))
		kinds := make([]string, 0, len(c.EffortCeiling))
		for k := range c.EffortCeiling {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		for _, k := range kinds {
			key := strings.TrimSpace(k)
			if key == "" {
				say("[warn] usage.burn_guard.effort_ceiling: blank task kind — dropped")
				continue
			}
			v := c.EffortCeiling[k]
			if got, ok := usageChoice(v, burnGuardCeilings); ok {
				out.EffortCeiling[key] = got
				continue
			}
			say("[warn] usage.burn_guard.effort_ceiling.%s: %q is not an allowed ceiling — using %q (allowed: %s)", key, v, BurnGuardCeilingLow, strings.Join(burnGuardCeilings, ", "))
			out.EffortCeiling[key] = BurnGuardCeilingLow
		}
		if len(out.EffortCeiling) == 0 {
			out.EffortCeiling = d.EffortCeiling
		}
	}

	if c.RetryLimitClaude < 0 || c.RetryLimitClaude > BurnGuardRetryLimitClaudeMax {
		say("[warn] usage.burn_guard.retry_limit_claude: %d — using %d", c.RetryLimitClaude, d.RetryLimitClaude)
		out.RetryLimitClaude = d.RetryLimitClaude
	}
	return out
}

// BurnGuardRungs returns the allowed ladder rung names, in default order.
func BurnGuardRungs() []string { return append([]string(nil), burnGuardRungs...) }

// ValidateBurnGuardRung reports an error for a name that is not a ladder rung.
func ValidateBurnGuardRung(name string) error {
	if _, ok := usageChoice(name, burnGuardRungs); !ok {
		return fmt.Errorf("usage.burn_guard.ladder: %q is not a rung (allowed: %s)", name, strings.Join(burnGuardRungs, ", "))
	}
	return nil
}

// ValidateMaxProjectionGapPoints is the one rule for usage.burn_guard.max_projection_gap_points,
// shared by the REST config path, the CLI and the comm channel: a finite number from 0 to
// BurnGuardProjectionGapMax. Zero is valid and means the projections are never compared.
func ValidateMaxProjectionGapPoints(f float64) error {
	if !burnGuardFinite(f) || f < 0 || f > BurnGuardProjectionGapMax {
		return fmt.Errorf("usage.burn_guard.max_projection_gap_points: must be from 0 to %g (0 never compares the projections)", BurnGuardProjectionGapMax)
	}
	return nil
}
