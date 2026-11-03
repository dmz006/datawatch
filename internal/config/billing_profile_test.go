package config

import (
	"math"
	"path/filepath"
	"strings"
	"testing"
)

// Billing profile tests: defaults, validation, open provider keys,
// reconciliation with plan_tier_allocation_usd and the point-value math.
// Only temp dirs and in-memory fakes — no network, no real config home.

func testBillingConfig(t *testing.T) *Config {
	t.Helper()
	// Home the config might touch, isolated to a temp dir.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	return &Config{}
}

func TestEffectiveBillingProfileAssumedDefault(t *testing.T) {
	if got := (UsageConfig{}).EffectiveBillingProfile("claude-code"); !got.IsDefault {
		t.Fatalf("provider without a profile must be flagged IsDefault, got %+v", got)
	}
	if got := gotProfile(t, (UsageConfig{}).EffectiveBillingProfile("no-such-provider")); got.Mode != BillingModeAPIMetered {
		t.Fatalf("assumed mode = %q, want %q", got.Mode, BillingModeAPIMetered)
	}
	if got := gotProfile(t, (UsageConfig{}).EffectiveBillingProfile("no-such-provider")); got.AllowanceUnit != BillingUnitPercent {
		t.Fatalf("assumed unit = %q, want %q", got.AllowanceUnit, BillingUnitPercent)
	}
}

func gotProfile(t *testing.T, e EffectiveBillingProfile) BillingProfile {
	t.Helper()
	return e.Profile
}

func TestEffectiveBillingProfileConfigured(t *testing.T) {
	u := UsageConfig{Billing: map[string]BillingProfile{
		"claude-code": {
			Mode:         BillingModeBundledPlan,
			Plan:         "max_5x",
			MonthlyPrice: 100,
			Currency:     "USD",
			ResetWeekday: "friday",
			ResetTime:    "16:00",
		},
	}}
	got := u.EffectiveBillingProfile("claude-code")
	if got.IsDefault {
		t.Fatal("configured profile must not be flagged IsDefault")
	}
	p := got.Profile
	if p.Mode != BillingModeBundledPlan || p.Plan != "max_5x" || p.MonthlyPrice != 100 {
		t.Fatalf("profile round-trip wrong: %+v", p)
	}
	if p.Currency != "USD" || p.ResetWeekday != "friday" || p.ResetTime != "16:00" {
		t.Fatalf("optional fields lost: %+v", p)
	}
	// Empty mode / unit of a configured profile resolve to the defaults,
	// but the profile is still the operator's (not an assumed default).
	u.Billing["gemini"] = BillingProfile{MonthlyPrice: 30}
	got = u.EffectiveBillingProfile("gemini")
	if got.IsDefault {
		t.Fatal("partially configured profile must not be IsDefault")
	}
	if got.Profile.Mode != BillingModeAPIMetered {
		t.Fatalf("empty mode should resolve to %q, got %q", BillingModeAPIMetered, got.Profile.Mode)
	}
	if got.Profile.AllowanceUnit != BillingUnitPercent {
		t.Fatalf("empty unit should resolve to %q, got %q", BillingUnitPercent, got.Profile.AllowanceUnit)
	}
	// A provider with no entry never picks up anyone else's profile.
	if !u.EffectiveBillingProfile("openai").IsDefault {
		t.Fatal("unconfigured provider must fall back to the assumed default")
	}
}

func TestDefaultBillingProfileIsAssumedAPI(t *testing.T) {
	d := DefaultBillingProfile()
	if d.Mode != BillingModeAPIMetered || d.AllowanceUnit != BillingUnitPercent {
		t.Fatalf("DefaultBillingProfile = %+v, want mode %s unit %s", d, BillingModeAPIMetered, BillingUnitPercent)
	}
}

func TestValidateBillingProfilesErrors(t *testing.T) {
	cases := []struct {
		name     string
		profile  BillingProfile
		provider string
		want     string
	}{
		{"unknown mode", BillingProfile{Mode: "enterprise"}, "claude-code", "mode"},
		{"negative price", BillingProfile{MonthlyPrice: -5}, "openai", "monthly_price"},
		{"nan price", BillingProfile{MonthlyPrice: math.NaN()}, "openai", "monthly_price"},
		{"inf price", BillingProfile{MonthlyPrice: math.Inf(1)}, "openai", "monthly_price"},
		{"bad weekday", BillingProfile{ResetWeekday: "funday"}, "gemini", "reset_weekday"},
		{"bad time", BillingProfile{ResetTime: "24:99"}, "gemini", "reset_time"},
		{"bad time 12h", BillingProfile{ResetTime: "8:5"}, "gemini", "reset_time"},
		{"unknown unit", BillingProfile{AllowanceUnit: "dollars"}, "claude-code", "allowance_unit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testBillingConfig(t)
			cfg.Usage.Billing = map[string]BillingProfile{tc.provider: tc.profile}
			err := validateBillingProfiles(cfg)
			if err == nil {
				t.Fatalf("want error for %+v, got nil", tc.profile)
			}
			if !containsFold(t, err.Error(), tc.want) {
				t.Fatalf("error %q should mention %q", err.Error(), tc.want)
			}
		})
	}

	t.Run("empty provider key", func(t *testing.T) {
		cfg := testBillingConfig(t)
		cfg.Usage.Billing = map[string]BillingProfile{"": {Mode: BillingModeFreeLocal}}
		if err := validateBillingProfiles(cfg); err == nil {
			t.Fatal("want error for empty provider key, got nil")
		}
	})
}

func containsFold(t *testing.T, s, sub string) bool {
	t.Helper()
	return strings.Contains(s, sub)
}

func TestValidateBillingProfilesOpenKeys(t *testing.T) {
	cfg := testBillingConfig(t)
	cfg.Usage.Billing = map[string]BillingProfile{
		"claude-code": {
			Mode:         BillingModeBundledPlan,
			Plan:         "pro",
			MonthlyPrice: 20,
			Currency:     "USD",
			ResetWeekday: "monday",
			ResetTime:    "00:00",
		},
		"openai": {Mode: BillingModeAPIMetered, MonthlyPrice: 0},
		"gemini": {Mode: BillingModeAPIMetered},
		"llama":  {Mode: BillingModeFreeLocal, AllowanceUnit: BillingUnitTokens},
		"my-vpc": {Mode: BillingModeAPIMetered, AllowanceUnit: BillingUnitRequests},
		"ollama": {Mode: BillingModeFreeLocal},
	}
	if err := validateBillingProfiles(cfg); err != nil {
		t.Fatalf("open provider keys must all validate, got %v", err)
	}
	// All three allowance units are legal.
	cfg.Usage.Billing["a"] = BillingProfile{AllowanceUnit: BillingUnitPercent}
	cfg.Usage.Billing["b"] = BillingProfile{AllowanceUnit: BillingUnitRequests}
	cfg.Usage.Billing["c"] = BillingProfile{AllowanceUnit: BillingUnitTokens}
	if err := validateBillingProfiles(cfg); err != nil {
		t.Fatalf("all allowance units must validate, got %v", err)
	}
}

func TestValidateBillingProfilesEmptyOK(t *testing.T) {
	cfg := testBillingConfig(t)
	if err := validateBillingProfiles(cfg); err != nil {
		t.Fatalf("no profiles configured must validate, got %v", err)
	}
}

func TestReconciliationDerivedFromBillingProfile(t *testing.T) {
	cfg := testBillingConfig(t)
	cfg.Usage.Billing = map[string]BillingProfile{
		"claude-code": {Mode: BillingModeBundledPlan, Plan: "max_5x", MonthlyPrice: 100},
	}
	// No explicit table entry: the profile's monthly_price stands in.
	got, ok := cfg.EffectivePlanTierAllocationUSD("max_5x")
	if !ok || got != 100 {
		t.Fatalf("derived allocation = (%v, %v), want (100, true)", got, ok)
	}
	// Untiered plan does not invent an allocation for any tier.
	cfg2 := testBillingConfig(t)
	cfg2.Usage.Billing = map[string]BillingProfile{"claude-code": {Plan: "custom-slice"}}
	if _, ok := cfg2.EffectivePlanTierAllocationUSD("pro"); ok {
		t.Fatal("non-tier plan must not derive an allocation")
	}
}

func TestReconciliationExplicitTableWins(t *testing.T) {
	cfg := testBillingConfig(t)
	cfg.Autonomous.PlanTierAllocationUSD = map[string]float64{"max_5x": 200}
	cfg.Usage.Billing = map[string]BillingProfile{
		"claude-code": {Plan: "max_5x", MonthlyPrice: 100},
	}
	got, ok := cfg.EffectivePlanTierAllocationUSD("max_5x")
	if !ok || got != 200 {
		t.Fatalf("explicit entry must win: got (%v, %v), want (200, true)", got, ok)
	}
}

func TestReconciliationNeverDuplicatesStorage(t *testing.T) {
	cfg := testBillingConfig(t)
	cfg.Usage.Billing = map[string]BillingProfile{
		"claude-code": {Plan: "max_5x", MonthlyPrice: 100},
	}
	if _, ok := cfg.EffectivePlanTierAllocationUSD("max_5x"); !ok {
		t.Fatal("expected derived allocation")
	}
	// Resolving must not copy the value into the table: the billing map
	// stays the only storage of the price (no drift, no double counting).
	if len(cfg.Autonomous.PlanTierAllocationUSD) != 0 {
		t.Fatalf("resolver duplicated storage into plan_tier_allocation_usd: %v", cfg.Autonomous.PlanTierAllocationUSD)
	}
}

func TestReconciliationLowestPriceWins(t *testing.T) {
	cfg := testBillingConfig(t)
	cfg.Usage.Billing = map[string]BillingProfile{
		"claude-code":      {Plan: "max_5x", MonthlyPrice: 100},
		"claude-code-corp": {Plan: "max_5x", MonthlyPrice: 200},
		"other":            {Plan: "pro", MonthlyPrice: 5},
	}
	got, ok := cfg.EffectivePlanTierAllocationUSD("max_5x")
	if !ok || got != 100 {
		t.Fatalf("lowest monthly_price should win: got (%v, %v), want (100, true)", got, ok)
	}
	got, ok = cfg.EffectivePlanTierAllocationUSD("pro")
	if !ok || got != 5 {
		t.Fatalf("pro allocation = (%v, %v), want (5, true)", got, ok)
	}
}

func TestPointCostEquivalentMath(t *testing.T) {
	// point = monthly_price / (52/12) / 100  ≡  monthly_price * 12 / (52 * 100)
	cases := []struct {
		monthly float64
		want    float64
	}{
		{20, 20 * 12.0 / 52 / 100},   // ≈ 0.046154
		{100, 100.0 * 12 / 52 / 100}, // ≈ 1.923077
		{0, 0},
		{40, 40 * 12.0 / 52 / 100},
	}
	for _, tc := range cases {
		p := BillingProfile{MonthlyPrice: tc.monthly}
		got := p.PointCostEquivalent()
		if math.Abs(got-tc.want) > 1e-12 {
			t.Fatalf("PointCostEquivalent(%v) = %v, want %v", tc.monthly, got, tc.want)
		}
	}
	// The 4.333 weekly-allowances-per-month factor is visible in the ratio.
	a := BillingProfile{MonthlyPrice: 20}.PointCostEquivalent()
	b := BillingProfile{MonthlyPrice: 100}.PointCostEquivalent()
	if math.Abs(b/a-5) > 1e-12 {
		t.Fatalf("5x price should give 5x point value: %v vs %v", b, a)
	}
	// A non-finite price yields NaN (an estimate that is unusable, not 0).
	if !math.IsNaN(BillingProfile{MonthlyPrice: math.Inf(1)}.PointCostEquivalent()) {
		t.Fatal("inf monthly price should give a NaN estimate")
	}
}
