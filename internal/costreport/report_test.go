package costreport

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// fakeSource is the test Source: it records the query it got and answers
// with a fixed result, so the tests exercise the report against the
// contract, not against a real spend store.
type fakeSource struct {
	result *SourceResult
	err    error
	got    SourceOptions
}

func (f *fakeSource) Records(opts SourceOptions) (*SourceResult, error) {
	f.got = opts
	return f.result, f.err
}

// testRates is the per-backend rate table the tests price with; "ollama"
// deliberately has no entry so the rate-missing paths are covered.
var testRateMap = map[string]session.CostRate{
	"claude-code": {InPerK: 0.01, OutPerK: 0.02},
	"aider":       {InPerK: 0.03, OutPerK: 0.006},
}

func testRate(backend string) (session.CostRate, bool) {
	rate, ok := testRateMap[backend]
	return rate, ok
}

const tokFuzz = 1e-9

var (
	day      = 24 * time.Hour
	fixedNow = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
)

func fixedDay(month time.Month, day, hour int) time.Time {
	return time.Date(2026, month, day, hour, 0, 0, 0, time.UTC)
}

func makeRecord(sessionID, automationID, automatonID, backend string, at time.Time, in, out int, peer bool) Record {
	return Record{
		SessionID:    sessionID,
		AutomationID: automationID,
		AutomatonID:  automatonID,
		Backend:      backend,
		At:           at,
		TokensIn:     in,
		TokensOut:    out,
		Peer:         peer,
	}
}

func floatEq(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > tokFuzz {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func wantRow(t *testing.T, report *Report, key string, want Row) {
	t.Helper()
	for _, row := range report.Rows {
		if row.Key != key {
			continue
		}
		if row.Sessions != want.Sessions || row.TokensIn != want.TokensIn || row.TokensOut != want.TokensOut ||
			row.RateConfigured != want.RateConfigured {
			t.Fatalf("row %q: want %+v, got %+v", key, want, row)
		}
		if want.USDEstimate == nil {
			if row.USDEstimate != nil {
				t.Fatalf("row %q: want no usd_estimate, got %v", key, *row.USDEstimate)
			}
			return
		}
		if row.USDEstimate == nil {
			t.Fatalf("row %q: want usd_estimate %v, got none", key, *want.USDEstimate)
		}
		floatEq(t, *row.USDEstimate, *want.USDEstimate)
		return
	}
	t.Fatalf("row %q not found in %d rows", key, len(report.Rows))
}

func usd(v float64) *float64 { return &v }

func TestCostReportRowsPerAutomaton(t *testing.T) {
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("s1", "auto-1", "auto1", "claude-code", fixedDay(time.October, 6, 10), 1000, 2000, false),
		makeRecord("s2", "auto-2", "auto1", "claude-code", fixedDay(time.October, 6, 11), 0, 1000, false),
		makeRecord("s3", "auto-3", "auto2", "aider", fixedDay(time.October, 7, 9), 500, 500, false),
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src, nil, testRate, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rep.Rows))
	}
	wantRow(t, rep, "auto1", Row{Sessions: 2, TokensIn: 1000, TokensOut: 3000, RateConfigured: true, USDEstimate: usd(0.07)})
	wantRow(t, rep, "auto2", Row{Sessions: 1, TokensIn: 500, TokensOut: 500, RateConfigured: true, USDEstimate: usd(0.018)})
}

func TestCostReportRowsPerAutomation(t *testing.T) {
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("s1", "a1", "auto1", "claude-code", fixedDay(time.October, 6, 10), 100, 200, false),
		makeRecord("s2", "a1", "auto1", "aider", fixedDay(time.October, 6, 11), 10, 20, false),
		makeRecord("s3", "a2", "auto2", "aider", fixedDay(time.October, 7, 9), 30, 40, false),
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomation}, src, nil, testRate, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rep.Rows))
	}
	wantRow(t, rep, "a1", Row{Sessions: 2, TokensIn: 110, TokensOut: 220, RateConfigured: true,
		USDEstimate: usd(0.00542)})
	wantRow(t, rep, "a2", Row{Sessions: 1, TokensIn: 30, TokensOut: 40, RateConfigured: true, USDEstimate: usd(0.00114)})
}

func TestCostReportRowsPerBackend(t *testing.T) {
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("s1", "a1", "auto1", "claude-code", fixedDay(time.October, 6, 10), 1000, 2000, false),
		makeRecord("s2", "a2", "auto2", "aider", fixedDay(time.October, 7, 9), 500, 500, false),
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByBackend}, src, nil, testRate, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rep.Rows))
	}
	wantRow(t, rep, "aider", Row{Sessions: 1, TokensIn: 500, TokensOut: 500, RateConfigured: true, USDEstimate: usd(0.018)})
	wantRow(t, rep, "claude-code", Row{Sessions: 1, TokensIn: 1000, TokensOut: 2000, RateConfigured: true, USDEstimate: usd(0.05)})
}

func TestCostReportRowsPerWeek(t *testing.T) {
	records := []Record{
		// Tuesday Oct 6 → monday week 2026-10-05, sunday week 2026-10-04
		makeRecord("s1", "a1", "auto1", "claude-code", fixedDay(time.October, 6, 10), 100, 100, false),
		// Sunday Oct 4 → same key under both starts: 2026-10-04
		makeRecord("s2", "a2", "auto2", "aider", fixedDay(time.October, 4, 9), 50, 50, false),
		// Thursday Oct 8 → monday week 2026-10-05, sunday week 2026-10-04
		makeRecord("s3", "a3", "auto3", "claude-code", fixedDay(time.October, 8, 9), 10, 10, false),
	}
	for _, tc := range []struct {
		name string
		cfg  config.CostReportConfig
		want map[string]int // week key → sessions
	}{
		{
			name: "monday",
			cfg:  config.CostReportConfig{WeekStart: ptr("monday")},
			want: map[string]int{"2026-10-04": 1, "2026-10-05": 2},
		},
		{
			name: "sunday",
			cfg:  config.CostReportConfig{WeekStart: ptr("sunday")},
			want: map[string]int{"2026-10-04": 3},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := &fakeSource{result: &SourceResult{Records: records}}
			rep, err := Build(tc.cfg, Request{GroupBy: GroupByWeek}, src, nil, testRate, nil, fixedNow)
			if err != nil {
				t.Fatal(err)
			}
			if len(rep.Rows) != len(tc.want) {
				t.Fatalf("want %d rows, got %d: %+v", len(tc.want), len(rep.Rows), rep.Rows)
			}
			for key, sessions := range tc.want {
				for _, row := range rep.Rows {
					if row.Key == key && row.Sessions != sessions {
						t.Fatalf("week %s: want %d sessions, got %d", key, sessions, row.Sessions)
					}
				}
			}
		})
	}
}

func TestCostReportUSDEstimateWhenRateConfigured(t *testing.T) {
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("s1", "a1", "auto1", "aider", fixedDay(time.October, 6, 10), 1000, 1000, false),
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src, nil, testRate, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	// 1000/1K * 0.03 + 1000/1K * 0.006 = 0.036
	wantRow(t, rep, "auto1", Row{Sessions: 1, TokensIn: 1000, TokensOut: 1000, RateConfigured: true, USDEstimate: usd(0.036)})
}

func TestCostReportUSDEstimateAbsentWhenRateMissing(t *testing.T) {
	src := &fakeSource{result: &SourceResult{Records: []Record{
		// Group "mixed" spans a priced and an unpriced backend → no estimate.
		makeRecord("s1", "a1", "mixed", "claude-code", fixedDay(time.October, 6, 10), 1000, 0, false),
		makeRecord("s2", "a1", "mixed", "ollama", fixedDay(time.October, 6, 11), 1000, 0, false),
		// Group "local" is entirely unpriced → no estimate either.
		makeRecord("s3", "a2", "local", "ollama", fixedDay(time.October, 6, 12), 42, 42, false),
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src, nil, testRate, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	// Tokens still come first and stay complete even without a rate.
	wantRow(t, rep, "mixed", Row{Sessions: 2, TokensIn: 2000, TokensOut: 0, RateConfigured: false})
	wantRow(t, rep, "local", Row{Sessions: 1, TokensIn: 42, TokensOut: 42, RateConfigured: false})
}

func TestCostReportUSDEstimateAbsentWhenNoRateSource(t *testing.T) {
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("s1", "a1", "auto1", "claude-code", fixedDay(time.October, 6, 10), 100, 100, false),
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src, nil, nil, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	wantRow(t, rep, "auto1", Row{Sessions: 1, TokensIn: 100, TokensOut: 100, RateConfigured: false})
}

func TestCostReportWindowBounds(t *testing.T) {
	since := fixedDay(time.October, 5, 0)
	until := fixedDay(time.October, 6, 0)
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("before", "a", "auto1", "aider", fixedDay(time.October, 4, 23), 1, 1, false),
		makeRecord("edge-in", "b", "auto1", "aider", since, 10, 10, false),
		makeRecord("mid", "c", "auto1", "aider", fixedDay(time.October, 5, 12), 20, 20, false),
		makeRecord("edge-out", "d", "auto2", "aider", until, 40, 40, false),
		makeRecord("after", "e", "auto2", "aider", fixedDay(time.October, 6, 1), 80, 80, false),
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton, Since: since, Until: until}, src, nil, testRate, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if !src.got.Since.Equal(since) || !src.got.Until.Equal(until) {
		t.Fatalf("source got window %v..%v, want %v..%v", src.got.Since, src.got.Until, since, until)
	}
	// auto1 keeps edge-in + mid, drops "before"; auto2 keeps only the
	// until-edge session, drops "after".
	wantRow(t, rep, "auto1", Row{Sessions: 2, TokensIn: 30, TokensOut: 30, RateConfigured: true, USDEstimate: usd(0.00108)})
	wantRow(t, rep, "auto2", Row{Sessions: 1, TokensIn: 40, TokensOut: 40, RateConfigured: true, USDEstimate: usd(0.00144)})
}

func TestCostReportDefaultWindowFromConfig(t *testing.T) {
	for _, tc := range []struct {
		name      string
		days      *int
		wantSince time.Time
	}{
		{"default seven days", nil, fixedNow.Add(-7 * day)},
		{"configured thirty days", ptr(30), fixedNow.Add(-30 * day)},
		{"nonpositive value stays default", ptr(0), fixedNow.Add(-7 * day)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := &fakeSource{result: &SourceResult{}}
			rep, err := Build(config.CostReportConfig{DefaultWindowDays: tc.days}, Request{Until: fixedNow}, src, nil, testRate, nil, fixedNow)
			if err != nil {
				t.Fatal(err)
			}
			if !rep.Until.Equal(fixedNow) {
				t.Fatalf("until: want %v, got %v", fixedNow, rep.Until)
			}
			if !rep.Since.Equal(tc.wantSince) {
				t.Fatalf("since: want %v, got %v", tc.wantSince, rep.Since)
			}
			if !src.got.Since.Equal(tc.wantSince) || !src.got.Until.Equal(fixedNow) {
				t.Fatalf("source got %v..%v, want %v..%v", src.got.Since, src.got.Until, tc.wantSince, fixedNow)
			}
		})
	}
}

func TestCostReportRetentionAndFallbackFields(t *testing.T) {
	src := &fakeSource{result: &SourceResult{
		Records:          []Record{makeRecord("s1", "a1", "auto1", "aider", fixedDay(time.October, 6, 10), 1, 1, false)},
		LifetimeFallback: true,
	}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src, nil, testRate, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if rep.RetentionDays != 31 {
		t.Fatalf("retention_days: want 31, got %d", rep.RetentionDays)
	}
	if !rep.LifetimeFallback {
		t.Fatal("lifetime_fallback: want true, got false")
	}
	// And it stays off when the source did not fall back.
	src2 := &fakeSource{result: &SourceResult{}}
	rep2, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src2, nil, testRate, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if rep2.RetentionDays != RetentionDays {
		t.Fatalf("retention_days: want %d, got %d", RetentionDays, rep2.RetentionDays)
	}
	if rep2.LifetimeFallback {
		t.Fatal("lifetime_fallback: want false, got true")
	}
}

func TestCostReportConfigDefaults(t *testing.T) {
	var cfg config.CostReportConfig
	if got := cfg.EffectiveGroupBy(); got != "automaton" {
		t.Fatalf("default_group_by: want automaton, got %q", got)
	}
	if got := cfg.EffectiveWindowDays(); got != config.DefaultCostReportWindowDays && got != 7 {
		t.Fatalf("default_window_days: want 7, got %d", got)
	}
	if got := cfg.EffectiveShowUSDEstimates(); !got {
		t.Fatal("show_usd_estimates: want default true")
	}
	if got := cfg.EffectiveWeekStart(); got != config.DefaultCostReportWeekStart && got != "monday" {
		t.Fatalf("week_start: want monday, got %q", got)
	}
	if got := cfg.EffectiveIncludePeerSpend(); got {
		t.Fatal("include_peer_spend: want default false")
	}
}

func TestCostReportGroupByDefaultsTable(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  string
		want GroupBy
	}{
		{"absent", "", GroupByAutomaton},
		{"automaton", "automaton", GroupByAutomaton},
		{"automation", "automation", GroupByAutomation},
		{"backend", "backend", GroupByBackend},
		{"release", "release", GroupByRelease},
		{"week", "week", GroupByWeek},
		{"unknown falls back to automaton", "bogus", GroupByAutomaton},
		{"case-insensitive", "Week", GroupByWeek},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg config.CostReportConfig
			if tc.cfg != "" {
				cfg.DefaultGroupBy = &tc.cfg
			}
			src := &fakeSource{result: &SourceResult{}}
			rep, err := Build(cfg, Request{}, src, &fakeShipped{}, testRate, nil, fixedNow)
			if err != nil {
				t.Fatal(err)
			}
			if rep.GroupBy != tc.want {
				t.Fatalf("group_by: want %q, got %q", tc.want, rep.GroupBy)
			}
		})
	}
}

func TestCostReportInvalidGroupByIsRejected(t *testing.T) {
	src := &fakeSource{result: &SourceResult{}}
	if _, err := Build(config.CostReportConfig{}, Request{GroupBy: "everything"}, src, nil, testRate, nil, fixedNow); err == nil {
		t.Fatal("want an error for an unknown group_by, got none")
	}
}

func TestCostReportPeerSpendExcludedByDefault(t *testing.T) {
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("own", "a1", "auto1", "aider", fixedDay(time.October, 6, 10), 10, 10, false),
		makeRecord("peer", "a2", "auto2", "aider", fixedDay(time.October, 6, 11), 99, 99, true),
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src, nil, testRate, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if src.got.IncludePeerSpend {
		t.Fatal("source options: include_peer_spend should default to false")
	}
	wantRow(t, rep, "auto1", Row{Sessions: 1, TokensIn: 10, TokensOut: 10, RateConfigured: true, USDEstimate: usd(0.00036)})
	for _, row := range rep.Rows {
		if row.Key == "auto2" {
			t.Fatalf("peer row should be excluded, got %+v", row)
		}
	}
}

func TestCostReportPeerSpendIncludedWhenConfigured(t *testing.T) {
	cfg := config.CostReportConfig{IncludePeerSpend: ptr(true)}
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("own", "a1", "auto1", "aider", fixedDay(time.October, 6, 10), 10, 10, false),
		makeRecord("peer", "a2", "auto2", "aider", fixedDay(time.October, 6, 11), 99, 99, true),
	}}}
	rep, err := Build(cfg, Request{GroupBy: GroupByAutomaton}, src, nil, testRate, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if !src.got.IncludePeerSpend {
		t.Fatal("source options: include_peer_spend should be true when configured")
	}
	if len(rep.Rows) != 2 {
		t.Fatalf("want both rows, got %+v", rep.Rows)
	}
	wantRow(t, rep, "auto2", Row{Sessions: 1, TokensIn: 99, TokensOut: 99, RateConfigured: true, USDEstimate: usd(0.003564)})
}

func TestCostReportUSDEstimatesHiddenWhenConfiguredOff(t *testing.T) {
	cfg := config.CostReportConfig{ShowUSDEstimates: ptr(false)}
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("s1", "a1", "auto1", "aider", fixedDay(time.October, 6, 10), 1000, 1000, false),
	}}}
	rep, err := Build(cfg, Request{GroupBy: GroupByAutomaton}, src, nil, testRate, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	// Tokens still show; the estimate is absent and the row stays unpriced.
	wantRow(t, rep, "auto1", Row{Sessions: 1, TokensIn: 1000, TokensOut: 1000, RateConfigured: false})
}

func TestCostReportSourceFailureSurfaces(t *testing.T) {
	wanted := errors.New("store unavailable")
	src := &fakeSource{err: wanted}
	if _, err := Build(config.CostReportConfig{}, Request{}, src, nil, testRate, nil, fixedNow); err == nil || !errors.Is(err, wanted) {
		t.Fatalf("want wrapped source error, got %v", err)
	}
}

func TestCostReportNilSourceIsRejected(t *testing.T) {
	if _, err := Build(config.CostReportConfig{}, Request{}, nil, nil, testRate, nil, fixedNow); err == nil {
		t.Fatal("want an error for a nil source, got none")
	}
}

func ptr[T any](v T) *T { return &v }

// billingOf builds the BillingFunc over a usage.billing map, the same
// resolution the daemon wires (config.UsageConfig.EffectiveBillingProfile).
func billingOf(profiles map[string]config.BillingProfile) BillingFunc {
	uc := config.UsageConfig{Billing: profiles}
	return uc.EffectiveBillingProfile
}

// fakeShipped is the test ShippedSource: one fixed unit list.
type fakeShipped struct {
	units []ShippedUnit
	err   error
}

func (f *fakeShipped) ShippedUnits() (*ShippedResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &ShippedResult{Units: f.units}, nil
}

func TestCostReportBillingBundledPlan(t *testing.T) {
	billing := billingOf(map[string]config.BillingProfile{
		"aider": {Mode: config.BillingModeBundledPlan, MonthlyPrice: 100, Currency: "USD"},
	})
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("s1", "a1", "auto1", "aider", fixedDay(time.October, 6, 10), 1000, 1000, false),
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src, nil, testRate, billing, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	var row Row
	for _, r := range rep.Rows {
		if r.Key == "auto1" {
			row = r
			break
		}
	}
	if row.BillingMode != config.BillingModeBundledPlan {
		t.Fatalf("billing_mode: want %q, got %q", config.BillingModeBundledPlan, row.BillingMode)
	}
	if !strings.Contains(row.BillingLabel, "API-equivalent, not billed") {
		t.Fatalf("billing_label: want the API-equivalent wording, got %q", row.BillingLabel)
	}
	if row.USDEstimate != nil {
		t.Fatalf("bundled plans are not billed: want no usd_estimate, got %v", *row.USDEstimate)
	}
	if row.APIEquivalentUSD == nil {
		t.Fatal("want api_equivalent_usd priced at API rates")
	}
	// 1000/1K * 0.03 + 1000/1K * 0.006 = 0.036
	floatEq(t, *row.APIEquivalentUSD, 0.036)
	// Points: est / (monthly_price / (52/12) / 100).
	pc := config.BillingProfile{MonthlyPrice: 100}.PointCostEquivalent()
	floatEq(t, *row.EstimatedUsagePoints, 0.036/pc)
	floatEq(t, *row.ShareOfMonthlyPrice, 0.036/100)
}

func TestCostReportBillingFreeLocal(t *testing.T) {
	billing := billingOf(map[string]config.BillingProfile{
		"aider": {Mode: config.BillingModeFreeLocal},
	})
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("s1", "a1", "auto1", "aider", fixedDay(time.October, 6, 10), 1000, 1000, false),
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src, nil, testRate, billing, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	var row Row
	for _, r := range rep.Rows {
		if r.Key == "auto1" {
			row = r
			break
		}
	}
	if row.BillingMode != config.BillingModeFreeLocal {
		t.Fatalf("billing_mode: want %q, got %q", config.BillingModeFreeLocal, row.BillingMode)
	}
	if row.USDEstimate != nil || row.APIEquivalentUSD != nil {
		t.Fatalf("free local inference is not billed: want no dollar figures, got %+v", row)
	}
	if row.TokensIn != 1000 {
		t.Fatalf("tokens stay visible, got %+v", row)
	}
}

func TestCostReportBillingAssumedAndMetered(t *testing.T) {
	// No usage.billing entry at all: the row reads as assumed api_metered;
	// an explicit one reads as metered.
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("s1", "a1", "auto1", "aider", fixedDay(time.October, 6, 10), 1000, 1000, false),
	}}}
	// Assumed: the billing map does not name "aider".
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src, nil, testRate, billingOf(nil), fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	var assumed Row
	var metered Row
	for _, r := range rep.Rows {
		if r.Key == "auto1" {
			assumed = r
		}
	}
	if assumed.BillingMode != config.BillingModeAPIMetered {
		t.Fatalf("billing_mode: want %q, got %q", config.BillingModeAPIMetered, assumed.BillingMode)
	}
	if !strings.Contains(assumed.BillingLabel, "assumed") {
		t.Fatalf("billing_label: want the assumed wording, got %q", assumed.BillingLabel)
	}
	if assumed.USDEstimate == nil {
		t.Fatal("assumed metering still carries the usd_estimate")
	}
	floatEq(t, *assumed.USDEstimate, 0.036)

	// Explicit metered.
	billing := billingOf(map[string]config.BillingProfile{
		"aider": {Mode: config.BillingModeAPIMetered},
	})
	rep, err = Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src, nil, testRate, billing, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rep.Rows {
		if r.Key == "auto1" {
			metered = r
		}
	}
	if metered.BillingMode != config.BillingModeAPIMetered {
		t.Fatalf("billing_mode: want %q, got %q", config.BillingModeAPIMetered, metered.BillingMode)
	}
	if strings.Contains(metered.BillingLabel, "assumed") {
		t.Fatalf("an explicit profile is not assumed, got %q", metered.BillingLabel)
	}
	if metered.USDEstimate == nil {
		t.Fatal("metered rows carry the usd_estimate")
	}
	floatEq(t, *metered.USDEstimate, 0.036)
}

func TestCostReportBillingMixedModes(t *testing.T) {
	billing := billingOf(map[string]config.BillingProfile{
		"aider":       {Mode: config.BillingModeBundledPlan, MonthlyPrice: 100},
		"claude-code": {Mode: config.BillingModeAPIMetered},
	})
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("s1", "a1", "auto1", "aider", fixedDay(time.October, 6, 10), 1000, 1000, false),
		makeRecord("s2", "a2", "auto1", "claude-code", fixedDay(time.October, 6, 11), 1000, 1000, false),
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src, nil, testRate, billing, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	var row Row
	for _, r := range rep.Rows {
		if r.Key == "auto1" {
			row = r
		}
	}
	if row.BillingMode != MixedBillingMode {
		t.Fatalf("billing_mode: want %q, got %q", MixedBillingMode, row.BillingMode)
	}
	if row.USDEstimate == nil {
		t.Fatal("mixed rows keep the unambiguous usd_estimate")
	}
	// aider 0.036 + claude-code (1000/1K*0.01 + 1000/1K*0.02) 0.03 = 0.066
	floatEq(t, *row.USDEstimate, 0.066)
}

func TestCostReportBillingNilKeepsLegacyShape(t *testing.T) {
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("s1", "a1", "auto1", "aider", fixedDay(time.October, 6, 10), 1000, 1000, false),
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src, nil, testRate, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	var row Row
	for _, r := range rep.Rows {
		if r.Key == "auto1" {
			row = r
		}
	}
	if row.USDEstimate == nil {
		t.Fatal("legacy shape keeps the usd_estimate")
	}
	if row.BillingMode != "" || row.BillingLabel != "" || row.APIEquivalentUSD != nil {
		t.Fatalf("legacy shape has no billing fields, got %+v", row)
	}
}

func TestCostReportReleaseGrouping(t *testing.T) {
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("s1", "a1", "auto1", "aider", fixedDay(time.October, 6, 10), 100, 100, false),
		makeRecord("s2", "a2", "auto2", "aider", fixedDay(time.October, 6, 11), 50, 50, false),
		makeRecord("s3", "a3", "auto3", "aider", fixedDay(time.October, 7, 9), 25, 25, false),
	}}}
	rec := src.result
	rec.Records[0].Release = "v1.2.0"
	rec.Records[1].Release = "v1.2.0"
	// rec.Records[2] keeps a blank release → "unreleased".

	shipped := &fakeShipped{units: []ShippedUnit{
		{UnitID: "prd-1", Kind: "prd", Release: "v1.1.0"},
		{UnitID: "auto-7", Kind: "automation", Release: "v1.1.0"},
		{UnitID: "prd-2", Kind: "prd", Release: "v1.2.0"},
	}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByRelease}, src, shipped, testRate, billingOf(nil), fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if rep.GroupBy != GroupByRelease {
		t.Fatalf("group_by: want release, got %q", rep.GroupBy)
	}
	if len(rep.Rows) != 3 {
		t.Fatalf("want v1.1.0, v1.2.0 and unreleased, got %+v", rep.Rows)
	}
	want := map[string]struct {
		sessions int
		units    int
	}{
		"v1.1.0":     {sessions: 0, units: 2}, // shipped, no spend in the window
		"v1.2.0":     {sessions: 2, units: 1}, // two sessions, one shipped PRD
		"unreleased": {sessions: 1, units: 0},
	}
	for key, w := range want {
		var row Row
		found := false
		for _, r := range rep.Rows {
			if r.Key == key {
				row = r
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("release %q not found in %+v", key, rep.Rows)
		}
		if row.Sessions != w.sessions || row.Units != w.units {
			t.Fatalf("release %q: want sessions=%d units=%d, got sessions=%d units=%d", key, w.sessions, w.units, row.Sessions, row.Units)
		}
	}
	// Rows stay sorted by key.
	for i := 1; i < len(rep.Rows); i++ {
		if rep.Rows[i-1].Key >= rep.Rows[i].Key {
			t.Fatalf("rows not sorted by key: %+v", rep.Rows)
		}
	}
}

func TestCostReportReleaseWithoutShippedSourceIsRejected(t *testing.T) {
	src := &fakeSource{result: &SourceResult{}}
	if _, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByRelease}, src, nil, testRate, billingOf(nil), fixedNow); err == nil {
		t.Fatal("want an error for group_by=release without a shipped source")
	}
}

func TestCostReportRetentionNote(t *testing.T) {
	// Inside the retention horizon: the note states the 31-day window.
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("s1", "a1", "auto1", "aider", fixedDay(time.October, 6, 10), 1, 1, false),
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton, Since: fixedNow.Add(-3 * day)}, src, nil, testRate, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rep.RetentionNote, "31") {
		t.Fatalf("retention note should name the 31-day window, got %q", rep.RetentionNote)
	}
	if rep.LifetimeFallback {
		t.Fatal("a 3-day window does not fall back to lifetime totals")
	}

	// Beyond the horizon: the fallback note says the rows carry lifetime
	// totals.
	rep, err = Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton, Since: fixedNow.Add(-120 * day)}, src, nil, testRate, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.LifetimeFallback {
		t.Fatal("a 120-day window must flag the lifetime fallback")
	}
	if !strings.Contains(rep.RetentionNote, "lifetime") {
		t.Fatalf("fallback note should mention lifetime totals, got %q", rep.RetentionNote)
	}
}

func TestCostReportExtendedUsageSummedPerGroup(t *testing.T) {
	t0 := fixedDay(time.October, 6, 10)
	r1 := makeRecord("s1", "", "auto1", "claude-code", t0, 100, 50, false)
	r1.CacheRead, r1.CacheCreation, r1.ToolCalls, r1.ActiveSeconds = 10, 20, 3, 60
	r1.FirstActivity, r1.LastActivity = t0, t0.Add(time.Hour)
	r2 := makeRecord("s2", "", "auto1", "claude-code", t0.Add(2*time.Hour), 1, 1, false)
	r2.CacheRead, r2.CacheCreation, r2.ToolCalls, r2.ActiveSeconds = 5, 6, 4, 30
	r2.FirstActivity, r2.LastActivity = t0.Add(time.Hour), t0.Add(3*time.Hour)
	r3 := makeRecord("s3", "", "", "claude-code", t0, 1, 1, false)
	src := &fakeSource{result: &SourceResult{Records: []Record{r1, r2, r3}}}
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src, nil, testRate, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Rows) != 2 {
		t.Fatalf("want 2 rows (auto1 + unattributed), got %+v", rep.Rows)
	}
	var row Row
	for _, r := range rep.Rows {
		if r.Key == "auto1" {
			row = r
		}
	}
	if row.Sessions != 2 || row.CacheRead != 15 || row.CacheCreation != 26 || row.ToolCalls != 7 {
		t.Fatalf("bad sums: %+v", row)
	}
	floatEq(t, row.ActiveSeconds, 90)
	floatEq(t, row.SpanSeconds, (3 * time.Hour).Seconds())
	if row.FirstActivity == nil || !row.FirstActivity.Equal(t0) || !row.LastActivity.Equal(t0.Add(3*time.Hour)) {
		t.Fatalf("bad span: %+v", row)
	}
}

func TestCostReportBundledUSDLabel(t *testing.T) {
	src := &fakeSource{result: &SourceResult{Records: []Record{
		makeRecord("s1", "", "auto1", "claude-code", fixedDay(time.October, 6, 10), 1000, 1000, false),
	}}}
	billing := billingOf(map[string]config.BillingProfile{
		"claude-code": {Mode: config.BillingModeBundledPlan, MonthlyPrice: 100},
	})
	rep, err := Build(config.CostReportConfig{}, Request{GroupBy: GroupByAutomaton}, src, nil, testRate, billing, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	row := rep.Rows[0]
	if row.APIEquivalentUSD == nil || !strings.Contains(row.USDLabel, "API-equivalent estimate") {
		t.Fatalf("want API-equivalent label, got %+v", row)
	}
}

func notesRecord(id string, first, at time.Time) Record {
	r := makeRecord(id, "", id, "claude-code", at, 10, 5, false)
	r.FirstActivity = first
	r.LastActivity = at
	return r
}

func TestCostReportWindowPartialRowNote(t *testing.T) {
	since := fixedNow.Add(-7 * day)
	src := &fakeSource{result: &SourceResult{Records: []Record{
		notesRecord("old", since.Add(-3*day), since.Add(day)),
		notesRecord("new", since.Add(day), since.Add(2*day)),
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{Until: fixedNow}, src, nil, testRate, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rep.Rows {
		switch r.Key {
		case "old":
			if !r.WindowPartial || !strings.Contains(r.WindowNote, "7 days") || !strings.Contains(r.WindowNote, "31 days") {
				t.Fatalf("old row note missing: %+v", r)
			}
		case "new":
			if r.WindowPartial || r.WindowNote != "" {
				t.Fatalf("new row must not be partial: %+v", r)
			}
		}
	}
	if !strings.Contains(rep.WindowNote, "1 row(s)") || !strings.Contains(rep.WindowNote, "not zero") {
		t.Fatalf("top-level note: %q", rep.WindowNote)
	}
}

func TestCostReportWindowNoteAbsentWhenNothingPartial(t *testing.T) {
	since := fixedNow.Add(-7 * day)
	src := &fakeSource{result: &SourceResult{Records: []Record{notesRecord("a", since.Add(day), since.Add(2*day))}}}
	rep, err := Build(config.CostReportConfig{}, Request{Until: fixedNow}, src, nil, testRate, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if rep.WindowNote != "" {
		t.Fatalf("unexpected note %q", rep.WindowNote)
	}
}

func TestCostReportUnknownSpendBeforeLedgerHorizon(t *testing.T) {
	since := fixedNow.Add(-45 * day)
	src := &fakeSource{result: &SourceResult{Records: []Record{notesRecord("a", since.Add(day), since.Add(2*day))}}}
	rep, err := Build(config.CostReportConfig{}, Request{Since: since, Until: fixedNow}, src, nil, testRate, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if rep.UnknownBefore == nil || !strings.Contains(rep.UnknownSpendNote, "unknown") || !strings.Contains(rep.UnknownSpendNote, "not zero") {
		t.Fatalf("unknown note missing: %+v", rep)
	}
	for _, r := range rep.Rows {
		if !r.SpendUnknown || r.SpendUnknownNote == "" {
			t.Fatalf("row not marked unknown: %+v", r)
		}
	}
	if len(rep.Rows) != 1 || rep.Rows[0].TokensIn != 10 {
		t.Fatalf("recorded figures must be untouched, not backfilled: %+v", rep.Rows)
	}
}

func TestCostReportUnknownAbsentInsideLedger(t *testing.T) {
	src := &fakeSource{result: &SourceResult{Records: []Record{notesRecord("a", fixedNow.Add(-2*day), fixedNow.Add(-day))}}}
	rep, err := Build(config.CostReportConfig{}, Request{Until: fixedNow}, src, nil, testRate, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if rep.UnknownSpendNote != "" || rep.UnknownBefore != nil || rep.Rows[0].SpendUnknown {
		t.Fatalf("unexpected unknown marking: %+v", rep)
	}
}

func TestCostReportPartialAndUnknownTogether(t *testing.T) {
	since := fixedNow.Add(-40 * day)
	src := &fakeSource{result: &SourceResult{Records: []Record{notesRecord("a", since.Add(-5*day), since.Add(day))}}}
	rep, err := Build(config.CostReportConfig{}, Request{Since: since, Until: fixedNow}, src, nil, testRate, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	r := rep.Rows[0]
	if !r.WindowPartial || !r.SpendUnknown || rep.WindowNote == "" || rep.UnknownSpendNote == "" {
		t.Fatalf("both notes expected: %+v / %+v", r, rep)
	}
}

func TestCostReportUnknownOnlyForRowsBeforeHorizon(t *testing.T) {
	since := fixedNow.Add(-45 * day)
	src := &fakeSource{result: &SourceResult{Records: []Record{
		notesRecord("old", since.Add(day), since.Add(2*day)),
		notesRecord("new", fixedNow.Add(-3*day), fixedNow.Add(-day)),
	}}}
	rep, err := Build(config.CostReportConfig{}, Request{Since: since, Until: fixedNow}, src, nil, testRate, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if rep.UnknownBefore == nil || len(rep.Rows) != 2 {
		t.Fatalf("unexpected report: %+v", rep)
	}
	for _, r := range rep.Rows {
		want := r.Key == "old"
		if r.SpendUnknown != want || (r.SpendUnknownNote != "") != want {
			t.Fatalf("row %s SpendUnknown=%v want %v", r.Key, r.SpendUnknown, want)
		}
	}
}
