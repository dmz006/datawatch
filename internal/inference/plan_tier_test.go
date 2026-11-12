package inference

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// startingTable is the operator-policy starting table
// (autonomous.plan_tier_max_inflight defaults): pro=1, max_5x=2,
// max_20x=4, api=0 (unlimited — the dollar budget is a later phase).
func startingTable(t *testing.T) map[string]int {
	t.Helper()
	return map[string]int{"pro": 1, "max_5x": 2, "max_20x": 4, "api": 0}
}

func TestEffectiveMaxInflight_TableLookup(t *testing.T) {
	table := startingTable(t)
	cases := []struct {
		tier PlanTier
		want int
	}{
		{PlanTierPro, 1},
		{PlanTierMax5x, 2},
		{PlanTierMax20x, 4},
		{PlanTierAPI, 0},
	}
	for _, c := range cases {
		l := LLM{Name: "cc", Kind: KindClaudeCode, PlanTier: c.tier}
		if got := EffectiveMaxInflight(l, table); got != c.want {
			t.Errorf("PlanTier=%s: got %d, want %d", c.tier, got, c.want)
		}
	}
}

func TestEffectiveMaxInflight_TableOverrideWins(t *testing.T) {
	// Operator overrode the policy table; the derived value follows.
	table := startingTable(t)
	table["pro"] = 7
	if got := EffectiveMaxInflight(LLM{Name: "cc", Kind: KindClaudeCode, PlanTier: PlanTierPro}, table); got != 7 {
		t.Fatalf("overridden pro table: got %d, want 7", got)
	}
	// A tier absent from the table falls back to the kind default.
	if got := EffectiveMaxInflight(LLM{Name: "aider", Kind: KindAider, PlanTier: PlanTierAPI}, map[string]int{"pro": 3}); got != 1 {
		t.Fatalf("tier missing from table: got %d, want subscription default 1", got)
	}
	// A nil table also falls back to the kind default.
	if got := EffectiveMaxInflight(LLM{Name: "cc", Kind: KindClaudeCode, PlanTier: PlanTierPro}, nil); got != 1 {
		t.Fatalf("nil table: got %d, want subscription default 1", got)
	}
}

func TestEffectiveMaxInflight_ExplicitOverrideWins(t *testing.T) {
	table := startingTable(t)
	l := LLM{Name: "cc", Kind: KindClaudeCode, PlanTier: PlanTierPro, MaxInflight: 9}
	if got := EffectiveMaxInflight(l, table); got != 9 {
		t.Fatalf("explicit max_inflight=9: got %d, want 9", got)
	}
	// Explicit override beats the api=0 (unlimited) tier entry too.
	l = LLM{Name: "cc", Kind: KindClaudeCode, PlanTier: PlanTierAPI, MaxInflight: 2}
	if got := EffectiveMaxInflight(l, table); got != 2 {
		t.Fatalf("explicit max_inflight=2 over api tier: got %d, want 2", got)
	}
}

func TestEffectiveMaxInflight_UnsetDefault_SubscriptionKinds(t *testing.T) {
	table := startingTable(t)
	for _, k := range []Kind{KindClaudeCode, KindAider, KindGoose, KindPi, KindPiPrompt, KindOpenCodeACP, KindOpenCodePrompt, KindShell} {
		if got := EffectiveMaxInflight(LLM{Name: "x", Kind: k}, table); got != 1 {
			t.Errorf("kind %s, tier unset: got %d, want conservative 1", k, got)
		}
	}
}

func TestEffectiveMaxInflight_UnsetDefault_LocalKindsUnlimited(t *testing.T) {
	table := startingTable(t)
	for _, k := range []Kind{KindOllama, KindOpenWebUI, KindOpenCode} {
		if got := EffectiveMaxInflight(LLM{Name: "x", Kind: k}, table); got != 0 {
			t.Errorf("local kind %s, tier unset: got %d, want 0 (unlimited; node: pools govern)", k, got)
		}
	}
}

func TestEffectiveMaxInflight_UnsetDefault_APISessionKindsUnlimited(t *testing.T) {
	// Dollar-metered API kinds stay unlimited without a cap: the dollar
	// budget is a later phase and MaxInflight remains the operator's tool.
	table := startingTable(t)
	for _, k := range []Kind{KindClaude, KindGemini, KindGeminiAPI, KindOpenCodeAPI} {
		if got := EffectiveMaxInflight(LLM{Name: "x", Kind: k}, table); got != 0 {
			t.Errorf("api kind %s, tier unset: got %d, want 0 (unlimited)", k, got)
		}
	}
}

func TestLLMValidate_PlanTier(t *testing.T) {
	base := func(t PlanTier) *LLM { return &LLM{Name: "cc", Kind: KindClaudeCode, PlanTier: t} }
	if err := base("").Validate(); err != nil {
		t.Fatalf("empty plan_tier must be valid (unset), got %v", err)
	}
	for _, v := range []PlanTier{PlanTierPro, PlanTierMax5x, PlanTierMax20x, PlanTierAPI} {
		if err := base(v).Validate(); err != nil {
			t.Errorf("plan_tier %q must be valid, got %v", v, err)
		}
	}
	err := base("ultra").Validate()
	if err == nil {
		t.Fatal("unknown plan_tier must be rejected")
	}
	if !strings.Contains(err.Error(), "plan_tier") {
		t.Fatalf("error must name plan_tier, got %q", err)
	}
	err = base("PRO").Validate()
	if err == nil {
		t.Fatalf("plan_tier is case-sensitive: %q must be rejected", "PRO")
	}
}

func TestLLM_PlanTier_YAMLAndJSONRoundTrip(t *testing.T) {
	l := LLM{Name: "cc", Kind: KindClaudeCode, PlanTier: PlanTierMax20x, MaxInflight: 3}

	yb, err := yaml.Marshal(l)
	if err != nil {
		t.Fatalf("yaml marshal: %v", err)
	}
	if !strings.Contains(string(yb), "plan_tier: max_20x") {
		t.Fatalf("yaml must carry plan_tier, got:\n%s", yb)
	}
	var backY LLM
	if err := yaml.Unmarshal(yb, &backY); err != nil {
		t.Fatalf("yaml unmarshal: %v", err)
	}
	if backY.PlanTier != PlanTierMax20x || backY.MaxInflight != 3 {
		t.Fatalf("yaml round-trip mismatch: %+v", backY)
	}

	jb, err := json.Marshal(l)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	if !strings.Contains(string(jb), `"plan_tier":"max_20x"`) {
		t.Fatalf("json must carry plan_tier, got: %s", jb)
	}
	var backJ LLM
	if err := json.Unmarshal(jb, &backJ); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if backJ.PlanTier != PlanTierMax20x {
		t.Fatalf("json round-trip mismatch: %+v", backJ)
	}

	// Unset must serialize away (omitempty) and read back empty.
	unset := LLM{Name: "cc", Kind: KindOllama}
	ujb, _ := json.Marshal(unset)
	if strings.Contains(string(ujb), "plan_tier") {
		t.Fatalf("unset plan_tier must be omitted from json, got: %s", ujb)
	}
	var backU LLM
	if err := json.Unmarshal(ujb, &backU); err != nil {
		t.Fatalf("json unmarshal unset: %v", err)
	}
	if backU.PlanTier != PlanTierUnset {
		t.Fatalf("unset round-trip mismatch: %q", backU.PlanTier)
	}
}
