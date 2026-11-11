package server

import (
	"context"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/burnguard"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
)

func billingModeServer(t *testing.T, llms ...*inference.LLM) *Server {
	t.Helper()
	reg := inference.NewRegistry()
	for _, l := range llms {
		if err := reg.Add(l); err != nil {
			t.Fatalf("add %s: %v", l.Name, err)
		}
	}
	return &Server{cfg: &config.Config{}, inferenceReg: reg}
}

func TestUsageUnitKeyPerMode(t *testing.T) {
	for mode, want := range map[string]string{
		config.BillingModeAPI:       UsageUnitDollars,
		config.BillingModeAllowance: UsageUnitPoints,
		config.BillingModeLocal:     UsageUnitNone,
	} {
		if got := UsageUnitKey(mode); got != want {
			t.Fatalf("UsageUnitKey(%q) = %q, want %q", mode, got, want)
		}
	}
	s := billingModeServer(t,
		&inference.LLM{Name: "claude-code", Kind: inference.KindClaudeCode},
		&inference.LLM{Name: "local-box", Kind: inference.KindOllama},
		&inference.LLM{Name: "metered", Kind: inference.KindClaude},
	)
	got := s.backendUnitKeys()
	if got["claude-code"] != UsageUnitPoints || got["local-box"] != UsageUnitNone || got["metered"] != UsageUnitDollars {
		t.Fatalf("unit keys = %v", got)
	}
}

func TestBudgetStatusesSkipLocalKeepAllowanceAndAPI(t *testing.T) {
	s := billingModeServer(t,
		&inference.LLM{Name: "claude-code", Kind: inference.KindClaudeCode},
		&inference.LLM{Name: "local-box", Kind: inference.KindOllama},
		&inference.LLM{Name: "metered", Kind: inference.KindClaude},
	)
	in := []autonomous.BudgetStatus{{LLM: "claude-code"}, {LLM: "local-box"}, {LLM: "metered"}}
	out := s.filterBudgetStatuses(in)
	if len(out) != 2 || out[0].LLM != "claude-code" || out[1].LLM != "metered" {
		t.Fatalf("filtered = %+v", out)
	}
}

// An allowance entry (claude-code) must behave exactly as before the billing
// mode change: the guard with the mode gate wired makes the same decisions and
// admission changes as the guard without it.
func TestBurnGuardAllowanceEntryBehavesAsBefore(t *testing.T) {
	cfg := rungFixtureConfig()
	base, baseAdm, _, _ := burnGuardRig(t, &cfg)
	gated, gatedAdm, _, _ := burnGuardRig(t, &cfg)
	s := billingModeServer(t, &inference.LLM{Name: "claude-code", Kind: inference.KindClaudeCode})
	gated.in.Applies = s.burnGuardApplies

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		a, b := base.Tick(ctx), gated.Tick(ctx)
		if a.Direction != b.Direction || a.Rung != b.Rung || a.Level != b.Level || len(a.Actions) != len(b.Actions) {
			t.Fatalf("tick %d: before %+v, after %+v", i, a, b)
		}
	}
	if baseAdm.cap != gatedAdm.cap || baseAdm.held["claude-code"] != gatedAdm.held["claude-code"] ||
		baseAdm.routes["claude-code"] != gatedAdm.routes["claude-code"] {
		t.Fatalf("admission differs: before cap=%d held=%v routes=%v, after cap=%d held=%v routes=%v",
			baseAdm.cap, baseAdm.held, baseAdm.routes, gatedAdm.cap, gatedAdm.held, gatedAdm.routes)
	}
	if gatedAdm.cap == 3 {
		t.Fatalf("allowance entry was not guarded: cap still %d", gatedAdm.cap)
	}
}

func TestBurnGuardIgnoresAPIEntryWithoutDollarBudget(t *testing.T) {
	cfg := rungFixtureConfig()
	g, adm, _, _ := burnGuardRig(t, &cfg)
	s := billingModeServer(t, &inference.LLM{Name: "claude-code", Kind: inference.KindClaudeCode, BillingMode: config.BillingModeAPI})
	g.in.Applies = s.burnGuardApplies
	d := g.Tick(context.Background())
	if d.Level != 0 || adm.cap != 3 {
		t.Fatalf("api entry was guarded: %+v cap=%d", d, adm.cap)
	}

	// With a dollar budget set, the guard acts as it does for allowance.
	s2 := billingModeServer(t, &inference.LLM{Name: "claude-code", Kind: inference.KindClaudeCode, BillingMode: config.BillingModeAPI,
		Budget: &inference.BudgetWindow{CeilingUSD: 50, Hours: 24}})
	g2, adm2, _, _ := burnGuardRig(t, &cfg)
	g2.in.Applies = s2.burnGuardApplies
	if d := g2.Tick(context.Background()); d.Rung != burnguard.RungLowerInflight || adm2.cap != 2 {
		t.Fatalf("budgeted api entry not guarded: %+v cap=%d", d, adm2.cap)
	}
}
