package costreport

import (
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
)

// unitBilling resolves each backend through a fixed LLM entry table, the way
// the report does with the registry.
func unitBilling(entries map[string]inference.LLM) BillingFunc {
	cfg := &config.Config{}
	cfg.Usage.Billing = map[string]config.BillingProfile{
		"claude-code": {Mode: config.BillingModeBundledPlan, MonthlyPrice: 100},
	}
	return ResolvedBilling(cfg, func(b string) (inference.LLM, bool) {
		l, ok := entries[b]
		return l, ok
	})
}

var unitEntries = map[string]inference.LLM{
	"claude-code": {Name: "claude-code", Kind: inference.KindClaudeCode, BillingMode: config.BillingModeAllowance},
	"aider":       {Name: "aider", Kind: inference.Kind("aider"), BillingMode: config.BillingModeAPI},
	"ollama":      {Name: "ollama", Kind: inference.KindOllama, BillingMode: config.BillingModeLocal},
}

func unitOf(rep *Report, unit string) *UnitTotal {
	for i := range rep.TotalsByUnit {
		if rep.TotalsByUnit[i].Unit == unit {
			return &rep.TotalsByUnit[i]
		}
	}
	return nil
}

func buildUnits(t *testing.T, recs []Record) *Report {
	t.Helper()
	src := &fakeSource{result: &SourceResult{Records: recs}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src, nil, testRate,
		unitBilling(unitEntries), fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestBillingModeUnitsMixedGroup(t *testing.T) {
	at := fixedDay(time.October, 6, 10)
	rep := buildUnits(t, []Record{
		makeRecord("s1", "x", "mix", "claude-code", at, 1000, 2000, false),
		makeRecord("s2", "x", "mix", "aider", at, 500, 500, false),
		makeRecord("s3", "x", "mix", "ollama", at, 100, 100, false),
	})
	if len(rep.TotalsByUnit) != 3 {
		t.Fatalf("want 3 unit totals, got %+v", rep.TotalsByUnit)
	}
	d := unitOf(rep, UnitDollars)
	if d == nil || d.Dollars == nil || d.Points != nil {
		t.Fatalf("dollars total wrong: %+v", d)
	}
	floatEq(t, *d.Dollars, 0.018) // aider only, never plus the allowance estimate
	p := unitOf(rep, UnitPoints)
	if p == nil || p.Points == nil || p.Dollars != nil {
		t.Fatalf("points total wrong: %+v", p)
	}
	n := unitOf(rep, UnitNone)
	if n == nil || n.Dollars != nil || n.Points != nil || n.TokensIn != 100 {
		t.Fatalf("local total wrong: %+v", n)
	}
	row := rep.Rows[0]
	if row.BillingMode != MixedBillingMode || row.USDEstimate != nil || len(row.UnitTotals) != 3 {
		t.Fatalf("mixed row wrong: %+v", row)
	}
}

func TestBillingModeUnitsAllLocalGroup(t *testing.T) {
	at := fixedDay(time.October, 6, 10)
	rep := buildUnits(t, []Record{
		makeRecord("s1", "x", "loc", "ollama", at, 100, 200, false),
		makeRecord("s2", "x", "loc", "ollama", at, 50, 50, false),
	})
	if len(rep.TotalsByUnit) != 1 || rep.TotalsByUnit[0].Unit != UnitNone {
		t.Fatalf("want one local total, got %+v", rep.TotalsByUnit)
	}
	tot := rep.TotalsByUnit[0]
	if tot.Dollars != nil || tot.Points != nil || tot.Percent != nil || tot.TokensIn != 150 || tot.TokensOut != 250 {
		t.Fatalf("local total wrong: %+v", tot)
	}
	row := rep.Rows[0]
	if row.USDEstimate != nil || row.APIEquivalentUSD != nil || row.TokensIn != 150 || row.BillingMode != config.BillingModeFreeLocal {
		t.Fatalf("local row wrong: %+v", row)
	}
}

func TestBillingModeUnitsAPIOnlyGroup(t *testing.T) {
	at := fixedDay(time.October, 6, 10)
	rep := buildUnits(t, []Record{
		makeRecord("s1", "x", "api", "aider", at, 500, 500, false),
		makeRecord("s2", "x", "api", "aider", at, 500, 500, false),
	})
	if len(rep.TotalsByUnit) != 1 || rep.TotalsByUnit[0].Unit != UnitDollars {
		t.Fatalf("want one dollars total, got %+v", rep.TotalsByUnit)
	}
	floatEq(t, *rep.TotalsByUnit[0].Dollars, 0.036)
	row := rep.Rows[0]
	if row.USDEstimate == nil || row.BillingMode != config.BillingModeAPIMetered {
		t.Fatalf("api row wrong: %+v", row)
	}
	floatEq(t, *row.USDEstimate, 0.036)
}
