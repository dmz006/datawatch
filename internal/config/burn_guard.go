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
	// Ladder rung names, in the order the default ladder applies them. They are
	// entry-generic: each rung acts on the ladder's entry, not on any fixed backend.
	BurnGuardRungLowerInflight = "lower_inflight"
	BurnGuardRungRouteEntry    = "route_entry"
	BurnGuardRungHoldEntry     = "hold_entry"

	// Legacy first-version rung names (now route_entry and hold_entry). They are
	// still accepted in saved configs and normalize to the entry-generic names
	// (burnGuardLegacyRungs).
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
	// (default lower_inflight, route_entry, hold_entry). The legacy first-version
	// names route_pi_prompt and hold_claude are accepted and normalize to
	// route_entry and hold_entry.
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
	// Entries are the paid LLM entries the guard evaluates, one projection and one
	// ladder each. Empty means one entry built from the settings above (see
	// EffectiveEntries), so a config written before per-entry guarding keeps working.
	Entries []BurnGuardEntryConfig `yaml:"entries,omitempty" json:"entries,omitempty"`
}

// Plan types of a guarded entry.
const (
	// BurnGuardPlanFlat is a flat plan read as a weekly percentage of an allowance.
	BurnGuardPlanFlat = "flat"
	// BurnGuardPlanMetered is a metered plan read as a spend figure in dollars.
	BurnGuardPlanMetered = "metered"
	// BurnGuardUsageSourceReadings reads the entry's usage from the readings series.
	BurnGuardUsageSourceReadings = "readings"
	// BurnGuardUsageSourceNone means the entry has no usage reading source; it is
	// never held.
	BurnGuardUsageSourceNone = "none"

	// BurnGuardLegacyEntryDefault and BurnGuardLegacyRouteToDefault name the entry
	// and the route target of the single entry built when no entries are configured
	// (the guard acted on these before per-entry guarding). They are configuration
	// defaults only; the guard reads every name from the entry.
	BurnGuardLegacyEntryDefault   = "claude-code"
	BurnGuardLegacyRouteToDefault = "pi-prompt"
)

// BurnGuardEntryConfig is one paid LLM entry the guard evaluates.
type BurnGuardEntryConfig struct {
	// LLM is the LLM entry name (the backend name admission uses).
	LLM string `yaml:"llm" json:"llm"`
	// UsageSource is where the entry's usage is read: readings (the readings series,
	// readings that name this entry) or none. An entry with no source is never held.
	UsageSource string `yaml:"usage_source" json:"usage_source"`
	// Plan is flat (a weekly percentage) or metered (a spend figure). Default flat.
	Plan string `yaml:"plan" json:"plan"`
	// PeriodTarget is the ceiling of the period: a percentage for a flat plan
	// (default 100, plus bought extra credit) or dollars for a metered plan
	// (required; a metered entry with no target is never held).
	PeriodTarget float64 `yaml:"period_target" json:"period_target"`
	// Model is the model whose per-model cost rate prices the entry's tokens; empty
	// means the entry's first model.
	Model string `yaml:"model,omitempty" json:"model,omitempty"`
	// Ladder is this entry's ordered rungs; empty means the guard-wide ladder.
	Ladder []string `yaml:"ladder,omitempty" json:"ladder,omitempty"`
	// HeldModels limits the hold rung to these models of the entry; empty holds the
	// whole entry.
	HeldModels []string `yaml:"held_models,omitempty" json:"held_models,omitempty"`
	// RouteTo is the backend the route rung sends new work to; empty means the
	// route rung does nothing for this entry.
	RouteTo string `yaml:"route_to,omitempty" json:"route_to,omitempty"`
	// PointsPerUSD converts a flat entry's dollar-priced tokens to points of its
	// weekly allowance; 0 means price the tokens with the guard-wide cache
	// coefficients (the fitted ones) instead.
	PointsPerUSD float64 `yaml:"points_per_usd,omitempty" json:"points_per_usd,omitempty"`
}

// HasUsageSource reports whether the entry has a usage reading source.
func (e BurnGuardEntryConfig) HasUsageSource() bool {
	return strings.EqualFold(strings.TrimSpace(e.UsageSource), BurnGuardUsageSourceReadings)
}

// IsMetered reports whether the entry is on a metered plan.
func (e BurnGuardEntryConfig) IsMetered() bool {
	return strings.EqualFold(strings.TrimSpace(e.Plan), BurnGuardPlanMetered)
}

// EffectiveEntries returns the entries the guard evaluates: the configured ones with a
// name, or, when none is configured, one flat entry reading the readings series with
// the guard-wide ladder (the behaviour before per-entry guarding). The first entry owns
// the readings that name no entry.
func (c BurnGuardConfig) EffectiveEntries() []BurnGuardEntryConfig {
	var out []BurnGuardEntryConfig
	seen := map[string]bool{}
	for _, e := range c.Entries {
		e.LLM = strings.TrimSpace(e.LLM)
		if e.LLM == "" || seen[e.LLM] {
			continue
		}
		seen[e.LLM] = true
		if strings.TrimSpace(e.Plan) == "" {
			e.Plan = BurnGuardPlanFlat
		}
		out = append(out, e)
	}
	if len(out) > 0 {
		return out
	}
	return []BurnGuardEntryConfig{{
		LLM: BurnGuardLegacyEntryDefault, UsageSource: BurnGuardUsageSourceReadings,
		Plan: BurnGuardPlanFlat, RouteTo: BurnGuardLegacyRouteToDefault,
	}}
}

var (
	burnGuardRungs    = []string{BurnGuardRungLowerInflight, BurnGuardRungRouteEntry, BurnGuardRungHoldEntry}
	burnGuardCeilings = []string{BurnGuardCeilingLow, BurnGuardCeilingMedium, BurnGuardCeilingHigh, BurnGuardCeilingCheapBackend}
)

// burnGuardLegacyRungs maps legacy first-version rung names to their
// entry-generic names.
var burnGuardLegacyRungs = map[string]string{
	BurnGuardRungRoutePiPrompt: BurnGuardRungRouteEntry,
	BurnGuardRungHoldClaude:    BurnGuardRungHoldEntry,
}

// burnGuardRungName resolves one rung name: the entry-generic name, or the
// entry-generic name a legacy first-version name maps to. ok is false when the
// name is not a rung at all.
func burnGuardRungName(r string) (string, bool) {
	name, ok := usageChoice(r, burnGuardRungs)
	if ok {
		return name, true
	}
	if name, ok2 := burnGuardLegacyRungs[r]; ok2 {
		return name, true
	}
	return r, false
}

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

	// Ladder: keep valid, non-duplicate rungs in order (legacy first-version
	// names normalize to the entry-generic names); an empty result is the
	// default ladder.
	out.Ladder = nil
	seen := map[string]bool{}
	for _, r := range c.Ladder {
		name, ok := burnGuardRungName(r)
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

	// Entries: an entry's ladder keeps valid rungs (empty means the guard-wide
	// ladder); an unknown plan or source is reported and read as flat / none.
	if len(c.Entries) > 0 {
		out.Entries = make([]BurnGuardEntryConfig, 0, len(c.Entries))
		for i, e := range c.Entries {
			// An entry's ladder keeps valid rungs, legacy names included (empty means
			// the guard-wide ladder).
			var rungs []string
			for _, r := range e.Ladder {
				if name, ok := burnGuardRungName(r); ok {
					rungs = append(rungs, name)
				} else {
					say("[warn] usage.burn_guard.entries[%d].ladder: %q is not a rung — dropped", i, r)
				}
			}
			e.Ladder = rungs
			if p := strings.TrimSpace(e.Plan); p != "" && !strings.EqualFold(p, BurnGuardPlanFlat) && !strings.EqualFold(p, BurnGuardPlanMetered) {
				say("[warn] usage.burn_guard.entries[%d].plan: %q — using %q", i, e.Plan, BurnGuardPlanFlat)
				e.Plan = BurnGuardPlanFlat
			}
			if src := strings.TrimSpace(e.UsageSource); src != "" && !e.HasUsageSource() && !strings.EqualFold(src, BurnGuardUsageSourceNone) {
				say("[warn] usage.burn_guard.entries[%d].usage_source: %q — using %q", i, e.UsageSource, BurnGuardUsageSourceNone)
				e.UsageSource = BurnGuardUsageSourceNone
			}
			if !burnGuardFinite(e.PeriodTarget) || e.PeriodTarget < 0 {
				say("[warn] usage.burn_guard.entries[%d].period_target: %v — unset", i, e.PeriodTarget)
				e.PeriodTarget = 0
			}
			out.Entries = append(out.Entries, e)
		}
	}

	if c.RetryLimitClaude < 0 || c.RetryLimitClaude > BurnGuardRetryLimitClaudeMax {
		say("[warn] usage.burn_guard.retry_limit_claude: %d — using %d", c.RetryLimitClaude, d.RetryLimitClaude)
		out.RetryLimitClaude = d.RetryLimitClaude
	}
	return out
}

// BurnGuardRungs returns the allowed ladder rung names (the entry-generic names), in
// default order.
func BurnGuardRungs() []string { return append([]string(nil), burnGuardRungs...) }

// ValidateBurnGuardRung reports an error for a name that is not a ladder rung. The
// legacy first-version names count: they normalize to the entry-generic names.
func ValidateBurnGuardRung(name string) error {
	if _, ok := burnGuardRungName(name); !ok {
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
