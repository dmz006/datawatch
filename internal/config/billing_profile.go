package config

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Billing profiles (usage.billing) describe the operator's pricing model per
// provider, so the cost surfaces can label a provider as a bundled
// subscription, metered API spend, or free local inference instead of
// guessing from the price fields.
//
// usage.billing is an open map keyed by provider (claude-code, openai,
// gemini, or any other operator-chosen key). Every field is optional; a
// provider with no profile at all resolves to the assumed default profile
// below, and that assumption is flagged so surfaces can label it.

// The mode and allowance unit vocabularies.
const (
	// BillingModeBundledPlan: a fixed monthly subscription (e.g. Claude
	// Pro / Max) whose allowance resets on a schedule.
	BillingModeBundledPlan = "bundled_plan"
	// BillingModeAPIMetered: pay-per-token API spend with no allowance.
	BillingModeAPIMetered = "api_metered"
	// BillingModeFreeLocal: local inference (ollama etc.) at zero cost.
	BillingModeFreeLocal = "free_local"

	// Allowance units for the weekly allowance of a bundled plan.
	BillingUnitPercent  = "percent"
	BillingUnitRequests = "requests"
	BillingUnitTokens   = "tokens"
)

// DefaultBillingMode and DefaultBillingUnit are the assumed values when no
// profile (or no field of a profile) is set. The mode default is an
// *assumption*, not a fact about the provider: surfaces that show billing
// state must label api_metered as assumed whenever IsDefault (or the empty
// mode) applies.
const (
	DefaultBillingMode   = BillingModeAPIMetered
	DefaultBillingUnit   = BillingUnitPercent
	billingWeeksPerMonth = 52.0 / 12.0 // four-and-a-bit weekly allowances per month
)

// BillingProfile is one provider's billing model within usage.billing.
type BillingProfile struct {
	// Mode is bundled_plan | api_metered | free_local. Empty resolves to
	// the assumed default (api_metered) via EffectiveBillingProfile.
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`

	// Plan is free text naming the subscription plan. For the claude-code
	// provider the accepted plans are pro, max_5x and max_20x — the same
	// inference.PlanTier vocabulary the LLM entries' plan_tier uses.
	Plan string `yaml:"plan,omitempty" json:"plan,omitempty"`

	// MonthlyPrice is the monthly price of the plan in Currency. Must be a
	// finite number >= 0 when present (0 is allowed: a free plan).
	MonthlyPrice float64 `yaml:"monthly_price,omitempty" json:"monthly_price,omitempty"`

	// Currency is the ISO 4217 code of MonthlyPrice (USD, EUR, ...).
	Currency string `yaml:"currency,omitempty" json:"currency,omitempty"`

	// ResetWeekday and ResetTime are the plan's weekly allowance reset,
	// optional. Same vocabulary as usage.calculator: a weekday name
	// (sunday..saturday) and a 24-hour HH:MM local clock time.
	ResetWeekday string `yaml:"reset_weekday,omitempty" json:"reset_weekday,omitempty"`
	ResetTime    string `yaml:"reset_time,omitempty" json:"reset_time,omitempty"`

	// AllowanceUnit is percent | requests | tokens (default percent): the
	// unit the weekly allowance is counted in.
	AllowanceUnit string `yaml:"allowance_unit,omitempty" json:"allowance_unit,omitempty"`
}

// DefaultBillingProfile returns the assumed profile used for a provider with
// no usage.billing entry. Callers must keep the IsDefault flag (see
// EffectiveBillingProfile) so the assumed mode is labelable.
func DefaultBillingProfile() BillingProfile {
	return BillingProfile{Mode: DefaultBillingMode, AllowanceUnit: DefaultBillingUnit}
}

// EffectiveBillingProfile couples a BillingProfile with the flag that tells
// a surface whether the profile was configured or assumed.
type EffectiveBillingProfile struct {
	Profile   BillingProfile
	IsDefault bool // true when the provider has no usage.billing entry at all
}

// EffectiveBillingProfile returns the billing profile for provider (an open
// key: claude-code, openai, gemini or anything else the operator set). With
// no configured profile it returns the assumed default profile, flagged via
// IsDefault. Empty mode and allowance_unit of a configured profile resolve
// to their defaults (the profile itself remains operator-set, so IsDefault
// stays false).
func (u UsageConfig) EffectiveBillingProfile(provider string) EffectiveBillingProfile {
	p, ok := u.Billing[provider]
	if !ok {
		return EffectiveBillingProfile{Profile: DefaultBillingProfile(), IsDefault: true}
	}
	if p.Mode == "" {
		p.Mode = DefaultBillingMode // assumed default, profile itself is set
	}
	if p.AllowanceUnit == "" {
		p.AllowanceUnit = DefaultBillingUnit
	}
	return EffectiveBillingProfile{Profile: p, IsDefault: false}
}

// PointCostEquivalent returns this profile's per-point monthly cost
// equivalent, as an estimate: the value of one point (1% of the weekly
// allowance) in monthly currency units.
//
// point value = monthly_price / (weekly allowances per month) / 100
//
// with 52 weeks / 12 months of allowances per month. This is only an
// estimate: it assumes the plan's allowance is fully used each week and
// ignores overage, mid-cycle prorating and any non-percent allowance unit.
func (p BillingProfile) PointCostEquivalent() float64 {
	if math.IsNaN(p.MonthlyPrice) || math.IsInf(p.MonthlyPrice, 0) {
		return math.NaN()
	}
	return p.MonthlyPrice / billingWeeksPerMonth / 100
}

// EffectivePlanTierAllocationUSD resolves the dollar allocation for a plan
// tier (pro, max_5x, max_20x — inference.PlanTier vocabulary), reconciling
// the two places the operator may have recorded it:
//
//   - autonomous.plan_tier_allocation_usd — explicit per-tier table; always
//     wins for the tier it names.
//   - usage.billing.<provider>.plan — when the profile names a tier as its
//     plan, its monthly_price stands in for that tier's allocation.
//
// Precedence is therefore: the explicit table entry > a billing profile's
// monthly_price. The values are *derived*, never copied: the billing map
// stays the only storage of the profile's price and the profile is never
// written into plan_tier_allocation_usd, so the two cannot drift. When
// several configured profiles name the same tier with different prices the
// lowest monthly_price wins (the conservative figure for spend math); the
// provider key is the tie-breaker.
//
// ok is false when neither source declares the tier's allocation.
func (c *Config) EffectivePlanTierAllocationUSD(tier string) (float64, bool) {
	if usd, ok := c.Autonomous.PlanTierAllocationUSD[tier]; ok {
		return usd, true
	}
	best := math.MaxFloat64
	found := false
	// Deterministic walk: provider keys in sorted order, lowest price wins.
	names := make([]string, 0, len(c.Usage.Billing))
	for provider := range c.Usage.Billing {
		names = append(names, provider)
	}
	sort.Strings(names)
	for _, provider := range names {
		p := c.Usage.Billing[provider]
		if !strings.EqualFold(strings.TrimSpace(p.Plan), tier) {
			continue
		}
		if math.IsNaN(p.MonthlyPrice) || math.IsInf(p.MonthlyPrice, 0) {
			continue
		}
		if !found || p.MonthlyPrice < best {
			best, found = p.MonthlyPrice, true
		}
	}
	if !found {
		return 0, false
	}
	return best, true
}

// validateBillingProfiles checks every usage.billing entry. It runs in the
// config validate path next to the plan_tier_* checks: unknown mode,
// negative or non-finite monthly_price, a bad reset weekday or time and an
// unknown allowance unit are refused, as is an empty provider key.
func validateBillingProfiles(cfg *Config) error {
	for provider, p := range cfg.Usage.Billing {
		if strings.TrimSpace(provider) == "" {
			return fmt.Errorf("usage.billing: provider key must not be empty")
		}
		what := "usage.billing." + provider
		mode := p.Mode
		if mode == "" {
			mode = DefaultBillingMode // resolved assumption, always valid
		}
		switch mode {
		case BillingModeBundledPlan, BillingModeAPIMetered, BillingModeFreeLocal:
		default:
			return fmt.Errorf("%s.mode: unknown billing mode %q (allowed: %s, %s, %s)",
				what, p.Mode, BillingModeBundledPlan, BillingModeAPIMetered, BillingModeFreeLocal)
		}
		if p.MonthlyPrice < 0 || math.IsNaN(p.MonthlyPrice) || math.IsInf(p.MonthlyPrice, 0) {
			return fmt.Errorf("%s.monthly_price: %v must be a finite number >= 0", what, p.MonthlyPrice)
		}
		if p.ResetWeekday != "" && !containsString(usageWeekdays, p.ResetWeekday) {
			return fmt.Errorf("%s.reset_weekday: unknown weekday %q (allowed: %s)",
				what, p.ResetWeekday, strings.Join(usageWeekdays, ", "))
		}
		if p.ResetTime != "" && !usageClockPattern.MatchString(p.ResetTime) {
			return fmt.Errorf("%s.reset_time: %q must be a 24-hour HH:MM clock time", what, p.ResetTime)
		}
		unit := p.AllowanceUnit
		if unit == "" {
			unit = DefaultBillingUnit // resolved assumption, always valid
		}
		switch unit {
		case BillingUnitPercent, BillingUnitRequests, BillingUnitTokens:
		default:
			return fmt.Errorf("%s.allowance_unit: unknown allowance unit %q (allowed: %s, %s, %s)",
				what, p.AllowanceUnit, BillingUnitPercent, BillingUnitRequests, BillingUnitTokens)
		}
	}
	return nil
}

// containsString reports whether s is present in a small string list (the
// usage calculator keeps its weekday/clock validation data unexported, so
// the billing profile checks reuse it).
func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
