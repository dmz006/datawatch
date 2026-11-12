package inference

import "testing"

// InflightSourceOf / EffectiveMaxInflight agreement tests: the source
// must always explain the effective cap, and precedence must be
// explicit > plan_tier > kind.

func TestInflightSourceOf_Precedence(t *testing.T) {
	sub := LLM{Kind: KindClaudeCode} // subscription kind: default 1
	local := LLM{Kind: KindOllama}   // local kind: default 0 (unlimited)
	api := LLM{Kind: KindClaude}     // API kind: default 0 (unlimited)

	// 1. Explicit override always wins, even when the tier table
	// would give a different answer.
	explicit := sub
	explicit.MaxInflight = 7
	explicit.PlanTier = PlanTierMax5x
	table := map[string]int{string(PlanTierMax5x): 2}
	if got := EffectiveMaxInflight(explicit, table); got != 7 {
		t.Errorf("explicit: cap = %d, want 7", got)
	}
	if got := InflightSourceOf(explicit, table); got != InflightSourceExplicit {
		t.Errorf("explicit: source = %q, want explicit", got)
	}
	// Explicit with MaxInflight 0 (unset) does NOT count as explicit.
	zero := sub
	if got := InflightSourceOf(zero, nil); got != InflightSourceKind {
		t.Errorf("zero MaxInflight: source = %q, want kind", got)
	}

	// 2. Plan tier wins over the kind default when the tier is in the
	// table — including tier caps of 0 (unlimited) which must still
	// read as plan_tier-sourced.
	tiered := sub
	tiered.PlanTier = PlanTierMax5x
	caps := map[string]int{string(PlanTierMax5x): 2, string(PlanTierAPI): 0}
	if got := EffectiveMaxInflight(tiered, caps); got != 2 {
		t.Errorf("tier: cap = %d, want 2", got)
	}
	if got := InflightSourceOf(tiered, caps); got != InflightSourcePlanTier {
		t.Errorf("tier: source = %q, want plan_tier", got)
	}
	apiTiered := api
	apiTiered.PlanTier = PlanTierAPI
	if got := EffectiveMaxInflight(apiTiered, caps); got != 0 {
		t.Errorf("api tier cap 0: cap = %d, want 0", got)
	}
	if got := InflightSourceOf(apiTiered, caps); got != InflightSourcePlanTier {
		t.Errorf("api tier cap 0: source = %q, want plan_tier", got)
	}

	// 3. Tier not in the table (or nil table) falls back to kind.
	missing := sub
	missing.PlanTier = PlanTierMax20x
	if got := EffectiveMaxInflight(missing, caps); got != 1 {
		t.Errorf("missing tier: cap = %d, want 1 (kind default)", got)
	}
	if got := InflightSourceOf(missing, caps); got != InflightSourceKind {
		t.Errorf("missing tier: source = %q, want kind", got)
	}
	if got := InflightSourceOf(local, nil); got != InflightSourceKind {
		t.Errorf("local nil table: source = %q, want kind", got)
	}
	if got := EffectiveMaxInflight(local, nil); got != 0 {
		t.Errorf("local nil table: cap = %d, want 0", got)
	}
}
