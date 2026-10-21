package main

import (
	"testing"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
)

// TestSyncLLMLimits_PlanTierDrivesLedgerLimit pins wiring behavior: the
// ledger limit for each "llm:<name>" pool is inference.EffectiveMaxInflight
// (explicit max_inflight > tier table > conservative kind default), not the
// raw max_inflight field. syncLLMLimits is the exact call syncPools makes,
// so this covers the ledger limit following the derived value and re-syncing
// on registry change (the 10s ticker re-runs syncPools, which re-runs this).
func TestSyncLLMLimits_PlanTierDrivesLedgerLimit(t *testing.T) {
	cfg := config.DefaultConfig() // carrying the operator-policy defaults
	reg := inference.NewRegistry()
	specs := []inference.LLM{
		{Name: "cc-unset", Kind: inference.KindClaudeCode},
		{Name: "cc-pro", Kind: inference.KindClaudeCode, PlanTier: inference.PlanTierPro},
		{Name: "cc-5x", Kind: inference.KindClaudeCode, PlanTier: inference.PlanTierMax5x},
		{Name: "cc-20x", Kind: inference.KindClaudeCode, PlanTier: inference.PlanTierMax20x},
		{Name: "cc-api", Kind: inference.KindClaudeCode, PlanTier: inference.PlanTierAPI},
		{Name: "cc-explicit", Kind: inference.KindClaudeCode, PlanTier: inference.PlanTierPro, MaxInflight: 9},
		{Name: "ollama-local", Kind: inference.KindOllama},
	}
	for i := range specs {
		if err := reg.Add(&specs[i]); err != nil {
			t.Fatalf("registry add %s: %v", specs[i].Name, err)
		}
	}

	led := capacity.New(capacity.Options{})
	syncLLMLimits(cfg, reg, led)

	cases := []struct {
		name string
		want int
	}{
		{"llm:cc-unset", 1},          // subscription kind, tier unset: conservative 1
		{"llm:cc-pro", 1},            // policy table pro=1
		{"llm:cc-5x", 2},             // policy table max_5x=2
		{"llm:cc-20x", 4},            // policy table max_20x=4
		{"llm:cc-api", 0},            // policy table api=0 (unlimited)
		{"llm:cc-explicit", 9},       // explicit max_inflight wins over the tier
		{"llm:ollama-local", 0},      // local kind: unlimited, node: pools govern
	}
	for _, c := range cases {
		if got := led.Limit(c.name); got != c.want {
			t.Errorf("Limit(%q) = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestSyncLLMLimits_TableOverrideFollows(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Autonomous.PlanTierMaxInflight = map[string]int{"pro": 7, "max_5x": 8, "max_20x": 9}
	reg := inference.NewRegistry()
	if err := reg.Add(&inference.LLM{Name: "cc", Kind: inference.KindClaudeCode, PlanTier: inference.PlanTierPro}); err != nil {
		t.Fatal(err)
	}
	led := capacity.New(capacity.Options{})
	syncLLMLimits(cfg, reg, led)
	if got := led.Limit("llm:cc"); got != 7 {
		t.Fatalf("Limit(llm:cc) = %d, want 7 (operator-overridden pro cap)", got)
	}
	// Re-sync on registry change: the same call reflects a new entry.
	if err := reg.Add(&inference.LLM{Name: "aider", Kind: inference.KindAider, PlanTier: inference.PlanTierMax20x}); err != nil {
		t.Fatal(err)
	}
	syncLLMLimits(cfg, reg, led)
	if got := led.Limit("llm:aider"); got != 9 {
		t.Fatalf("Limit(llm:aider) = %d, want 9 after re-sync", got)
	}
}
