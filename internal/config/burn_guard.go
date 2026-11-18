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

	// pace_line policy modes: off never fires, shadow computes and records the
	// decision without touching anything, on backs claude-code off until the
	// estimate is back inside the line and the window.
	BurnGuardModeOff    = "off"
	BurnGuardModeShadow = "shadow"
	BurnGuardModeOn     = "on"

	// pace_line defaults and bounds. An unset mode reads shadow: shadow
	// first, then on.
	BurnGuardModeDefault                    = "shadow"
	BurnGuardWeekToleranceDefault           = 2
	BurnGuardWeekToleranceMax               = 10
	BurnGuardPointsToWeekFactorDefault      = 1.3
	BurnGuardPointsToWeekFactorMin          = 1.0
	BurnGuardPointsToWeekFactorMax          = 10.0
	BurnGuardWindowPercentPerPointDefault   = 4.0
	BurnGuardWindowPercentPerPointMin       = 1.0
	BurnGuardWindowPercentPerPointMax       = 25.0
	BurnGuardWindowStopPercentDefault       = 85.0
	BurnGuardWindowStopPercentMin           = 50.0
	BurnGuardWindowStopPercentMax           = 95.0
	BurnGuardReadingFreshnessMinutesDefault = 60
	BurnGuardReadingFreshnessMinutesMax     = 1440
)

// BurnGuardConfig is the usage.burn_guard section. Booleans and numerics carry
// no omitempty so an explicit false or 0 survives a save and reload.
//
// The pace_line policy settings (mode, week_tolerance,
// points_to_week_factor, window_percent_per_point, window_stop_percent,
// reading_freshness_minutes) sit flat in this same section, beside the
// existing guard settings; settings only, the guard's behavior is unchanged
// until the pace_line story lands. The window anchors (windows ending at
// 02:00, 07:00 and 12:00, then every 5 hours) are fixed behavior, not a
// setting.
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

	// PaceLineMode is the pace_line policy mode: off | shadow | on
	// (unset reads shadow; an empty string is stored and read as shadow).
	PaceLineMode string `yaml:"mode" json:"mode"`
	// PaceLineWeekTolerance is how many points the week reading may run over
	// the pace line and still be ok, whole number 0 to 10 (default 2).
	PaceLineWeekTolerance int `yaml:"week_tolerance" json:"week_tolerance"`
	// PaceLinePointsToWeekFactor counts one point of ledger burn as this many
	// points of the weekly allowance, number 1 to 10 (default 1.3).
	PaceLinePointsToWeekFactor float64 `yaml:"points_to_week_factor" json:"points_to_week_factor"`
	// PaceLineWindowPercentPerPoint is how much of the current window one
	// point burned is, number 1 to 25 (default 4).
	PaceLineWindowPercentPerPoint float64 `yaml:"window_percent_per_point" json:"window_percent_per_point"`
	// PaceLineWindowStopPercent is the percent used at which a window is
	// over, number 50 to 95 (default 85).
	PaceLineWindowStopPercent float64 `yaml:"window_stop_percent" json:"window_stop_percent"`
	// PaceLineReadingFreshnessMinutes is how long a reading may be before it
	// is stale and the guard asks the operator for a fresh one, whole number
	// 1 to 1440 (default 60).
	PaceLineReadingFreshnessMinutes int `yaml:"reading_freshness_minutes" json:"reading_freshness_minutes"`
}

var burnGuardModes = []string{BurnGuardModeOff, BurnGuardModeShadow, BurnGuardModeOn}

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
		// pace_line policy: unset mode reads shadow — shadow first, then on.
		PaceLineMode:                    BurnGuardModeDefault,
		PaceLineWeekTolerance:           BurnGuardWeekToleranceDefault,
		PaceLinePointsToWeekFactor:      BurnGuardPointsToWeekFactorDefault,
		PaceLineWindowPercentPerPoint:   BurnGuardWindowPercentPerPointDefault,
		PaceLineWindowStopPercent:       BurnGuardWindowStopPercentDefault,
		PaceLineReadingFreshnessMinutes: BurnGuardReadingFreshnessMinutesDefault,
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

	// pace_line policy: an unset mode reads shadow; an unset (zero) value takes
	// its default with no warning, like the price settings; a set value out of
	// range degrades to the default with a warning.
	mode := strings.TrimSpace(c.PaceLineMode)
	if mode == "" {
		out.PaceLineMode = d.PaceLineMode
	} else if m, ok := usageChoice(mode, burnGuardModes); !ok {
		say("[warn] usage.burn_guard.mode: %q is not a mode — using %q (allowed: %s)", c.PaceLineMode, d.PaceLineMode, strings.Join(burnGuardModes, ", "))
		out.PaceLineMode = d.PaceLineMode
	} else {
		out.PaceLineMode = m
	}
	if c.PaceLineWeekTolerance < 0 || c.PaceLineWeekTolerance > BurnGuardWeekToleranceMax {
		say("[warn] usage.burn_guard.week_tolerance: %d — using %d (allowed: 0 to %d)", c.PaceLineWeekTolerance, d.PaceLineWeekTolerance, BurnGuardWeekToleranceMax)
		out.PaceLineWeekTolerance = d.PaceLineWeekTolerance
	}
	pnum := func(key string, v, lo, hi, def float64) float64 {
		if v == 0 { // unset, like the price settings
			return def
		}
		if burnGuardFinite(v) && v >= lo && v <= hi {
			return v
		}
		say("[warn] usage.burn_guard.%s: %v — using %v (allowed: %g to %g)", key, v, def, lo, hi)
		return def
	}
	out.PaceLinePointsToWeekFactor = pnum("points_to_week_factor", c.PaceLinePointsToWeekFactor, BurnGuardPointsToWeekFactorMin, BurnGuardPointsToWeekFactorMax, d.PaceLinePointsToWeekFactor)
	out.PaceLineWindowPercentPerPoint = pnum("window_percent_per_point", c.PaceLineWindowPercentPerPoint, BurnGuardWindowPercentPerPointMin, BurnGuardWindowPercentPerPointMax, d.PaceLineWindowPercentPerPoint)
	out.PaceLineWindowStopPercent = pnum("window_stop_percent", c.PaceLineWindowStopPercent, BurnGuardWindowStopPercentMin, BurnGuardWindowStopPercentMax, d.PaceLineWindowStopPercent)
	if c.PaceLineReadingFreshnessMinutes == 0 { // unset, kept silent like the factors
		out.PaceLineReadingFreshnessMinutes = d.PaceLineReadingFreshnessMinutes
	} else if c.PaceLineReadingFreshnessMinutes > BurnGuardReadingFreshnessMinutesMax {
		say("[warn] usage.burn_guard.reading_freshness_minutes: %d — using %d (allowed: 1 to %d)", c.PaceLineReadingFreshnessMinutes, d.PaceLineReadingFreshnessMinutes, BurnGuardReadingFreshnessMinutesMax)
		out.PaceLineReadingFreshnessMinutes = d.PaceLineReadingFreshnessMinutes
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

// BurnGuardModes returns the allowed pace_line mode names, in order:
// off, shadow, on.
func BurnGuardModes() []string { return append([]string(nil), burnGuardModes...) }

// IsBurnGuardMode reports whether name is a pace_line mode (off, shadow, on),
// case-insensitively, blank names rejected.
func IsBurnGuardMode(name string) bool {
	_, ok := usageChoice(name, burnGuardModes)
	return ok
}

// ValidateBurnGuardPaceLine is the one rule for the pace_line settings —
// usage.burn_guard.mode, week_tolerance, points_to_week_factor,
// window_percent_per_point, window_stop_percent and
// reading_freshness_minutes — shared by the REST config path, the CLI and the
// comm channel. Call it with resolved settings (unset fields read as default
// first, as normalizeBurnGuard does): an empty mode is the unset default and
// is accepted. A refusal is one error naming each offending key and its
// allowed range.
func ValidateBurnGuardPaceLine(c BurnGuardConfig) error {
	var problems []string
	if m := strings.TrimSpace(c.PaceLineMode); m != "" && !IsBurnGuardMode(m) {
		problems = append(problems, fmt.Sprintf("usage.burn_guard.mode: %q is not a mode (allowed: %s)", c.PaceLineMode, strings.Join(burnGuardModes, ", ")))
	}
	if c.PaceLineWeekTolerance < 0 || c.PaceLineWeekTolerance > BurnGuardWeekToleranceMax {
		problems = append(problems, fmt.Sprintf("usage.burn_guard.week_tolerance: must be a whole number from 0 to %d", BurnGuardWeekToleranceMax))
	}
	num := func(key string, v, lo, hi float64) {
		if !burnGuardFinite(v) || v < lo || v > hi {
			problems = append(problems, fmt.Sprintf("usage.burn_guard.%s: must be from %g to %g", key, lo, hi))
		}
	}
	num("points_to_week_factor", c.PaceLinePointsToWeekFactor, BurnGuardPointsToWeekFactorMin, BurnGuardPointsToWeekFactorMax)
	num("window_percent_per_point", c.PaceLineWindowPercentPerPoint, BurnGuardWindowPercentPerPointMin, BurnGuardWindowPercentPerPointMax)
	num("window_stop_percent", c.PaceLineWindowStopPercent, BurnGuardWindowStopPercentMin, BurnGuardWindowStopPercentMax)
	if c.PaceLineReadingFreshnessMinutes < 1 || c.PaceLineReadingFreshnessMinutes > BurnGuardReadingFreshnessMinutesMax {
		problems = append(problems, fmt.Sprintf("usage.burn_guard.reading_freshness_minutes: must be a whole number from 1 to %d", BurnGuardReadingFreshnessMinutesMax))
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
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
