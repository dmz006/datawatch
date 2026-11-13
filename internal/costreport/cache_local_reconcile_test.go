package costreport

import (
	"math"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

func TestBuild_CountsCacheAndLocalTokens(t *testing.T) {
	at := fixedDay(time.October, 7, 9)
	src := &fakeSource{result: &SourceResult{Records: []Record{
		{SessionID: "a", AutomatonID: "p1", Backend: "claude-code", At: at, TokensIn: 1000, TokensOut: 500,
			CacheRead: 100000, CacheCreation: 20000},
		{SessionID: "b", AutomatonID: "p1", Backend: "pi-prompt", At: at, TokensIn: 300, TokensOut: 200,
			CacheRead: 50},
		{SessionID: "c", AutomatonID: "p2", Backend: "ollama", At: at, TokensIn: 40, TokensOut: 10},
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src, nil, testRate, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	tot := rep.Totals
	if tot.Sessions != 3 || tot.TokensIn != 1340 || tot.TokensOut != 710 ||
		tot.CacheRead != 100050 || tot.CacheCreation != 20000 || tot.LocalTokens != 550 {
		t.Fatalf("totals = %+v", tot)
	}
	var p1 Row
	for _, r := range rep.Rows {
		if r.Key == "p1" {
			p1 = r
		}
	}
	if p1.CacheRead != 100050 || p1.CacheCreation != 20000 || p1.LocalTokens != 500 {
		t.Fatalf("p1 row = %+v", p1)
	}
}

func TestRecordUSD_PricesCacheApartFromInput(t *testing.T) {
	rate := session.CostRate{InPerK: 0.01, OutPerK: 0.02}
	r := Record{TokensIn: 1000, TokensOut: 1000, CacheRead: 10000, CacheCreation: 1000}
	// 0.01 + 0.02 + 10*0.01*0.1 + 1*0.01*1.25
	want := 0.01 + 0.02 + 0.01 + 0.0125
	if got := recordUSD(rate, r); math.Abs(got-want) > tokFuzz {
		t.Fatalf("recordUSD = %v, want %v", got, want)
	}
	if got := recordUSD(rate, Record{TokensIn: 1000}); math.Abs(got-0.01) > tokFuzz {
		t.Fatalf("no-cache record must price as before, got %v", got)
	}
}

func TestBuild_UsdIncludesCacheTokens(t *testing.T) {
	src := &fakeSource{result: &SourceResult{Records: []Record{
		{Backend: "claude-code", At: fixedDay(time.October, 7, 9), TokensIn: 1000, CacheRead: 10000},
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByBackend}, src, nil, testRate, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Rows) != 1 || rep.Rows[0].USDEstimate == nil || math.Abs(*rep.Rows[0].USDEstimate-0.02) > tokFuzz {
		t.Fatalf("rows = %+v", rep.Rows)
	}
}

func TestIsLocalBackend(t *testing.T) {
	for b, want := range map[string]bool{"pi": true, "pi-prompt": true, "Ollama": true, "ollama-x": true,
		"claude-code": false, "pistachio": false, "": false} {
		if got := isLocalBackend(b); got != want {
			t.Errorf("isLocalBackend(%q) = %v, want %v", b, got, want)
		}
	}
}

func bundledBilling(string) config.EffectiveBillingProfile {
	return config.EffectiveBillingProfile{Profile: config.BillingProfile{Mode: config.BillingModeBundledPlan, MonthlyPrice: 100}}
}

func TestBuild_ReconcilesAgainstReadings(t *testing.T) {
	d1 := fixedDay(time.October, 6, 0)
	d2 := fixedDay(time.October, 7, 0)
	src := &fakeSource{result: &SourceResult{Records: []Record{
		{Backend: "claude-code", At: d1.Add(3 * time.Hour), TokensIn: 1000},    // $0.01
		{Backend: "claude-code", At: d2.Add(3 * time.Hour), TokensOut: 1000},   // $0.02
		{Backend: "claude-code", At: d2.Add(4 * time.Hour), CacheRead: 100000}, // $0.10
		{Backend: "ollama", At: d2.Add(5 * time.Hour), TokensIn: 5000},         // no rate: left out
	}}}
	req := Request{GroupBy: GroupByBackend, Usage: &UsageInput{PaceLine: 3, Days: []UsageDay{
		{Start: d1, End: d1.Add(day), Points: 1},
		{Start: d2, End: d2.Add(day), Points: 4},
	}}}
	rep, err := Build(config.CostReportConfig{}, req, src, nil, testRate, bundledBilling, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	rc := rep.Reconciliation
	if rc == nil {
		t.Fatal("reconciliation missing")
	}
	if rc.Points != 5 || rc.PaceLine != 3 || !rc.AheadOfPace || len(rc.Days) != 2 {
		t.Fatalf("reconciliation = %+v", rc)
	}
	if rc.Days[0].Date != "2026-10-06" || math.Abs(rc.Days[0].APIEquivalentUSD-0.01) > tokFuzz ||
		math.Abs(rc.Days[1].APIEquivalentUSD-0.12) > tokFuzz {
		t.Fatalf("days = %+v", rc.Days)
	}
	if rc.ImpliedUSDPerPoint == nil || math.Abs(*rc.ImpliedUSDPerPoint-0.13/5) > tokFuzz {
		t.Fatalf("implied = %v", rc.ImpliedUSDPerPoint)
	}
}

func TestBuild_NoReadingsNoReconciliation(t *testing.T) {
	src := &fakeSource{result: &SourceResult{}}
	for _, u := range []*UsageInput{nil, {}, {PaceLine: 4}} {
		rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByBackend, Usage: u}, src, nil, testRate, nil, fixedNow)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Reconciliation != nil {
			t.Fatalf("usage %+v: reconciliation must be nil", u)
		}
	}
}

func TestReconcile_SkipsNonBundledPeerAndNilRate(t *testing.T) {
	d := fixedDay(time.October, 7, 0)
	usage := &UsageInput{Days: []UsageDay{{Start: d, End: d.Add(day), Points: 0}}}
	recs := []Record{
		{Backend: "claude-code", At: d.Add(time.Hour), TokensIn: 1000, Peer: true},
		{Backend: "claude-code", At: time.Time{}, TokensIn: 1000},
		{Backend: "claude-code", At: d.Add(time.Hour), TokensIn: 1000},
	}
	metered := func(string) config.EffectiveBillingProfile {
		return config.EffectiveBillingProfile{Profile: config.BillingProfile{Mode: config.BillingModeAPIMetered}}
	}
	got := reconcile(usage, recs, testRate, metered, d.Add(-day), d.Add(2*day), false)
	if got.APIEquivalentUSD != 0 || got.ImpliedUSDPerPoint != nil || got.AheadOfPace {
		t.Fatalf("metered backend must not count: %+v", got)
	}
	got = reconcile(usage, recs, nil, nil, d.Add(-day), d.Add(2*day), false)
	if got.APIEquivalentUSD != 0 {
		t.Fatalf("nil rate must price nothing: %+v", got)
	}
	got = reconcile(usage, recs, testRate, nil, d.Add(-day), d.Add(2*day), false)
	if math.Abs(got.APIEquivalentUSD-0.01) > tokFuzz {
		t.Fatalf("only the in-window, non-peer record counts, got %v", got.APIEquivalentUSD)
	}
}
